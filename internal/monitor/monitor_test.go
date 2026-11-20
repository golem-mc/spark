package monitor

import (
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/golem-mc/spark/internal/metric"
)

func TestTickStatistics(t *testing.T) {
	s := NewTickStatistics(metric.New())
	if s.TPS5Sec() != 20 || s.DurationSupported() {
		t.Fatalf("fresh statistics: tps=%v durations=%v", s.TPS5Sec(), s.DurationSupported())
	}
	start := time.Unix(1000, 0)
	// 20 ticks every 2 seconds: 10 TPS.
	for tick := int64(0); tick <= 100; tick++ {
		s.OnTickStart(tick, start.Add(time.Duration(tick)*100*time.Millisecond), 20)
		s.OnTickEnd(float64(tick % 10))
	}
	if got := s.TPS5Sec(); math.Abs(got-10) > 1e-9 {
		t.Errorf("tps 5s = %v, want 10", got)
	}
	// 1m starts full of one-second 20 TPS samples; five of them are replaced
	// by two-second 10 TPS ones, weighted by their time.
	if got, want := s.TPS1Min(), (55.0*20+5*2*10)/65; math.Abs(got-want) > 1e-9 {
		t.Errorf("tps 1m = %v, want %v", got, want)
	}
	if !s.DurationSupported() || s.Duration10Sec().Max() != 9 || s.Duration10Sec().Min() != 0 {
		t.Error("durations not recorded")
	}
	if s.MaxIdealDuration() != 50 {
		t.Error("max ideal duration")
	}
	s.OnTickStart(101, start, 40)
	if s.TargetTPS() != 40 || s.MaxIdealDuration() != 25 {
		t.Error("target follows the tick rate")
	}
}

func TestPingSummary(t *testing.T) {
	s := Summarise([]PlayerPing{{"a", 0}, {"b", 40}, {"c", 20}, {"d", 300}})
	if s.Total() != 360 || s.Min() != 20 || s.Max() != 300 || s.Median() != 40 || s.Percentile95() != 300 {
		t.Errorf("summary = %+v", s.Values())
	}
	if empty := Summarise(nil); empty.Total() != 0 || empty.Max() != 0 {
		t.Error("empty summary")
	}
	stats := NewPingStatistics(metric.New())
	stats.Record(nil)
	if stats.Average().Samples() != 0 {
		t.Error("no players, no sample")
	}
	stats.Record([]PlayerPing{{"a", 30}})
	if stats.Average().Mean() != 30 {
		t.Error("median recorded")
	}
	if p, ok := FindPing([]PlayerPing{{"Alice", 5}}, "alice"); !ok || p.Name != "Alice" {
		t.Error("case-insensitive lookup")
	}
}

func TestMemory(t *testing.T) {
	runtime.GC() // the live heap is measured by a collection
	heap := HeapUsage()
	if heap.Used <= 0 || heap.Committed < heap.Used || heap.Max <= 0 {
		t.Errorf("heap = %+v", heap)
	}
	if NonHeapUsage().Used <= 0 {
		t.Error("non-heap")
	}
	if pools := HeapPools(); len(pools) == 0 || pools[0].CollectionUsage.Used <= 0 {
		t.Errorf("pools = %+v", pools)
	}
}
