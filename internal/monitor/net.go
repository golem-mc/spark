package monitor

import (
	"maps"
	"regexp"
	"slices"
	"sync"

	"github.com/golem-mc/spark/internal/rolling"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/net"
)

const (
	// netPollSeconds is how often the counters are read; the averages span
	// 15 minutes of readings.
	netPollSeconds = 60
	netWindow      = 15 * 60 / netPollSeconds
)

// netIgnored are virtual ethernet adapters and container bridges.
var netIgnored = regexp.MustCompile(`^(veth\w+)|(br-\w+)$`)

// NetAverages are an interface's rates per second over the last 15 minutes.
// spark: NetworkInterfaceAverages
type NetAverages struct {
	RxBytes, TxBytes, RxPackets, TxPackets *rolling.Average
}

// Network keeps per-interface traffic rates. spark: NetworkMonitor
type Network struct {
	mu       sync.Mutex
	last     map[string]net.IOCountersStat
	averages map[string]*NetAverages
}

func newNetwork() *Network { return &Network{averages: map[string]*NetAverages{}} }

func (n *Network) poll() {
	counters, err := net.IOCounters(true)
	if err != nil {
		return
	}
	latest := make(map[string]net.IOCountersStat, len(counters))
	for _, c := range counters {
		latest[c.Name] = c
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	previous := n.last
	if previous == nil && len(latest) == 0 {
		return
	}
	n.last = latest
	if previous == nil {
		return
	}
	for name := range latest {
		if !netIgnored.MatchString(name) && n.averages[name] == nil {
			n.averages[name] = &NetAverages{rolling.New(netWindow), rolling.New(netWindow), rolling.New(netWindow), rolling.New(netWindow)}
		}
	}
	rate := func(now, before uint64) float64 {
		if now < before {
			return 0
		}
		return float64(now-before) / netPollSeconds
	}
	for name, avg := range n.averages {
		now, before := latest[name], previous[name]
		avg.RxBytes.Add(rate(now.BytesRecv, before.BytesRecv))
		avg.TxBytes.Add(rate(now.BytesSent, before.BytesSent))
		avg.RxPackets.Add(rate(now.PacketsRecv, before.PacketsRecv))
		avg.TxPackets.Add(rate(now.PacketsSent, before.PacketsSent))
	}
}

// Interfaces answers the interfaces with averages, sorted by name.
func (n *Network) Interfaces() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Sorted(maps.Keys(n.averages))
}

// Averages answers an interface's averages.
func (n *Network) Averages(name string) *NetAverages {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.averages[name]
}

// Disk answers the used and total bytes of the filesystem holding the
// server's directory, zeros when the OS does not say. spark: DiskUsage
func Disk() (used, total int64) {
	u, err := disk.Usage(".")
	if err != nil {
		return 0, 0
	}
	// The JDK's total minus usable space, so space reserved for root counts
	// as used.
	return int64(u.Total - u.Free), int64(u.Total)
}
