package monitor

import (
	"runtime/debug"
	"time"
)

// GCName is the name the Go collector is reported under, where a JVM lists
// its collectors ("G1 Young Generation", ...).
const GCName = "Go"

// GCStats are a collector's collections and their total stop-the-world
// pause time. spark: GarbageCollectorStatistics
type GCStats struct {
	Count int64
	Time  time.Duration
}

// PollGC answers the collectors' statistics since the process started.
func PollGC() map[string]GCStats {
	var s debug.GCStats
	debug.ReadGCStats(&s)
	return map[string]GCStats{GCName: {Count: s.NumGC, Time: s.PauseTotal}}
}

// PollGCSince answers the statistics since initial was polled.
func PollGCSince(initial map[string]GCStats) map[string]GCStats {
	out := PollGC()
	for name, s := range out {
		if i, ok := initial[name]; ok {
			out[name] = GCStats{Count: s.Count - i.Count, Time: s.Time - i.Time}
		}
	}
	return out
}

// AvgTime is the mean pause per collection in milliseconds, 0 with none.
func (s GCStats) AvgTime() float64 {
	if s.Count == 0 {
		return 0
	}
	return float64(s.Time.Milliseconds()) / float64(s.Count)
}

// AvgFrequency is the mean milliseconds between collections over uptime.
func (s GCStats) AvgFrequency(uptime time.Duration) int64 {
	if s.Count == 0 {
		return 0
	}
	return int64((float64(uptime.Milliseconds()) - float64(s.Time.Milliseconds())) / float64(s.Count))
}
