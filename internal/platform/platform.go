// Package platform is spark on golem: its configuration, monitors and the
// /spark command with its modules. spark: SparkPlatform, CommandManager and
// the command modules
package platform

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/activitylog"
	"github.com/golem-mc/spark/internal/bytebin"
	"github.com/golem-mc/spark/internal/config"
	"github.com/golem-mc/spark/internal/metric"
	"github.com/golem-mc/spark/internal/monitor"
)

// Platform is one enabled spark.
type Platform struct {
	Server plugin.Server
	// Version is spark's version, without a leading v.
	Version string
	Config  *config.Config

	// ViewerURL is spark's web viewer, ending in a slash; an upload's key
	// is appended to it.
	ViewerURL string
	Bytebin   *bytebin.Client
	Activity  *activitylog.Log

	Metrics  *metric.Metrics
	Ticks    *monitor.TickStatistics
	Ping     *monitor.PingStatistics
	Monitors *monitor.Monitors

	// startupGC are the GC statistics once the server finished starting, the
	// base of the GC figures reports show.
	startupGC map[string]monitor.GCStats

	samplerMu sync.Mutex
	sampler   *activeSampler
	// The monitors /spark tickmonitor and gcmonitor toggle; tick goroutine
	// only.
	tickMonitor *tickMonitor
	gcMonitor   *monitor.GCMonitor
	// latestCounts are the world counts last taken on the tick goroutine.
	latestCounts atomic.Pointer[metric.WorldCounts]

	commands         []*Command
	disableBroadcast bool
	closers          []func()
	tasks            []plugin.Task

	// started is when spark was enabled, the start of the server's normal
	// operation. spark: serverNormalOperationStartTime
	started time.Time
}

// New makes spark for srv, reading plugins/spark/config.json.
func New(srv plugin.Server) *Platform {
	metrics := metric.New()
	p := &Platform{
		Server:   srv,
		Version:  version(srv),
		Config:   config.Load(filepath.Join(srv.DataDir(), "config.json")),
		Activity: activitylog.Load(filepath.Join(srv.DataDir(), "activity.json")),
		Metrics:  metrics,
		Ticks:    monitor.NewTickStatistics(metrics),
		Ping:     monitor.NewPingStatistics(metrics),
	}
	p.ViewerURL = p.Config.String("viewerUrl", "https://spark.lucko.me/")
	if !strings.HasSuffix(p.ViewerURL, "/") {
		p.ViewerURL += "/"
	}
	p.Bytebin = bytebin.New(p.Config.String("bytebinUrl", "https://spark-usercontent.lucko.me/"), "spark-plugin")
	p.disableBroadcast = p.Config.Bool("disableResponseBroadcast", false)
	for _, module := range []func(*Platform) []*Command{samplerCommands, healthCommands, tickMonitorCommands, gcCommands, heapCommands, activityCommands} {
		p.commands = append(p.commands, module(p)...)
	}
	return p
}

// version answers this plugin's module version, "dev" for a build that has
// none.
func version(srv plugin.Server) string {
	for _, info := range srv.Plugins() {
		if info.Name == srv.Name() {
			v := strings.TrimPrefix(info.Version, "v")
			if v == "" || v == "(devel)" {
				return "dev"
			}
			return v
		}
	}
	return "dev"
}

// Enable starts the monitors and registers the commands and permissions.
// spark: SparkPlatform.enable
func (p *Platform) Enable() error {
	p.started = time.Now()
	p.startupGC = monitor.PollGC()
	p.Monitors = monitor.Start(p.Metrics)
	plugin.Listen(p.Server, func(e *plugin.ServerTickStartEvent) {
		p.Ticks.OnTickStart(e.Tick, time.Now(), p.Server.Ticks().Rate())
		if s := p.activeSampler(); s != nil {
			s.OnTickStart(e.Tick)
		}
		if p.tickMonitor != nil {
			p.tickMonitor.onTick(e.Tick, time.Now())
		}
	}, plugin.ListenOptions{Priority: plugin.Lowest})
	plugin.Listen(p.Server, func(e *plugin.ServerTickEndEvent) {
		p.Ticks.OnTickEnd(e.MSPT)
		if s := p.activeSampler(); s != nil {
			s.OnTickEnd(e.Tick, time.Duration(e.MSPT*float64(time.Millisecond)))
		}
	}, plugin.ListenOptions{Priority: plugin.Monitor})
	p.tasks = append(p.tasks, p.Server.Scheduler().RunAsyncTimer(monitor.PingQuerySeconds*time.Second, monitor.PingQuerySeconds*time.Second, func() {
		p.Server.Scheduler().RunTask(func() { p.Ping.Record(p.pings()) })
	}))
	// spark: WorldMetricsCollector
	p.tasks = append(p.tasks, p.Server.Scheduler().RunAsyncTimer(metric.Interval, metric.Interval, func() {
		p.Server.Scheduler().RunTask(func() {
			c := p.counts()
			p.latestCounts.Store(&c)
			p.Metrics.WorldInfo.Record(time.Now().UnixMilli(), c)
		})
	}))

	for _, node := range p.Permissions() {
		p.Server.Permissions().Register(plugin.Permission{Name: node, Default: plugin.PermissionDefault{OpLevel: 2}})
	}
	if err := p.registerCommand(); err != nil {
		return err
	}
	if p.Config.Bool("overrideTpsCommand", true) {
		if err := p.registerTPSCommand(); err != nil {
			p.Server.Logger().Warn("Could not override /tps", "error", err)
		}
	}
	return nil
}

// Disable stops everything Enable started. spark: SparkPlatform.disable
func (p *Platform) Disable() {
	for _, c := range p.closers {
		c()
	}
	if s := p.activeSampler(); s != nil {
		p.unsetSampler(s)
		s.Stop(true)
	}
	if p.tickMonitor != nil {
		p.tickMonitor.close()
	}
	if p.gcMonitor != nil {
		p.gcMonitor.Close()
	}
	for _, t := range p.tasks {
		t.Cancel()
	}
	if p.Monitors != nil {
		p.Monitors.Close()
	}
}

// pings reads every online player's ping; call it on the tick goroutine.
// spark: PlayerPingProvider.poll
func (p *Platform) pings() []monitor.PlayerPing {
	players := p.Server.Players()
	out := make([]monitor.PlayerPing, 0, len(players))
	for _, pl := range players {
		out = append(out, monitor.PlayerPing{Name: pl.Name(), Ping: int(pl.Ping().Milliseconds())})
	}
	return out
}

// registerCommand registers /spark: the bare command and a greedy string of
// words, completed by the modules.
func (p *Platform) registerCommand() error {
	run := func(ctx *plugin.CommandContext) (int, error) {
		p.Execute(ctx.Sender, strings.Fields(plugin.Arg[string](ctx, "args")))
		return 1, nil
	}
	return p.Server.Commands().Register(plugin.Command{
		Name:        commandName,
		Description: "Main plugin command",
		Run:         run, Children: []plugin.Node{{
			Arg: plugin.GreedyArg("args"),
			Run: run,
			Suggest: func(ctx *plugin.CommandContext, partial string) []string {
				return withPrefix(partial, p.Complete(ctx.Sender, strings.Split(partial, " ")))
			},
		}},
	})
}

// withPrefix turns completions of partial's last word into replacements of
// the whole of partial, which is what a greedy argument's suggestions are.
func withPrefix(partial string, candidates []string) []string {
	prefix := ""
	if i := strings.LastIndex(partial, " "); i >= 0 {
		prefix = partial[:i+1]
	}
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = prefix + c
	}
	return out
}
