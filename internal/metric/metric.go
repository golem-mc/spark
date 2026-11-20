// Package metric keeps the last hour of spark's time series (TPS, tick
// durations, CPU, memory, allocation, world counts and ping), sampled about
// every 10 seconds, for the viewer's graphs. spark: Metrics, MetricSeries
package metric

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/golem-mc/spark/internal/rolling"
	"github.com/golem-mc/spark/internal/sparkproto"
)

const (
	retention = time.Hour
	// Interval is how often a series takes a sample.
	Interval = 10 * time.Second
)

// Series is a ring of timestamped values, dropping those older than an hour.
// Safe from any goroutine. spark: MetricSeries
type Series[T any] struct {
	mu     sync.Mutex
	times  []int64 // unix milliseconds, in order
	values []T
	newest atomic.Int64
}

// Record adds a value at timestamp ms (unix milliseconds).
func (s *Series[T]) Record(ms int64, v T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := ms - retention.Milliseconds()
	drop := 0
	for drop < len(s.times) && s.times[drop] < cutoff {
		drop++
	}
	if drop > 0 {
		s.times = append(s.times[:0], s.times[drop:]...)
		s.values = append(s.values[:0], s.values[drop:]...)
	}
	s.times = append(s.times, ms)
	s.values = append(s.values, v)
	s.newest.Store(ms)
}

// Len answers how many values the series holds.
func (s *Series[T]) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.times)
}

// export answers the start timestamp, the deltas between timestamps (the
// first 0) and the values.
func (s *Series[T]) export() (start int64, deltas []uint32, values []T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deltas = make([]uint32, len(s.times))
	for i, t := range s.times {
		if i == 0 {
			start = t
			continue
		}
		deltas[i] = uint32(t - s.times[i-1])
	}
	return start, deltas, append([]T(nil), s.values...)
}

// MemoryUsage is a memory area's usage in bytes.
type MemoryUsage struct{ Init, Used, Committed, Max int64 }

// WorldCounts are the players, entities, block entities and loaded chunks
// across all worlds.
type WorldCounts struct{ Players, Entities, TileEntities, Chunks int32 }

// Metrics are every series spark records. spark: Metrics
type Metrics struct {
	start time.Time

	CPUProcess, CPUSystem    Series[float64]
	HeapUsage, NonHeapUsage  Series[MemoryUsage]
	Allocation               Series[float64]
	TPS                      Series[float64]
	TickDuration, PlayerPing Series[rolling.Values]
	WorldInfo                Series[WorldCounts]
}

// New answers empty metrics, which start recording one interval from now.
func New() *Metrics { return &Metrics{start: time.Now()} }

// Due reports whether series s should take a sample at now: an interval after
// the metrics started and an interval after its last sample.
func Due[T any](m *Metrics, s *Series[T], now time.Time) bool {
	if now.Before(m.start.Add(Interval)) {
		return false
	}
	newest := s.newest.Load()
	return newest == 0 || newest < now.UnixMilli()-Interval.Milliseconds()
}

func averagesProto(v rolling.Values) *sparkproto.RollingAverageValues {
	return &sparkproto.RollingAverageValues{Mean: v.MeanV, Max: v.MaxV, Min: v.MinV, Median: v.MedianV, Percentile95: v.P95}
}

// AveragesProto converts averages to their message.
func AveragesProto(v rolling.Values) *sparkproto.RollingAverageValues { return averagesProto(v) }

// MemoryProto converts a memory usage to its message.
func MemoryProto(u MemoryUsage) *sparkproto.MemoryUsage {
	return &sparkproto.MemoryUsage{Used: u.Used, Committed: u.Committed, Init: u.Init, Max: u.Max}
}

func doubles(s *Series[float64]) *sparkproto.DoubleMetricSeries {
	start, deltas, values := s.export()
	return &sparkproto.DoubleMetricSeries{StartTimestampMs: start, TimestampDeltasMs: deltas, Values: values}
}

func averages(s *Series[rolling.Values]) *sparkproto.AveragesMetricSeries {
	start, deltas, values := s.export()
	out := &sparkproto.AveragesMetricSeries{StartTimestampMs: start, TimestampDeltasMs: deltas}
	for _, v := range values {
		out.Values = append(out.Values, averagesProto(v))
	}
	return out
}

func memory(s *Series[MemoryUsage]) *sparkproto.MemoryUsageMetricSeries {
	start, deltas, values := s.export()
	out := &sparkproto.MemoryUsageMetricSeries{StartTimestampMs: start, TimestampDeltasMs: deltas}
	for _, v := range values {
		out.Values = append(out.Values, MemoryProto(v))
	}
	return out
}

func worldInfo(s *Series[WorldCounts]) *sparkproto.WorldInfoMetricSeries {
	start, deltas, values := s.export()
	out := &sparkproto.WorldInfoMetricSeries{StartTimestampMs: start, TimestampDeltasMs: deltas}
	for _, v := range values {
		out.Values = append(out.Values, &sparkproto.WorldInfoMetricSeries_Values{
			Players: v.Players, Entities: v.Entities, TileEntities: v.TileEntities, Chunks: v.Chunks})
	}
	return out
}

// Proto exports the series that hold samples. spark: Metrics.exportProto
func (m *Metrics) Proto() *sparkproto.Metrics {
	out := &sparkproto.Metrics{}
	if m.CPUProcess.Len() > 0 {
		out.CpuUsageProcess = doubles(&m.CPUProcess)
	}
	if m.CPUSystem.Len() > 0 {
		out.CpuUsageSystem = doubles(&m.CPUSystem)
	}
	if m.HeapUsage.Len() > 0 {
		out.MemoryUsageHeap = memory(&m.HeapUsage)
	}
	if m.NonHeapUsage.Len() > 0 {
		out.MemoryUsageNonHeap = memory(&m.NonHeapUsage)
	}
	if m.Allocation.Len() > 0 {
		out.MemoryAllocation = doubles(&m.Allocation)
	}
	if m.TPS.Len() > 0 {
		out.Tps = doubles(&m.TPS)
	}
	if m.TickDuration.Len() > 0 {
		out.TickDuration = averages(&m.TickDuration)
	}
	if m.WorldInfo.Len() > 0 {
		out.WorldInfo = worldInfo(&m.WorldInfo)
	}
	if m.PlayerPing.Len() > 0 {
		out.PlayerPing = averages(&m.PlayerPing)
	}
	return out
}
