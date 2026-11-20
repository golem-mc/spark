package metric

import (
	"slices"
	"testing"
	"time"
)

func TestSeries(t *testing.T) {
	var s Series[float64]
	base := time.Now().UnixMilli()
	s.Record(base, 1)
	s.Record(base+10_000, 2)
	s.Record(base+int64(time.Hour/time.Millisecond)+5_000, 3) // drops the first, older than an hour
	start, deltas, values := s.export()
	if start != base+10_000 || !slices.Equal(deltas, []uint32{0, uint32(time.Hour/time.Millisecond) - 5_000}) || !slices.Equal(values, []float64{2, 3}) {
		t.Errorf("export = %d %v %v", start, deltas, values)
	}
}

func TestDue(t *testing.T) {
	m := New()
	now := time.Now()
	if Due(m, &m.TPS, now) {
		t.Error("nothing is due in the first interval")
	}
	later := now.Add(Interval + time.Second)
	if !Due(m, &m.TPS, later) {
		t.Error("an empty series is due after the first interval")
	}
	m.TPS.Record(later.UnixMilli(), 20)
	if Due(m, &m.TPS, later.Add(time.Second)) || !Due(m, &m.TPS, later.Add(Interval+time.Millisecond)) {
		t.Error("due an interval after the last sample")
	}
	if p := m.Proto(); p.Tps == nil || p.Tps.Values[0] != 20 || p.CpuUsageProcess != nil {
		t.Errorf("proto = %v", p)
	}
}
