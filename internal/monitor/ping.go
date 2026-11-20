package monitor

import (
	"math"
	"slices"
	"strings"

	"github.com/golem-mc/spark/internal/rolling"
)

const (
	// PingQuerySeconds is how often player pings are read; the average spans
	// 15 minutes of readings.
	PingQuerySeconds = 10
	pingWindow       = 15 * 60 / PingQuerySeconds
)

// PingSummary is the pings of the players online at one moment, in
// milliseconds. spark: PingSummary
type PingSummary struct {
	values []int // sorted
	total  int
}

// NewPingSummary summarises pings; players with no ping measured yet (0)
// are left out, and none at all gives a summary of a single 0.
func NewPingSummary(pings []int) PingSummary {
	values := slices.DeleteFunc(slices.Clone(pings), func(p int) bool { return p <= 0 })
	if len(values) == 0 {
		values = []int{0}
	}
	slices.Sort(values)
	s := PingSummary{values: values}
	for _, v := range values {
		s.total += v
	}
	return s
}

func (s PingSummary) Total() int      { return s.total }
func (s PingSummary) Mean() float64   { return float64(s.total) / float64(len(s.values)) }
func (s PingSummary) Max() float64    { return float64(s.values[len(s.values)-1]) }
func (s PingSummary) Min() float64    { return float64(s.values[0]) }
func (s PingSummary) Median() float64 { return s.percentile(0.5) }

func (s PingSummary) Percentile95() float64 { return s.percentile(0.95) }

func (s PingSummary) percentile(p float64) float64 {
	return float64(s.values[int(math.Ceil(p*float64(len(s.values)-1)))])
}

// Values answers the summary as plain averages.
func (s PingSummary) Values() rolling.Values {
	return rolling.Values{MeanV: s.Mean(), MaxV: s.Max(), MinV: s.Min(), MedianV: s.Median(), P95: s.Percentile95()}
}

// PlayerPing is one player's ping in milliseconds.
type PlayerPing struct {
	Name string
	Ping int
}

// PingStatistics averages the players' median ping. spark: PingStatistics
type PingStatistics struct {
	average *rolling.Average
}

// NewPingStatistics answers empty statistics.
func NewPingStatistics() *PingStatistics {
	return &PingStatistics{average: rolling.New(pingWindow)}
}

// Record adds one reading of every online player's ping; no players, no
// sample.
func (p *PingStatistics) Record(pings []PlayerPing) PingSummary {
	s := Summarise(pings)
	if s.Total() != 0 {
		p.average.Add(s.Median())
	}
	return s
}

// Average is the median pings over the last 15 minutes.
func (p *PingStatistics) Average() *rolling.Average { return p.average }

// Summarise summarises pings.
func Summarise(pings []PlayerPing) PingSummary {
	values := make([]int, len(pings))
	for i, pp := range pings {
		values[i] = pp.Ping
	}
	return NewPingSummary(values)
}

// FindPing answers a player's ping by name, exactly first and then ignoring
// case. spark: PingStatistics.query
func FindPing(pings []PlayerPing, name string) (PlayerPing, bool) {
	for _, p := range pings {
		if p.Name == name {
			return p, true
		}
	}
	for _, p := range pings {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return PlayerPing{}, false
}
