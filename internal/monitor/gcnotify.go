package monitor

import (
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"time"
)

// GCInfo describes one finished collection. spark: GarbageCollectionNotificationInfo
type GCInfo struct {
	// Pause is the cycle's stop-the-world time.
	Pause time.Duration
	// End is when its last pause ended.
	End time.Time
	// Forced is a collection asked for (runtime.GC, debug.FreeOSMemory)
	// rather than started by the heap's growth or the two-minute timer.
	Forced bool
	// HeapBefore and HeapAfter are the Go heap's live and unswept objects
	// when the cycle began, estimated as the previous cycle's live heap plus
	// what was allocated since, and the live heap it marked. Zero when the
	// notice covers more than one cycle and this is not the last.
	HeapBefore, HeapAfter uint64
}

// GCMonitor calls its listeners after each collection, on its own goroutine.
// The runtime has no GC notifications: a cleanup attached to an object
// nothing references runs after the collection that frees it, and attaches
// itself to a new one. spark: GarbageCollectionMonitor
type GCMonitor struct {
	mu        sync.Mutex
	listeners []func(GCInfo)
	closed    bool

	numGC, forced uint64
	live, allocs  uint64
}

type gcSentinel struct {
	_ *int // a pointer keeps it out of the tiny allocator
	_ [16]byte
}

var gcSamples = []string{"/gc/cycles/total:gc-cycles", "/gc/cycles/forced:gc-cycles", "/gc/heap/live:bytes", "/gc/heap/allocs:bytes"}

func readGCMetrics() (cycles, forced, live, allocs uint64) {
	s := make([]metrics.Sample, len(gcSamples))
	for i, name := range gcSamples {
		s[i].Name = name
	}
	metrics.Read(s)
	return s[0].Value.Uint64(), s[1].Value.Uint64(), s[2].Value.Uint64(), s[3].Value.Uint64()
}

// NewGCMonitor starts watching for collections.
func NewGCMonitor() *GCMonitor {
	m := &GCMonitor{}
	m.numGC, m.forced, m.live, m.allocs = readGCMetrics()
	m.arm()
	return m
}

func (m *GCMonitor) arm() {
	runtime.AddCleanup(&gcSentinel{}, func(m *GCMonitor) { m.collected() }, m)
}

// AddListener adds a function called after each collection.
func (m *GCMonitor) AddListener(f func(GCInfo)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, f)
}

// Close stops the notifications; a listener may still be running.
func (m *GCMonitor) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

func (m *GCMonitor) collected() {
	cycles, forced, live, allocs := readGCMetrics()
	var stats debug.GCStats
	stats.PauseEnd = make([]time.Time, 256)
	debug.ReadGCStats(&stats)

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.arm()
	n := int(cycles - m.numGC)
	forcedNew := int(forced - m.forced)
	before := m.live + (allocs - m.allocs)
	m.numGC, m.forced, m.live, m.allocs = cycles, forced, live, allocs
	listeners := append([]func(GCInfo){}, m.listeners...)
	m.mu.Unlock()

	// Pause and PauseEnd list the latest cycles first.
	n = min(n, len(stats.Pause), len(stats.PauseEnd))
	for i := n - 1; i >= 0; i-- {
		info := GCInfo{Pause: stats.Pause[i], End: stats.PauseEnd[i], Forced: i < forcedNew}
		if i == 0 {
			info.HeapBefore, info.HeapAfter = before, live
		}
		for _, l := range listeners {
			l(info)
		}
	}
}
