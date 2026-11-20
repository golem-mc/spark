package platform

import (
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/golem-mc/spark/internal/metric"
	"github.com/golem-mc/spark/internal/monitor"
	"github.com/golem-mc/spark/internal/sparkproto"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

// sparkDataVersion is the version of the data format spark's viewer reads.
const sparkDataVersion = 2

// processStart approximates when the process started: package
// initialisation runs before main.
var processStart = time.Now()

// usage converts a memory usage to its message.
func usage(u monitor.MemoryUsage) *sparkproto.MemoryUsage { return metric.MemoryProto(u) }

// cpuModel is the CPU's model name, read once.
var cpuModel = sync.OnceValue(func() string {
	infos, err := cpu.Info()
	if err != nil || len(infos) == 0 {
		return ""
	}
	return strings.TrimSpace(infos[0].ModelName)
})

// osInfo is the operating system's name (a Linux distribution's
// PRETTY_NAME), its version and the architecture, read once.
// spark: OperatingSystemInfo
var osInfo = sync.OnceValue(func() *sparkproto.SystemStatistics_Os {
	os := &sparkproto.SystemStatistics_Os{Arch: runtime.GOARCH, Name: runtime.GOOS}
	if info, err := host.Info(); err == nil {
		os.Version = info.KernelVersion
		if info.Platform != "" {
			os.Name = strings.TrimSpace(info.Platform + " " + info.PlatformVersion)
		}
	}
	if name := prettyName(); name != "" {
		os.Name = name
	}
	return os
})

func prettyName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for line := range strings.Lines(string(data)) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "PRETTY_NAME="); ok {
			return strings.TrimSpace(strings.ReplaceAll(v, `"`, " "))
		}
	}
	return ""
}

var homeDirs = []*regexp.Regexp{
	regexp.MustCompile(`C:\\Users\\\w+`), regexp.MustCompile(`/Users/\w+`), regexp.MustCompile(`/home/\w+`),
}

// redact hides user names in paths. spark: PlatformStatisticsProvider.VmArgRedactor
func redact(s string) string {
	s = homeDirs[0].ReplaceAllString(s, `C:\Users\<redacted>`)
	s = homeDirs[1].ReplaceAllString(s, "/Users/<redacted>")
	return homeDirs[2].ReplaceAllString(s, "/home/<redacted>")
}

// runtimeArgs are the Go runtime's settings and the server's flags, in place
// of the JVM's arguments.
func runtimeArgs() string {
	var args []string
	for _, env := range []string{"GOGC", "GOMEMLIMIT", "GOMAXPROCS", "GODEBUG"} {
		if v, ok := os.LookupEnv(env); ok {
			args = append(args, env+"="+v)
		}
	}
	return redact(strings.Join(append(args, os.Args[1:]...), " "))
}

// systemStatistics are the machine's and the runtime's.
// spark: PlatformStatisticsProvider.getSystemStatistics
func (p *Platform) systemStatistics() *sparkproto.SystemStatistics {
	cpuMon := p.Monitors.CPU
	s := &sparkproto.SystemStatistics{
		Cpu: &sparkproto.SystemStatistics_Cpu{
			Threads:      int32(runtime.NumCPU()),
			ProcessUsage: &sparkproto.SystemStatistics_Cpu_Usage{Last1M: cpuMon.Process1Min(), Last15M: cpuMon.Process15Min()},
			SystemUsage:  &sparkproto.SystemStatistics_Cpu_Usage{Last1M: cpuMon.System1Min(), Last15M: cpuMon.System15Min()},
			ModelName:    cpuModel(),
		},
		Memory: &sparkproto.SystemStatistics_Memory{
			Physical: &sparkproto.SystemStatistics_Memory_MemoryPool{},
			Swap:     &sparkproto.SystemStatistics_Memory_MemoryPool{},
		},
		Disk: &sparkproto.SystemStatistics_Disk{},
		Os:   osInfo(),
		Java: &sparkproto.SystemStatistics_Java{
			Vendor:        "The Go Authors",
			Version:       runtime.Version(),
			VendorVersion: runtime.Compiler + " " + runtime.GOOS + "/" + runtime.GOARCH,
			VmArgs:        runtimeArgs(),
		},
		Jvm:    &sparkproto.SystemStatistics_Jvm{Name: "Go runtime", Vendor: "The Go Authors", Version: runtime.Version()},
		Uptime: time.Since(processStart).Milliseconds(),
		Gc:     map[string]*sparkproto.SystemStatistics_Gc{},
		Net:    map[string]*sparkproto.SystemStatistics_NetInterface{},
	}
	if v, err := mem.VirtualMemory(); err == nil {
		s.Memory.Physical.Total, s.Memory.Physical.Used = int64(v.Total), int64(v.Total-v.Available)
	}
	if sw, err := mem.SwapMemory(); err == nil {
		s.Memory.Swap.Total, s.Memory.Swap.Used = int64(sw.Total), int64(sw.Total-sw.Free)
	}
	s.Disk.Used, s.Disk.Total = monitor.Disk()
	uptime := time.Duration(s.Uptime) * time.Millisecond
	for name, gc := range monitor.PollGC() {
		s.Gc[name] = &sparkproto.SystemStatistics_Gc{Total: gc.Count, AvgTime: gc.AvgTime(), AvgFrequency: float64(gc.AvgFrequency(uptime))}
	}
	for _, name := range p.Monitors.Network.Interfaces() {
		avg := p.Monitors.Network.Averages(name)
		s.Net[name] = &sparkproto.SystemStatistics_NetInterface{
			RxBytesPerSecond:   metric.AveragesProto(avg.RxBytes.Snapshot()),
			TxBytesPerSecond:   metric.AveragesProto(avg.TxBytes.Snapshot()),
			RxPacketsPerSecond: metric.AveragesProto(avg.RxPackets.Snapshot()),
			TxPacketsPerSecond: metric.AveragesProto(avg.TxPackets.Snapshot()),
		}
	}
	return s
}

// tickSnapshot is what only the tick goroutine may read, taken there for a
// report built elsewhere.
type tickSnapshot struct {
	players    int
	onlineMode sparkproto.PlatformStatistics_OnlineMode
	world      *sparkproto.WorldStatistics // nil when not asked for
}

// snapshot reads the players and, with world, the worlds' statistics; call
// it on the tick goroutine.
func (p *Platform) snapshot(world bool) tickSnapshot {
	players := p.Server.Players()
	s := tickSnapshot{players: len(players), onlineMode: sparkproto.PlatformStatistics_UNKNOWN}
	// Online-mode UUIDs are version 4, offline ones version 3.
	// spark: PlatformStatisticsProvider (online mode from any player's UUID)
	for _, pl := range players {
		if v := pl.UUID().Version(); v == 4 || v == 3 {
			s.onlineMode = sparkproto.PlatformStatistics_OFFLINE
			if v == 4 {
				s.onlineMode = sparkproto.PlatformStatistics_ONLINE
			}
			break
		}
	}
	if world {
		s.world = p.worldStatistics()
	}
	return s
}

// platformStatistics are the server's: memory, GC since initialGC, uptime,
// tick rate and durations, ping, players and, when snap has them, the
// worlds. spark: PlatformStatisticsProvider.getPlatformStatistics
func (p *Platform) platformStatistics(initialGC map[string]monitor.GCStats, snap tickSnapshot) *sparkproto.PlatformStatistics {
	alloc := p.Monitors.Allocation
	mem := &sparkproto.PlatformStatistics_Memory{
		Heap:            usage(monitor.HeapUsage()),
		NonHeap:         usage(monitor.NonHeapUsage()),
		AllocBpsLast1M:  metric.AveragesProto(alloc.Last1m.Snapshot()),
		AllocBpsLast5M:  metric.AveragesProto(alloc.Last5m.Snapshot()),
		AllocBpsLast15M: metric.AveragesProto(alloc.Last15m.Snapshot()),
	}
	for _, pool := range monitor.HeapPools() {
		u := pool.Usage
		if u.Max == -1 {
			u.Max = u.Committed
		}
		mem.Pools = append(mem.Pools, &sparkproto.PlatformStatistics_Memory_MemoryPool{
			Name: pool.Name, Usage: usage(u), CollectionUsage: usage(pool.CollectionUsage)})
	}
	uptime := time.Since(p.started)
	s := &sparkproto.PlatformStatistics{
		Memory:      mem,
		Uptime:      uptime.Milliseconds(),
		Gc:          map[string]*sparkproto.PlatformStatistics_Gc{},
		PlayerCount: int64(snap.players),
		OnlineMode:  snap.onlineMode,
		World:       snap.world,
	}
	if initialGC != nil {
		for name, gc := range monitor.PollGCSince(initialGC) {
			s.Gc[name] = &sparkproto.PlatformStatistics_Gc{Total: gc.Count, AvgTime: gc.AvgTime(), AvgFrequency: float64(gc.AvgFrequency(uptime))}
		}
	}
	t := p.Ticks
	s.Tps = &sparkproto.PlatformStatistics_Tps{Last1M: t.TPS1Min(), Last5M: t.TPS5Min(), Last15M: t.TPS15Min(), GameTargetTps: int32(t.TargetTPS())}
	if t.DurationSupported() {
		s.Mspt = &sparkproto.PlatformStatistics_Mspt{
			Last1M:           metric.AveragesProto(t.Duration1Min().Snapshot()),
			Last5M:           metric.AveragesProto(t.Duration5Min().Snapshot()),
			GameMaxIdealMspt: int32(t.MaxIdealDuration()),
		}
	}
	if avg := p.Ping.Average(); avg.Samples() != 0 {
		s.Ping = &sparkproto.PlatformStatistics_Ping{Last15M: metric.AveragesProto(avg.Snapshot())}
	}
	return s
}

// golemVersion answers the golem module the server was built from.
func golemVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	const golem = "github.com/golem-mc/golem"
	if info.Main.Path == golem {
		return info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path == golem {
			if dep.Replace != nil && dep.Replace.Version != "" {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return "unknown"
}

// platformMetadata describes golem to the viewer. spark: PlatformInfo
func (p *Platform) platformMetadata() *sparkproto.PlatformMetadata {
	mc, _ := p.Server.Version()
	return &sparkproto.PlatformMetadata{
		Type:             sparkproto.PlatformMetadata_SERVER,
		Name:             "golem",
		Brand:            "golem",
		Version:          golemVersion(),
		MinecraftVersion: mc,
		SparkDataVersion: sparkDataVersion,
		SparkVersion:     p.Version,
	}
}

// sources are the plugins built into the server, by lower-case name.
// spark: SparkPlugin.getKnownSources
func (p *Platform) sources() map[string]*sparkproto.PluginOrModMetadata {
	out := map[string]*sparkproto.PluginOrModMetadata{}
	for _, info := range p.Server.Plugins() {
		out[strings.ToLower(info.Name)] = &sparkproto.PluginOrModMetadata{Name: info.Name, Version: info.Version, Description: info.Module}
	}
	return out
}

// creatorProto is the command sender of a report.
func creatorProto(s Sender) *sparkproto.CommandSenderMetadata {
	m := &sparkproto.CommandSenderMetadata{Type: sparkproto.CommandSenderMetadata_OTHER, Name: s.Name}
	if s.IsPlayer() {
		m.Type, m.UniqueId = sparkproto.CommandSenderMetadata_PLAYER, s.UUID
	}
	return m
}

// metadata is what every upload carries about the server.
// spark: SparkMetadata
type metadata struct {
	creator   *sparkproto.CommandSenderMetadata
	platform  *sparkproto.PlatformMetadata
	stats     *sparkproto.PlatformStatistics
	system    *sparkproto.SystemStatistics
	generated int64
	configs   map[string]string
	sources   map[string]*sparkproto.PluginOrModMetadata
	metrics   *sparkproto.Metrics
}

// gatherMetadata collects the metadata with the tick goroutine's part in
// snap; safe from any goroutine. spark: SparkMetadata.gather
func (p *Platform) gatherMetadata(creator Sender, initialGC map[string]monitor.GCStats, snap tickSnapshot) metadata {
	return metadata{
		creator:   creatorProto(creator),
		platform:  p.platformMetadata(),
		stats:     p.platformStatistics(initialGC, snap),
		system:    p.systemStatistics(),
		generated: time.Now().UnixMilli(),
		configs:   serverConfigurations(),
		sources:   p.sources(),
		metrics:   p.Metrics.Proto(),
	}
}

func (m metadata) health() *sparkproto.HealthMetadata {
	return &sparkproto.HealthMetadata{Creator: m.creator, PlatformMetadata: m.platform, PlatformStatistics: m.stats,
		SystemStatistics: m.system, GeneratedTime: m.generated, ServerConfigurations: m.configs, Sources: m.sources, Metrics: m.metrics}
}

func (m metadata) heap() *sparkproto.HeapMetadata {
	return &sparkproto.HeapMetadata{Creator: m.creator, PlatformMetadata: m.platform, PlatformStatistics: m.stats,
		SystemStatistics: m.system, GeneratedTime: m.generated, ServerConfigurations: m.configs, Sources: m.sources, Metrics: m.metrics}
}

// writeSampler fills a profile's metadata; the generated time is its end.
func (m metadata) writeSampler(s *sparkproto.SamplerMetadata) {
	s.Creator, s.PlatformMetadata, s.PlatformStatistics, s.SystemStatistics = m.creator, m.platform, m.stats, m.system
	s.EndTime, s.ServerConfigurations, s.Sources, s.Metrics = m.generated, m.configs, m.sources, m.metrics
}
