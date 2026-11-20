package platform

import (
	"fmt"
	"slices"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/monitor"
	"github.com/golem-mc/spark/internal/text"
)

// healthCommands are /spark health, tps and ping. spark: HealthModule
func healthCommands(*Platform) []*Command {
	return []*Command{
		{
			Aliases:  []string{"health", "healthreport"},
			AllowSub: true,
			Usage: []ArgInfo{
				{Sub: "dashboard"}, {Sub: "upload"},
				{Sub: "show", Name: "memory"}, {Sub: "show", Name: "network"},
			},
			Run: healthReport,
			Complete: func(_ *Platform, _ plugin.CommandSender, args []string) []string {
				var opts []string
				if len(args) > 0 && args[0] == "show" {
					opts = slices.DeleteFunc([]string{"--memory", "--network"}, func(o string) bool { return slices.Contains(args, o) })
				}
				return CompleteAt(args, map[int][]string{0: {"dashboard", "upload", "show"}}, map[int][]string{1: opts})
			},
		},
		{
			Aliases: []string{"tps", "cpu"},
			Run:     func(p *Platform, _ plugin.CommandSender, r *Responder, _ *Arguments) error { p.tps(r); return nil },
		},
		{
			Aliases: []string{"ping"},
			Usage:   []ArgInfo{{Name: "player", Param: "username"}},
			Run:     ping,
			Complete: func(_ *Platform, _ plugin.CommandSender, args []string) []string {
				return CompleteFlags(args, "--player")
			},
		},
	}
}

// tps reports the tick rate, tick durations and CPU usage.
// spark: HealthModule.tps
func (p *Platform) tps(r *Responder) {
	t := p.Ticks
	target := t.TargetTPS()
	r.ReplyPrefixed(text.T("TPS from last 5s, 10s, 1m, 5m, 15m:"))
	r.ReplyPrefixed(text.Of(text.T(" "),
		text.TPS(t.TPS5Sec(), target), text.T(", "),
		text.TPS(t.TPS10Sec(), target), text.T(", "),
		text.TPS(t.TPS1Min(), target), text.T(", "),
		text.TPS(t.TPS5Min(), target), text.T(", "),
		text.TPS(t.TPS15Min(), target)))
	r.ReplyPrefixed(text.Empty())

	if t.DurationSupported() {
		r.ReplyPrefixed(text.T("Tick durations (min/med/95%ile/max ms) from last 10s, 1m:"))
		r.ReplyPrefixed(text.Of(text.T(" "),
			text.TickDurations(t.Duration10Sec(), t.MaxIdealDuration()), text.T(";  "),
			text.TickDurations(t.Duration1Min(), t.MaxIdealDuration())))
		r.ReplyPrefixed(text.Empty())
	}

	cpu := p.Monitors.CPU
	r.ReplyPrefixed(text.T("CPU usage from last 10s, 1m, 15m:"))
	r.ReplyPrefixed(text.Of(text.T(" "),
		text.CPU(cpu.System10Sec()), text.T(", "), text.CPU(cpu.System1Min()), text.T(", "), text.CPU(cpu.System15Min()),
		text.T("  (system)", text.DarkGray)))
	r.ReplyPrefixed(text.Of(text.T(" "),
		text.CPU(cpu.Process10Sec()), text.T(", "), text.CPU(cpu.Process1Min()), text.T(", "), text.CPU(cpu.Process15Min()),
		text.T("  (process)", text.DarkGray)))
}

// ping reports the players' pings. spark: HealthModule.ping
func ping(p *Platform, _ plugin.CommandSender, r *Responder, args *Arguments) error {
	pings := p.pings()
	if names := args.Strings("player"); len(names) > 0 {
		for _, name := range names {
			pp, ok := monitor.FindPing(pings, name)
			if !ok {
				r.ReplyPrefixed(text.T("Ping data is not available for '" + name + "'."))
				continue
			}
			r.ReplyPrefixed(text.Of(text.T("Player "), text.T(pp.Name, text.White), text.T(" has "),
				text.PingRTT(float64(pp.Ping)), text.T(" ms ping.")))
		}
		return nil
	}
	summary := monitor.Summarise(pings)
	avg := p.Ping.Average()
	if summary.Total() == 0 && avg.Samples() == 0 {
		r.ReplyPrefixed(text.T("There is not enough data to show ping averages yet. Please try again later."))
		return nil
	}
	r.ReplyPrefixed(text.T("Average Pings (min/med/95%ile/max ms) from now, last 15m:"))
	r.ReplyPrefixed(text.Of(text.T(" "),
		text.PingRTTs(summary.Min(), summary.Median(), summary.Percentile95(), summary.Max()), text.T(";  "),
		text.PingRTTs(avg.Min(), avg.Median(), avg.Percentile95(), avg.Max())))
	return nil
}

// healthReport runs /spark health: show prints the report in chat, the
// rest upload it. spark: HealthModule.healthReport
func healthReport(p *Platform, sender plugin.CommandSender, r *Responder, args *Arguments) error {
	if args.SubCommand() == "show" {
		r.ReplyPrefixed(text.T("Generating server health report..."))
		r.Reply(p.healthReportLines(args.Bool("memory"), args.Bool("network"))...)
		return nil
	}
	r.ReplyPrefixed(text.T("Uploading health reports is not available yet; run /spark health show.", text.Red))
	return nil
}

// heading is a report section's title. spark: text(">", DARK_GRAY, BOLD) ...
func heading(title string) plugin.Text {
	return text.Of(text.T(">", text.DarkGray).Bold(), text.Space(), text.T(title, text.Gold))
}

// usageLine is "used / max   (percent)".
func usageLine(used, max int64) plugin.Text {
	return text.Of(text.T("    "),
		text.T(text.Bytes(used), text.White), text.Space(), text.T("/", text.Gray), text.Space(),
		text.T(text.Bytes(max), text.White), text.T("   "),
		text.T("(", text.Gray), text.T(text.Percent(float64(used), float64(max)), text.Green), text.T(")", text.Gray))
}

// healthReportLines is /spark health show's report. spark: HealthModule.healthReportShow
func (p *Platform) healthReportLines(memory, network bool) []plugin.Text {
	report := []plugin.Text{text.Empty()}
	t := p.Ticks
	target := t.TargetTPS()
	report = append(report,
		heading("TPS from last 5s, 10s, 1m, 5m, 15m:"),
		text.Of(text.T("    "),
			text.TPS(t.TPS5Sec(), target), text.T(", "),
			text.TPS(t.TPS10Sec(), target), text.T(", "),
			text.TPS(t.TPS1Min(), target), text.T(", "),
			text.TPS(t.TPS5Min(), target), text.T(", "),
			text.TPS(t.TPS15Min(), target)),
		text.Empty())
	if t.DurationSupported() {
		report = append(report,
			heading("Tick durations (min/med/95%ile/max ms) from last 10s, 1m:"),
			text.Of(text.T("    "),
				text.TickDurations(t.Duration10Sec(), t.MaxIdealDuration()), text.T("; "),
				text.TickDurations(t.Duration1Min(), t.MaxIdealDuration())),
			text.Empty())
	}

	cpu := p.Monitors.CPU
	report = append(report,
		heading("CPU usage from last 10s, 1m, 15m:"),
		text.Of(text.T("    "),
			text.CPU(cpu.System10Sec()), text.T(", "), text.CPU(cpu.System1Min()), text.T(", "), text.CPU(cpu.System15Min()),
			text.T("  (system)", text.DarkGray)),
		text.Of(text.T("    "),
			text.CPU(cpu.Process10Sec()), text.T(", "), text.CPU(cpu.Process1Min()), text.T(", "), text.CPU(cpu.Process15Min()),
			text.T("  (process)", text.DarkGray)),
		text.Empty())

	heap := monitor.HeapUsage()
	report = append(report,
		heading("Memory usage:"),
		usageLine(heap.Used, heap.Max),
		text.Of(text.T("    "), text.MemoryDiagram(float64(heap.Used), float64(heap.Committed), float64(heap.Max), 60)),
		text.Empty())

	if memory {
		report = append(report, detailedMemoryLines()...)
	}
	report = append(report, p.networkLines(network)...)

	if used, total := monitor.Disk(); total != 0 && used != 0 {
		report = append(report,
			heading("Disk usage:"),
			usageLine(used, total),
			text.Of(text.T("    "), text.DiskDiagram(float64(used), float64(total), 60)),
			text.Empty())
	}
	return report
}

// detailedMemoryLines are --memory's non-heap usage and heap pools.
// spark: HealthModule.addDetailedMemoryStats
func detailedMemoryLines() []plugin.Text {
	nonHeap := monitor.NonHeapUsage()
	report := []plugin.Text{
		heading("Non-heap memory usage:"),
		text.Of(text.T("    "), text.T(text.Bytes(nonHeap.Used), text.White)),
		text.Empty(),
	}
	for _, pool := range monitor.HeapPools() {
		u, c := pool.Usage, pool.CollectionUsage
		if u.Max == -1 {
			u.Max = u.Committed
		}
		report = append(report,
			heading(pool.Name+" pool usage:"),
			usageLine(u.Used, u.Max),
			text.Of(text.T("    "), text.MemoryPoolDiagram(float64(u.Used), float64(c.Used), float64(u.Committed), float64(u.Max), 60)),
			text.Of(text.T("     "), text.T("-", text.Red), text.Space(), text.T("Usage at last GC:", text.Gray), text.Space(),
				text.T(text.Bytes(c.Used), text.White)),
			text.Empty())
	}
	return report
}

// networkLines are the interfaces' traffic; without detailed, only the
// directions that saw any. spark: HealthModule.addNetworkStats
func (p *Platform) networkLines(detailed bool) []plugin.Text {
	var lines []plugin.Text
	for _, name := range p.Monitors.Network.Interfaces() {
		avg := p.Monitors.Network.Averages(name)
		for _, dir := range []struct {
			abbrev         string
			bytes, packets interface{ Mean() float64 }
		}{{"rx", avg.RxBytes, avg.RxPackets}, {"tx", avg.TxBytes, avg.TxPackets}} {
			bytesPerSec, packetsPerSec := int64(dir.bytes.Mean()), int64(dir.packets.Mean())
			if !detailed && bytesPerSec <= 0 && packetsPerSec <= 0 {
				continue
			}
			lines = append(lines, text.Colored(text.Gray,
				text.T("    "),
				text.BytesColored(bytesPerSec, text.Green, "/s"),
				text.T(" / "),
				text.T(thousands(packetsPerSec), text.White),
				text.T(" pps "),
				text.Colored(text.DarkGray, text.T("("), text.T(name+" "+dir.abbrev, text.White), text.T(")"))))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	out := append([]plugin.Text{heading("Network usage: (system, last 15m)")}, lines...)
	return append(out, text.Empty())
}

// thousands formats n with comma separators, as Java's "%,d".
func thousands(n int64) string {
	s := fmt.Sprint(n)
	start := 0
	if n < 0 {
		start = 1
	}
	for i := len(s) - 3; i > start; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// registerTPSCommand replaces /tps with spark's report, for senders with
// spark, spark.tps or bukkit.command.tps. spark: BukkitSparkPlugin (overrideTpsCommand)
func (p *Platform) registerTPSCommand() error {
	return p.Server.Commands().Register(plugin.Command{
		Name:        "tps",
		Description: "Gets the current ticks per second for the server",
		Override:    true,
		Run: func(ctx *plugin.CommandContext) (int, error) {
			s := ctx.Sender
			r := &Responder{p: p, sender: s, data: senderOf(s), alias: "tps"}
			if !s.HasPermission("spark") && !s.HasPermission("spark.tps") && !s.HasPermission("bukkit.command.tps") {
				r.Reply(text.T("You do not have permission to use this command.", text.Red))
				return 0, nil
			}
			p.tps(r)
			return 1, nil
		},
	})
}
