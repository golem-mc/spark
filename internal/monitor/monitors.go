package monitor

import (
	"math/rand/v2"
	"sync"
	"time"
)

// Monitors runs the polling monitors on their own goroutines until Close.
// spark: MonitoringExecutor with CpuMonitor and NetworkMonitor
type Monitors struct {
	CPU     *CPU
	Network *Network

	stop chan struct{}
	wg   sync.WaitGroup
}

// Start begins polling the CPU every second and the network every minute.
func Start() *Monitors {
	m := &Monitors{CPU: newCPU(), Network: newNetwork(), stop: make(chan struct{})}
	m.every(time.Second, func() { m.CPU.poll() })
	m.every(netPollSeconds*time.Second, m.Network.poll)
	return m
}

// every runs fn each period, the first time after a random delay of up to
// period (at most 10s), so the monitors do not all poll at once.
func (m *Monitors) every(period time.Duration, fn func()) {
	m.wg.Go(func() {
		delay := time.Duration(rand.Int64N(int64(min(period, 10*time.Second))))
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-m.stop:
			return
		}
		fn()
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fn()
			case <-m.stop:
				return
			}
		}
	})
}

// Close stops polling and waits for the pollers to return.
func (m *Monitors) Close() {
	close(m.stop)
	m.wg.Wait()
}
