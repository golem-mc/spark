package monitor

import (
	"runtime"
	"testing"
	"time"
)

func TestGCMonitor(t *testing.T) {
	m := NewGCMonitor()
	defer m.Close()
	got := make(chan GCInfo, 16)
	m.AddListener(func(info GCInfo) { got <- info })
	runtime.GC()
	select {
	case info := <-got:
		if !info.Forced || info.Pause <= 0 || info.End.IsZero() || info.HeapAfter == 0 || info.HeapBefore == 0 {
			t.Errorf("info = %+v", info)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notice of the collection")
	}
}

func TestGCStatsAverages(t *testing.T) {
	s := GCStats{Count: 4, Time: 200 * time.Microsecond}
	if s.AvgTime() != 0.05 || s.AvgFrequency(time.Second) != 249 {
		t.Errorf("avg time %v, frequency %v", s.AvgTime(), s.AvgFrequency(time.Second))
	}
}
