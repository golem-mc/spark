// Package monitor holds spark's background monitors: tick rate and duration,
// CPU, memory, network, disk and player ping.
package monitor

import (
	"math"
	"sync"
	"time"

	"github.com/golem-mc/spark/internal/rolling"
)

const (
	// tpsSampleInterval is how many ticks one TPS sample spans.
	tpsSampleInterval = 20
	ticksPerSecond    = 20
)

// TickStatistics measures the tick rate from tick starts and tick durations
// from tick ends. Feed it from the tick goroutine; read it from any.
// spark: SparkTickStatistics
type TickStatistics struct {
	mu   sync.Mutex
	tps  [5]*tpsAverage // 5s, 10s, 1m, 5m, 15m
	last time.Time

	durationSupported bool
	// durations are in milliseconds over 10s, 1m and 5m of ticks.
	duration10s, duration1m, duration5m *rolling.Average

	target int
}

// NewTickStatistics answers statistics for a server targeting 20 ticks a second.
func NewTickStatistics() *TickStatistics {
	t := &TickStatistics{
		duration10s: rolling.New(ticksPerSecond * 10),
		duration1m:  rolling.New(ticksPerSecond * 60),
		duration5m:  rolling.New(ticksPerSecond * 60 * 5),
		target:      ticksPerSecond,
	}
	for i, secs := range []int{5, 10, 60, 5 * 60, 15 * 60} {
		t.tps[i] = newTPSAverage(secs)
	}
	return t
}

// OnTickStart records the start of tick number tick at now, with the
// server's target rate. spark: SparkTickStatistics.onTick(int)
func (t *TickStatistics) OnTickStart(tick int64, now time.Time, targetRate float32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.target = max(1, int(math.Round(float64(targetRate))))
	if tick%tpsSampleInterval != 0 {
		return
	}
	if t.last.IsZero() {
		t.last = now
		return
	}
	diff := now.Sub(t.last)
	if diff <= 0 {
		return
	}
	current := float64(tpsSampleInterval) * float64(time.Second) / float64(diff)
	for _, avg := range t.tps {
		avg.add(current, diff)
	}
	t.last = now
}

// OnTickEnd records a tick's duration in milliseconds.
// spark: SparkTickStatistics.onTick(double)
func (t *TickStatistics) OnTickEnd(mspt float64) {
	t.mu.Lock()
	t.durationSupported = true
	t.mu.Unlock()
	t.duration10s.Add(mspt)
	t.duration1m.Add(mspt)
	t.duration5m.Add(mspt)
}

// TargetTPS is the tick rate the server aims for.
func (t *TickStatistics) TargetTPS() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.target
}

// MaxIdealDuration is the longest a tick may take at the target rate, in
// milliseconds.
func (t *TickStatistics) MaxIdealDuration() int { return 1000 / t.TargetTPS() }

func (t *TickStatistics) tpsAt(i int) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tps[i].average()
}

func (t *TickStatistics) TPS5Sec() float64  { return t.tpsAt(0) }
func (t *TickStatistics) TPS10Sec() float64 { return t.tpsAt(1) }
func (t *TickStatistics) TPS1Min() float64  { return t.tpsAt(2) }
func (t *TickStatistics) TPS5Min() float64  { return t.tpsAt(3) }
func (t *TickStatistics) TPS15Min() float64 { return t.tpsAt(4) }

// DurationSupported reports whether a tick end has been seen.
func (t *TickStatistics) DurationSupported() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.durationSupported
}

// Duration10Sec, Duration1Min and Duration5Min are tick durations in
// milliseconds.
func (t *TickStatistics) Duration10Sec() *rolling.Average { return t.duration10s }
func (t *TickStatistics) Duration1Min() *rolling.Average  { return t.duration1m }
func (t *TickStatistics) Duration5Min() *rolling.Average  { return t.duration5m }

// tpsAverage is a time-weighted average of TPS samples, starting full of
// 20 TPS one-second samples. spark: SparkTickStatistics.TpsRollingAverage
type tpsAverage struct {
	samples []float64
	times   []time.Duration
	index   int
	time    time.Duration
	total   float64 // sum of sample * nanoseconds
}

func newTPSAverage(size int) *tpsAverage {
	a := &tpsAverage{samples: make([]float64, size), times: make([]time.Duration, size)}
	for i := range size {
		a.samples[i] = ticksPerSecond
		a.times[i] = time.Second
	}
	a.time = time.Duration(size) * time.Second
	a.total = ticksPerSecond * float64(a.time)
	return a
}

func (a *tpsAverage) add(x float64, t time.Duration) {
	a.time -= a.times[a.index]
	a.total -= a.samples[a.index] * float64(a.times[a.index])
	a.samples[a.index] = x
	a.times[a.index] = t
	a.time += t
	a.total += x * float64(t)
	a.index = (a.index + 1) % len(a.samples)
}

func (a *tpsAverage) average() float64 { return a.total / float64(a.time) }
