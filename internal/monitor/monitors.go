package monitor

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/golem-mc/spark/internal/metric"
)

// Monitors runs the polling monitors on their own goroutines until Close.
// spark: MonitoringExecutor with CpuMonitor, MemoryMonitor,
// MemoryAllocationInfo and NetworkMonitor
type Monitors struct {
	CPU        *CPU
	Network    *Network
	Allocation *Allocation

	stop chan struct{}
	wg   sync.WaitGroup
}

// Start begins polling the CPU every second, memory every 10 seconds and the
// network every minute, recording into metrics.
func Start(metrics *metric.Metrics) *Monitors {
	m := &Monitors{CPU: newCPU(), Network: newNetwork(), Allocation: newAllocation(), stop: make(chan struct{})}
	m.every(time.Second, func() {
		now := time.Now()
		system, process := m.CPU.poll()
		if system >= 0 && metric.Due(metrics, &metrics.CPUSystem, now) {
			metrics.CPUSystem.Record(now.UnixMilli(), system)
		}
		if process >= 0 && metric.Due(metrics, &metrics.CPUProcess, now) {
			metrics.CPUProcess.Record(now.UnixMilli(), process)
		}
	})
	m.every(metric.Interval, func() {
		now := time.Now().UnixMilli()
		metrics.HeapUsage.Record(now, HeapUsage())
		metrics.NonHeapUsage.Record(now, NonHeapUsage())
		if bps, ok := m.Allocation.poll(); ok {
			metrics.Allocation.Record(now, bps)
		}
	})
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
