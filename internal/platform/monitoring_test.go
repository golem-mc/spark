package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/monitor"
)

func TestTickMonitor(t *testing.T) {
	var got []string
	m := &tickMonitor{send: func(msg plugin.Text) { got = append(got, msg.PlainString()) }, pred: reportPredicate{percentage: true, threshold: 100}, zeroTick: 1000}
	now := time.Unix(1000, 0)
	tick := int64(1000)
	step := func(d time.Duration) {
		now = now.Add(d)
		tick++
		m.onTick(tick, now)
	}
	m.onTick(tick, now)
	for range setupTicks / 2 {
		step(50 * time.Millisecond)
	}
	m.onGC(monitor.GCInfo{Pause: time.Millisecond}) // during setup: not reported, and the tick is left out
	step(500 * time.Millisecond)
	for range setupTicks / 2 {
		step(50 * time.Millisecond)
	}
	step(150 * time.Millisecond)
	step(90 * time.Millisecond)
	m.onGC(monitor.GCInfo{Pause: 1234 * time.Microsecond})
	want := []string{
		"Tick monitor started. Before the monitor becomes fully active, the server's average tick rate will be calculated over a period of 120 ticks (approx 6 seconds).",
		"Analysis is now complete.",
		"> Max: 50ms", "> Min: 50ms", "> Average: 50ms",
		"Starting now, any ticks with >100.0% increase in duration compared to the average will be reported.",
		"Tick #122 lasted 150 ms. (200% increase from avg)",
		"Tick #123 included GC lasting 1.23 ms. (type = Go)",
	}
	for i := range got {
		got[i] = strings.TrimPrefix(got[i], "[⚡] ")
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestReportPredicate(t *testing.T) {
	ticks := reportPredicate{threshold: 60}
	if !ticks.shouldReport(70, 20, 40) || ticks.shouldReport(70, -1, 0) || ticks.shouldReport(55, 5, 10) {
		t.Error("duration threshold")
	}
	if got := ticks.startMessage().PlainString(); got != "Starting now, any ticks with duration >60.0 will be reported." {
		t.Error(got)
	}
}

func TestGCReport(t *testing.T) {
	join := func(ts []plugin.Text) string {
		var out []string
		for _, x := range ts {
			out = append(out, x.PlainString())
		}
		return strings.Join(out, "\n")
	}
	got := join(gcReport(monitor.GCInfo{Pause: 250 * time.Microsecond, Forced: true, HeapBefore: 40 << 20, HeapAfter: 10 << 20}))
	want := "[⚡] Go GC lasting 0.25 ms. (cause = forced)\n  30.0 MB freed from Go heap\n    40.0 MB → 10.0 MB (75%)"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
