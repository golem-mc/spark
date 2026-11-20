// Package rolling keeps rolling averages of the last N samples.
package rolling

import (
	"math"
	"slices"
	"sync"
)

// Average is a rolling average over the last Size samples, safe from any
// goroutine. spark: RollingAverage (with float64 in place of BigDecimal)
type Average struct {
	mu      sync.Mutex
	size    int
	samples []float64 // ring, oldest at next once full
	next    int
	total   float64
}

// New answers an average of the last size samples.
func New(size int) *Average { return &Average{size: size, samples: make([]float64, 0, size)} }

// Add records a sample, dropping the oldest once the window is full.
func (a *Average) Add(v float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.samples) < a.size {
		a.samples = append(a.samples, v)
		a.total += v
		return
	}
	a.total += v - a.samples[a.next]
	a.samples[a.next] = v
	a.next = (a.next + 1) % a.size
}

// Samples answers how many samples the window holds.
func (a *Average) Samples() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.samples)
}

// Mean answers the mean, 0 with no samples.
func (a *Average) Mean() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.samples) == 0 {
		return 0
	}
	return a.total / float64(len(a.samples))
}

// Max answers the largest sample, 0 with none.
func (a *Average) Max() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.samples) == 0 {
		return 0
	}
	return slices.Max(a.samples)
}

// Min answers the smallest sample, 0 with none.
func (a *Average) Min() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.samples) == 0 {
		return 0
	}
	return slices.Min(a.samples)
}

// Median is the 50th percentile.
func (a *Average) Median() float64 { return a.Percentile(0.5) }

// Percentile95 is the 95th percentile.
func (a *Average) Percentile95() float64 { return a.Percentile(0.95) }

// Percentile answers the sample at rank ceil(p * (n-1)) of the sorted
// window, p in [0, 1]. spark: RollingAverage.percentile
func (a *Average) Percentile(p float64) float64 {
	a.mu.Lock()
	sorted := slices.Clone(a.samples)
	a.mu.Unlock()
	if len(sorted) == 0 {
		return 0
	}
	slices.Sort(sorted)
	return sorted[int(math.Ceil(p*float64(len(sorted)-1)))]
}

// Snapshot answers the window's statistics as plain values.
func (a *Average) Snapshot() Values {
	return Values{MeanV: a.Mean(), MaxV: a.Max(), MinV: a.Min(), MedianV: a.Median(), P95: a.Percentile95()}
}

// Values is a fixed set of averages. spark: ImmutableDoubleAverageInfo
type Values struct {
	MeanV, MaxV, MinV, MedianV, P95 float64
}

func (v Values) Mean() float64         { return v.MeanV }
func (v Values) Max() float64          { return v.MaxV }
func (v Values) Min() float64          { return v.MinV }
func (v Values) Median() float64       { return v.MedianV }
func (v Values) Percentile95() float64 { return v.P95 }
