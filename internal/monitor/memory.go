package monitor

import (
	"math"
	"runtime/metrics"

	"github.com/shirou/gopsutil/v4/mem"
)

// MemoryUsage is a memory area's usage in bytes; Max is -1 when the area has
// no limit. spark: java.lang.management.MemoryUsage
type MemoryUsage struct {
	Init, Used, Committed, Max int64
}

// MemoryPool is one memory area with its usage after the last collection.
type MemoryPool struct {
	Name                   string
	Usage, CollectionUsage MemoryUsage
}

const (
	mHeapObjects  = "/memory/classes/heap/objects:bytes"
	mHeapUnused   = "/memory/classes/heap/unused:bytes"
	mHeapFree     = "/memory/classes/heap/free:bytes"
	mHeapStacks   = "/memory/classes/heap/stacks:bytes"
	mOSStacks     = "/memory/classes/os-stacks:bytes"
	mMetaMCache   = "/memory/classes/metadata/mcache/inuse:bytes"
	mMetaMSpan    = "/memory/classes/metadata/mspan/inuse:bytes"
	mMetaOther    = "/memory/classes/metadata/other:bytes"
	mProfBuckets  = "/memory/classes/profiling/buckets:bytes"
	mOther        = "/memory/classes/other:bytes"
	mLive         = "/gc/heap/live:bytes"
	mMemoryLimit  = "/gc/gomemlimit:bytes"
	mHeapGoal     = "/gc/heap/goal:bytes"
	mAllocedBytes = "/gc/heap/allocs:bytes"
)

var memoryMetrics = []string{mHeapObjects, mHeapUnused, mHeapFree, mHeapStacks, mOSStacks,
	mMetaMCache, mMetaMSpan, mMetaOther, mProfBuckets, mOther, mLive, mMemoryLimit, mHeapGoal, mAllocedBytes}

// readMemory reads the runtime's memory metrics by name.
func readMemory() map[string]int64 {
	samples := make([]metrics.Sample, len(memoryMetrics))
	for i, name := range memoryMetrics {
		samples[i].Name = name
	}
	metrics.Read(samples)
	out := make(map[string]int64, len(samples))
	for _, s := range samples {
		if s.Value.Kind() == metrics.KindUint64 {
			out[s.Name] = int64(min(s.Value.Uint64(), math.MaxInt64))
		}
	}
	return out
}

// heapMax is the heap's limit: GOMEMLIMIT when one is set, else the machine's
// physical memory, since a Go heap otherwise grows until the OS refuses.
func heapMax(m map[string]int64) int64 {
	if limit := m[mMemoryLimit]; limit > 0 && limit < math.MaxInt64 {
		return limit
	}
	if v, err := mem.VirtualMemory(); err == nil && v.Total > 0 {
		return int64(v.Total)
	}
	return -1
}

// HeapUsage is the Go heap: live and not yet swept objects used, the heap
// spans the runtime holds from the OS committed. spark: MemoryMXBean.getHeapMemoryUsage
func HeapUsage() MemoryUsage {
	m := readMemory()
	return heapUsage(m)
}

func heapUsage(m map[string]int64) MemoryUsage {
	used := m[mHeapObjects]
	committed := used + m[mHeapUnused] + m[mHeapFree]
	return MemoryUsage{Used: used, Committed: committed, Max: heapMax(m)}
}

// NonHeapUsage is the runtime's memory outside the heap's objects:
// goroutine and OS stacks, allocator metadata, profiling buckets and the
// rest. spark: MemoryMXBean.getNonHeapMemoryUsage
func NonHeapUsage() MemoryUsage {
	m := readMemory()
	used := m[mHeapStacks] + m[mOSStacks] + m[mMetaMCache] + m[mMetaMSpan] + m[mMetaOther] + m[mProfBuckets] + m[mOther]
	return MemoryUsage{Used: used, Committed: used, Max: -1}
}

// HeapPools are the heap's areas, the counterpart of the JVM's heap memory
// pools: the object heap, with the live heap the last collection left as
// its collection usage, and the goroutine stacks.
func HeapPools() []MemoryPool {
	m := readMemory()
	heap := heapUsage(m)
	stacks := MemoryUsage{Used: m[mHeapStacks], Committed: m[mHeapStacks], Max: m[mHeapStacks]}
	return []MemoryPool{
		{Name: "Go heap", Usage: heap, CollectionUsage: MemoryUsage{Used: m[mLive], Committed: heap.Committed, Max: heap.Max}},
		{Name: "Goroutine stacks", Usage: stacks, CollectionUsage: stacks},
	}
}

// AllocatedBytes is the total the heap has allocated since the process
// started.
func AllocatedBytes() int64 { return readMemory()[mAllocedBytes] }
