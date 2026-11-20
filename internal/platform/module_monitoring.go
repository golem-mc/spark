package platform

import (
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/monitor"
	"github.com/golem-mc/spark/internal/text"
)

// heapPool is the memory area a collection is reported against, the Go
// heap of the health report's pools.
const heapPool = "Go heap"

// gcType names a collection as spark's GarbageCollectionMonitor.getGcType
// does a JVM's ("Young Gen"); Go has one kind.
func gcType(monitor.GCInfo) string { return monitor.GCName }

func pauseMillis(info monitor.GCInfo) string {
	return text.Decimal2(float64(info.Pause) / float64(time.Millisecond))
}

func tickMonitorCommands(*Platform) []*Command {
	return []*Command{{
		Aliases: []string{"tickmonitor", "tickmonitoring"},
		Usage: []ArgInfo{
			{Name: "threshold", Param: "percentage increase"},
			{Name: "threshold-tick", Param: "tick duration"},
			{Name: "without-gc"},
		},
		Run: tickMonitorCommand,
		Complete: func(_ *Platform, _ plugin.CommandSender, args []string) []string {
			return CompleteFlags(args, "--threshold", "--threshold-tick", "--without-gc")
		},
	}}
}

// tickMonitorCommand toggles the tick monitor. spark: TickMonitoringModule
func tickMonitorCommand(p *Platform, _ plugin.CommandSender, r *Responder, args *Arguments) error {
	if p.tickMonitor != nil {
		p.tickMonitor.close()
		p.tickMonitor = nil
		r.BroadcastPrefixed(text.T("Tick monitor disabled."))
		return nil
	}
	pred := reportPredicate{percentage: true, threshold: 100}
	threshold, err := args.Int("threshold")
	if err != nil {
		return err
	}
	if threshold != -1 {
		pred.threshold = float64(threshold)
	} else if threshold, err = args.Int("threshold-tick"); err != nil {
		return err
	} else if threshold != -1 {
		pred = reportPredicate{threshold: float64(threshold)}
	}
	m := &tickMonitor{send: r.Async().BroadcastPrefixed, pred: pred, zeroTick: p.Server.Ticks().Current()}
	if !args.Bool("without-gc") {
		m.gc = monitor.NewGCMonitor()
		m.gc.AddListener(m.onGC)
	}
	p.tickMonitor = m
	return nil
}

// reportPredicate decides which ticks the tick monitor reports: those more
// than threshold percent over the average, or longer than threshold
// milliseconds. spark: ReportPredicate
type reportPredicate struct {
	percentage bool
	threshold  float64
}

func (p reportPredicate) shouldReport(duration, increase, percentageChange float64) bool {
	if increase <= 0 {
		return false
	}
	if p.percentage {
		return percentageChange > p.threshold
	}
	return duration > p.threshold
}

func (p reportPredicate) startMessage() plugin.Text {
	if p.percentage {
		return text.T("Starting now, any ticks with >" + text.JavaDouble(p.threshold) + "% increase in " +
			"duration compared to the average will be reported.")
	}
	return text.T("Starting now, any ticks with duration >" + text.JavaDouble(p.threshold) + " will be reported.")
}

// setupTicks is how many ticks the tick monitor averages before it reports.
const setupTicks = 120

// tickMonitor reports ticks that take much longer than the average, timed
// from one tick's start to the next. spark: TickMonitor
type tickMonitor struct {
	// send broadcasts a report line; from any goroutine.
	send     func(plugin.Text)
	pred     reportPredicate
	zeroTick int64
	gc       *monitor.GCMonitor

	// mu guards the state below: ticks are timed on the tick goroutine,
	// collections reported on the GC monitor's.
	mu          sync.Mutex
	started     bool
	monitoring  bool
	lastTick    float64 // milliseconds; 0 skips the next tick's duration
	currentTick int64
	count       int
	sum         float64
	minTick     float64
	maxTick     float64
	average     float64
}

func (m *tickMonitor) close() {
	if m.gc != nil {
		m.gc.Close()
	}
}

func line(label string, value float64) plugin.Text {
	return text.Colored(text.Gray, text.T(">", text.White), text.Space(), text.T(label+": "),
		text.T(text.Decimal2(value)), text.T("ms"))
}

// onTick is called at the start of each tick. spark: TickMonitor.onTick
func (m *tickMonitor) onTick(tick int64, now time.Time) {
	ms := float64(now.UnixNano()) / 1e6
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTick = tick - m.zeroTick
	if !m.started {
		m.started, m.lastTick = true, ms
		m.minTick, m.maxTick = math.Inf(1), math.Inf(-1)
		m.send(text.T("Tick monitor started. Before the monitor becomes fully active, the server's " +
			"average tick rate will be calculated over a period of 120 ticks (approx 6 seconds)."))
		return
	}
	last := m.lastTick
	duration := ms - last
	m.lastTick = ms
	if last == 0 {
		return
	}
	if !m.monitoring {
		m.count++
		m.sum += duration
		m.minTick, m.maxTick = min(m.minTick, duration), max(m.maxTick, duration)
		if m.count >= setupTicks {
			m.average = m.sum / float64(m.count)
			m.monitoring = true
			m.send(text.T("Analysis is now complete.", text.Gold))
			m.send(line("Max", m.maxTick))
			m.send(line("Min", m.minTick))
			m.send(line("Average", m.average))
			m.send(m.pred.startMessage())
		}
	}
	if m.monitoring {
		increase := duration - m.average
		change := increase * 100 / m.average
		if m.pred.shouldReport(duration, increase, change) {
			m.send(text.Colored(text.Gray, text.T("Tick "), text.T("#"+strconv.FormatInt(m.currentTick, 10), text.DarkGray),
				text.T(" lasted "), text.T(text.Decimal2(duration), text.Gold), text.T(" ms. "), text.T("("),
				text.T(text.Decimal2(change)+"%", text.Gold), text.T(" increase from avg)")))
		}
	}
}

// onGC reports a collection, or keeps the tick it fell in out of the
// average. spark: TickMonitor.onGc
func (m *tickMonitor) onGC(info monitor.GCInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.monitoring {
		m.lastTick = 0
		return
	}
	m.send(text.Colored(text.Gray, text.T("Tick "), text.T("#"+strconv.FormatInt(m.currentTick, 10), text.DarkGray),
		text.T(" included "), text.T("GC", text.Red), text.T(" lasting "), text.T(pauseMillis(info), text.Gold),
		text.T(" ms. (type = "+gcType(info)+")")))
}

func gcCommands(*Platform) []*Command {
	return []*Command{
		{Aliases: []string{"gc"}, Run: gcStatistics},
		{Aliases: []string{"gcmonitor", "gcmonitoring"}, Run: gcMonitorCommand},
	}
}

// gcStatistics is /spark gc. spark: GcMonitoringModule
func gcStatistics(p *Platform, _ plugin.CommandSender, r *Responder, _ *Arguments) error {
	r.ReplyPrefixed(text.T("Calculating GC statistics..."))
	report := []plugin.Text{text.Empty(), text.Of(text.T(">", text.DarkGray).Bold(), text.Space(),
		text.T("Garbage Collector statistics", text.Gold))}
	uptime := time.Since(p.started)
	stats := monitor.PollGCSince(p.startupGC)
	for _, name := range []string{monitor.GCName} {
		gc, ok := stats[name]
		if !ok {
			continue
		}
		report = append(report, text.Empty(), text.Of(text.T("    "), text.T(name+" collector:", text.Gray)))
		if gc.Count == 0 {
			report = append(report, text.Of(text.T("      "), text.T("0", text.White), text.T(" collections", text.Gray)))
			continue
		}
		report = append(report,
			text.Of(text.T("      "), text.T(text.Decimal2(gc.AvgTime()), text.Gold), text.T(" ms avg", text.Gray),
				text.T(", ", text.DarkGray), text.T(strconv.FormatInt(gc.Count, 10), text.White), text.T(" total collections", text.Gray)),
			text.Of(text.T("      "), text.T(text.Seconds(gc.AvgFrequency(uptime)/1000), text.White),
				text.T(" avg frequency", text.Gray)))
	}
	if len(stats) == 0 {
		r.ReplyPrefixed(text.T("No garbage collectors are reporting data."))
		return nil
	}
	r.Reply(report...)
	return nil
}

// gcMonitorCommand toggles reporting each collection.
func gcMonitorCommand(p *Platform, _ plugin.CommandSender, r *Responder, _ *Arguments) error {
	if p.gcMonitor != nil {
		p.gcMonitor.Close()
		p.gcMonitor = nil
		r.BroadcastPrefixed(text.T("GC monitor disabled."))
		return nil
	}
	p.gcMonitor = monitor.NewGCMonitor()
	async := r.Async()
	p.gcMonitor.AddListener(func(info monitor.GCInfo) { async.Broadcast(gcReport(info)...) })
	r.BroadcastPrefixed(text.T("GC monitor enabled."))
	return nil
}

// gcReport describes one collection. spark: GcMonitoringModule.ReportingGcMonitor
func gcReport(info monitor.GCInfo) []plugin.Text {
	cause := ""
	if info.Forced {
		cause = " (cause = forced)"
	}
	report := []plugin.Text{text.Prefix(text.Colored(text.Gray, text.T(gcType(info)+" "), text.T("GC", text.Red),
		text.T(" lasting "), text.T(pauseMillis(info), text.Gold), text.T(" ms."+cause)))}
	before, after := int64(info.HeapBefore), int64(info.HeapAfter)
	switch diff := before - after; {
	case before == 0 || diff == 0:
	case diff > 0:
		report = append(report,
			text.Of(text.T("  "), text.T(text.Bytes(diff), text.Gold), text.T(" freed from ", text.DarkGray), text.T(heapPool, text.Gray)),
			text.Of(text.T("    "), text.T(text.Bytes(before), text.Gray), text.T(" → ", text.DarkGray), text.T(text.Bytes(after), text.Gray),
				text.Space(), text.T("(", text.DarkGray), text.T(text.Percent(float64(diff), float64(before)), text.White), text.T(")", text.DarkGray)))
	default:
		report = append(report,
			text.Of(text.T("  "), text.T(text.Bytes(-diff), text.Gold), text.T(" moved to ", text.DarkGray), text.T(heapPool, text.Gray)),
			text.Of(text.T("    "), text.T(text.Bytes(before), text.Gray), text.T(" → ", text.DarkGray), text.T(text.Bytes(after), text.Gray)))
	}
	return report
}
