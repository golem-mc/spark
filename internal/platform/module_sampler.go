package platform

import (
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/activitylog"
	"github.com/golem-mc/spark/internal/bytebin"
	"github.com/golem-mc/spark/internal/monitor"
	"github.com/golem-mc/spark/internal/sampler"
	"github.com/golem-mc/spark/internal/sparkproto"
	"github.com/golem-mc/spark/internal/text"
	"google.golang.org/protobuf/proto"
)

// activeSampler is the running profiler with what its reports need.
// spark: SamplerContainer's sampler
type activeSampler struct {
	*sampler.Sampler
	// initialGC are the GC statistics when it started.
	initialGC map[string]monitor.GCStats
}

// exportProps are the choices of one profiler export. spark: Sampler.ExportProps
type exportProps struct {
	creator             Sender
	comment             string
	separateParentCalls bool
}

func samplerCommands(*Platform) []*Command {
	return []*Command{{
		Aliases:  []string{"profiler", "sampler"},
		AllowSub: true,
		Usage: []ArgInfo{
			{Sub: "info"}, {Sub: "open"},
			{Sub: "start", Name: "timeout", Param: "timeout seconds"},
			{Sub: "start", Name: "thread *"},
			{Sub: "start", Name: "thread", Param: "thread name"},
			{Sub: "start", Name: "only-ticks-over", Param: "tick length millis"},
			{Sub: "start", Name: "interval", Param: "interval millis"},
			{Sub: "start", Name: "alloc"},
			{Sub: "start", Name: "lock"},
			{Sub: "stop"}, {Sub: "cancel"},
		},
		Run: profiler,
		Complete: func(_ *Platform, _ plugin.CommandSender, args []string) []string {
			var opts []string
			if len(args) > 0 {
				switch args[0] {
				case "stop", "upload":
					opts = []string{"--comment", "--save-to-file"}
				case "start":
					opts = []string{"--timeout", "--regex", "--combine-all", "--not-combined", "--interval",
						"--only-ticks-over", "--force-java-sampler", "--alloc", "--alloc-live-only", "--lock"}
				}
				opts = slices.DeleteFunc(opts, func(o string) bool { return slices.Contains(args, o) })
				if args[0] == "start" {
					opts = append(opts, "--thread") // allowed more than once
				}
			}
			return CompleteAt(args, map[int][]string{0: {"info", "start", "open", "stop", "cancel"}}, map[int][]string{1: opts})
		},
	}}
}

// profiler dispatches /spark profiler. spark: SamplerModule.profiler
func profiler(p *Platform, sender plugin.CommandSender, r *Responder, args *Arguments) error {
	sub := args.SubCommand()
	is := func(names ...string) bool {
		return slices.Contains(names, sub) || slices.ContainsFunc(names, args.Bool)
	}
	switch {
	case is("info"):
		p.profilerInfo(r)
	case is("open"):
		p.profilerOpen(r)
	case is("trust-viewer"):
		r.ReplyPrefixed(text.T("The live viewer is not supported.", text.Red))
	case is("cancel"):
		p.profilerCancel(r)
	case is("stop", "upload"):
		p.profilerStop(r, args)
	case is("start"):
		return p.profilerStart(r, args)
	case len(args.Raw()) == 0:
		p.profilerInfo(r)
	default:
		return p.profilerStart(r, args)
	}
	return nil
}

func cmdPrompt(cmd string) plugin.Text {
	return text.Of(text.T("  "), text.RunCommand(text.T(cmd, text.White), cmd))
}

func (p *Platform) activeSampler() *activeSampler {
	p.samplerMu.Lock()
	defer p.samplerMu.Unlock()
	return p.sampler
}

// unsetSampler clears the active sampler if it is s.
func (p *Platform) unsetSampler(s *activeSampler) {
	p.samplerMu.Lock()
	defer p.samplerMu.Unlock()
	if p.sampler == s {
		p.sampler = nil
	}
}

// startSampler starts a sampler and makes it the active one.
func (p *Platform) startSampler(settings sampler.Settings, onComplete func(*activeSampler, error)) (*activeSampler, error) {
	a := &activeSampler{initialGC: monitor.PollGC()}
	s, err := sampler.Start(settings, sampler.Env{
		MeasureWindow: p.measureWindow,
		OnComplete: func(_ *sampler.Sampler, err error) {
			p.unsetSampler(a)
			if onComplete != nil {
				onComplete(a, err)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	a.Sampler = s
	p.samplerMu.Lock()
	p.sampler = a
	p.samplerMu.Unlock()
	return a, nil
}

// profilerStart starts a profiler. spark: SamplerModule.profilerStart
func (p *Platform) profilerStart(r *Responder, args *Arguments) error {
	if previous := p.activeSampler(); previous != nil {
		if !previous.Background() {
			p.profilerInfo(r)
			return nil
		}
		r.ReplyPrefixed(text.T("Stopping the background profiler before starting... please wait"))
		p.unsetSampler(previous)
		previous.Stop(true)
	}

	timeout, err := args.Int("timeout")
	if err != nil {
		return err
	}
	if timeout != -1 && timeout <= 10 {
		r.ReplyPrefixed(text.T("The specified timeout is not long enough for accurate results to be formed. "+
			"Please choose a value greater than 10.", text.Red))
		return nil
	}
	if timeout != -1 && timeout < 30 {
		r.ReplyPrefixed(text.T("The accuracy of the output will significantly improve when the profiler is able to run for longer periods. " +
			"Consider setting a timeout value over 30 seconds."))
	}
	alloc, lock := args.Bool("alloc"), args.Bool("lock")
	switch {
	case alloc && lock:
		r.ReplyPrefixed(text.T("The --alloc and --lock flags cannot be used together. Please choose one profiling mode.", text.Red))
		return nil
	case alloc:
		r.ReplyPrefixed(text.T("Allocation profiling is not supported on your system. Check the console for more info.", text.Red))
		p.Server.Logger().Warn("Allocation profiling is not ported to golem yet.")
		return nil
	case lock:
		r.ReplyPrefixed(text.T("Lock profiling is not supported on your system. Check the console for more info.", text.Red))
		p.Server.Logger().Warn("Lock profiling is not ported to golem yet.")
		return nil
	}

	interval, err := args.Float("interval")
	if err != nil {
		return err
	}
	settings := sampler.Settings{Threads: sampler.ServerThreadOnly(), Grouper: sampler.ByPool}
	if interval > 0 {
		settings.Interval = time.Duration(interval * float64(time.Millisecond))
	}
	if threads := args.Strings("thread"); len(threads) > 0 {
		switch {
		case slices.Contains(threads, "*"):
			settings.Threads = sampler.AllThreads()
		case args.Bool("regex"):
			if settings.Threads, err = sampler.MatchingThreads(threads); err != nil {
				return ParseError("Invalid regex: " + err.Error())
			}
		default:
			settings.Threads = sampler.NamedThreads(threads)
		}
	}
	switch {
	case args.Bool("combine-all"):
		settings.Grouper = sampler.AsOne
	case args.Bool("not-combined"):
		settings.Grouper = sampler.ByName
	}
	ticksOver, err := args.Int("only-ticks-over")
	if err != nil {
		return err
	}
	if ticksOver != -1 {
		settings.OnlyTicksOver, settings.TicksOver = true, time.Duration(ticksOver)*time.Millisecond
	}
	if timeout != -1 {
		settings.AutoEnd = time.Now().Add(time.Duration(timeout) * time.Second)
	}

	r.BroadcastPrefixed(text.T("Starting a new profiler, please wait..."))
	props := p.exportProps(r, args)
	saveToFile := args.Bool("save-to-file")
	async := r.Async()
	_, err = p.startSampler(settings, func(s *activeSampler, err error) {
		if err != nil {
			async.BroadcastPrefixed(text.T("Profiler operation failed unexpectedly. Error: "+err.Error(), text.Red))
			p.Server.Logger().Error("Profiler operation failed unexpectedly", "error", err)
			return
		}
		async.BroadcastPrefixed(text.T("The active profiler has completed! Uploading results..."))
		p.Server.Scheduler().RunTask(func() {
			snap := p.snapshot(true)
			p.Server.Scheduler().RunAsync(func() { p.handleUpload(async, s, props, saveToFile, snap) })
		})
	})
	if err != nil {
		r.ReplyPrefixed(text.T(err.Error(), text.Red))
		return nil
	}

	r.BroadcastPrefixed(text.Of(text.T("Profiler is now running!", text.Gold), text.Space(), text.T("(go cpu profiler)", text.DarkGray)))
	if timeout == -1 {
		r.BroadcastPrefixed(text.T("It will run in the background until it is stopped by an admin."))
		r.BroadcastPrefixed(text.T("To stop the profiler and upload the results, run:"))
		r.BroadcastPrefixed(cmdPrompt("/" + commandName + " profiler stop"))
		r.BroadcastPrefixed(text.T("To view the profiler while it's running, run:"))
		r.BroadcastPrefixed(cmdPrompt("/" + commandName + " profiler open"))
	} else {
		r.BroadcastPrefixed(text.T("The results will be automatically returned after the profiler has been running for " +
			text.Seconds(int64(timeout)) + "."))
	}
	return nil
}

// profilerInfo reports on the active profiler. spark: SamplerModule.profilerInfo
func (p *Platform) profilerInfo(r *Responder) {
	s := p.activeSampler()
	if s == nil {
		r.ReplyPrefixed(text.T("The profiler isn't running!"))
		r.ReplyPrefixed(text.T("To start a new one, run:"))
		r.ReplyPrefixed(cmdPrompt("/" + commandName + " profiler start"))
		return
	}
	r.ReplyPrefixed(text.T("Profiler is already running!", text.Gold))
	running := int64(time.Since(s.StartTime()) / time.Second)
	if s.Background() {
		r.ReplyPrefixed(text.Of(text.T("It was started "), text.T("automatically", text.White),
			text.T(" when spark enabled and has been running in the background for "+text.Seconds(running)+".")))
	} else {
		r.ReplyPrefixed(text.T("So far, it has profiled for " + text.Seconds(running) + "."))
	}
	r.ReplyPrefixed(text.T("To view the profiler while it's running, run:"))
	r.ReplyPrefixed(cmdPrompt("/" + commandName + " profiler open"))
	if end := s.AutoEnd(); end.IsZero() {
		r.ReplyPrefixed(text.T("To stop the profiler and upload the results, run:"))
		r.ReplyPrefixed(cmdPrompt("/" + commandName + " profiler stop"))
	} else {
		r.ReplyPrefixed(text.T("It is due to complete automatically and upload results in " +
			text.Seconds(int64(time.Until(end)/time.Second)) + "."))
	}
	r.ReplyPrefixed(text.T("To cancel the profiler without uploading the results, run:"))
	r.ReplyPrefixed(cmdPrompt("/" + commandName + " profiler cancel"))
}

// profilerOpen would open the live viewer. spark: SamplerModule.profilerOpen
func (p *Platform) profilerOpen(r *Responder) {
	r.ReplyPrefixed(text.T("The live viewer is not supported.", text.Red))
}

// profilerCancel stops the profiler without uploading. spark: SamplerModule.profilerCancel
func (p *Platform) profilerCancel(r *Responder) {
	s := p.activeSampler()
	if s == nil {
		r.ReplyPrefixed(text.T("There isn't an active profiler running."))
		return
	}
	p.unsetSampler(s)
	p.Server.Scheduler().RunAsync(func() { s.Stop(true) })
	r.BroadcastPrefixed(text.T("Profiler has been cancelled.", text.Gold))
}

// profilerStop stops the profiler and uploads or saves it.
// spark: SamplerModule.profilerStop
func (p *Platform) profilerStop(r *Responder, args *Arguments) {
	s := p.activeSampler()
	if s == nil {
		r.ReplyPrefixed(text.T("There isn't an active profiler running."))
		return
	}
	p.unsetSampler(s)
	saveToFile := args.Bool("save-to-file")
	if saveToFile {
		r.BroadcastPrefixed(text.T("Stopping the profiler & saving results, please wait..."))
	} else {
		r.BroadcastPrefixed(text.T("Stopping the profiler & uploading results, please wait..."))
	}
	props := p.exportProps(r, args)
	snap := p.snapshot(true)
	async := r.Async()
	p.Server.Scheduler().RunAsync(func() {
		s.Stop(false)
		p.handleUpload(async, s, props, saveToFile, snap)
		// A stopped profile, background or not, gives way to a new
		// background one.
		if p.startBackgroundProfiler() {
			async.BroadcastPrefixed(text.Of(text.T("Restarted the background profiler. "),
				text.T("(If you don't want this to happen, run: /"+commandName+" profiler cancel)", text.DarkGray)))
		}
	})
}

// startBackgroundProfiler starts the background profiler when the config
// enables it, and reports whether it did.
// spark: BackgroundSamplerManager.startSampler
func (p *Platform) startBackgroundProfiler() bool {
	if !p.Config.Bool("backgroundProfiler", true) {
		return false
	}
	settings := sampler.Settings{
		Background: true,
		Interval:   time.Duration(p.Config.Int("backgroundProfilerInterval", 10)) * time.Millisecond,
		Threads:    sampler.ParseThreads(p.Config.String("backgroundProfilerThreadDumper", "default")),
		Grouper:    sampler.ParseGrouper(p.Config.String("backgroundProfilerThreadGrouper", "by-pool")),
	}
	_, err := p.startSampler(settings, func(_ *activeSampler, err error) {
		if err != nil {
			p.Server.Logger().Error("The background profiler failed", "error", err)
		}
	})
	if err != nil {
		p.Server.Logger().Warn("Failed to start background profiler.", "error", err)
		return false
	}
	return true
}

// initBackgroundProfiler starts the background profiler as spark enables,
// writing backgroundProfiler: true to the config the first time.
// spark: BackgroundSamplerManager.initialise
func (p *Platform) initBackgroundProfiler() {
	if !p.Config.Bool("backgroundProfiler", true) {
		return
	}
	if !p.Config.Contains("backgroundProfiler") {
		p.Config.Set("backgroundProfiler", true)
		if err := p.Config.Save(); err != nil {
			p.Server.Logger().Warn("Could not save the config", "error", err)
		}
	}
	p.Server.Logger().Info("Starting background profiler...")
	p.startBackgroundProfiler()
}

func (p *Platform) exportProps(r *Responder, args *Arguments) exportProps {
	props := exportProps{creator: r.Sender(), separateParentCalls: args.Bool("separate-parent-calls")}
	if comments := args.Strings("comment"); len(comments) > 0 {
		props.comment = comments[0]
	}
	return props
}

// samplerData is a profile's full export. spark: Sampler.toProto
func (p *Platform) samplerData(s *activeSampler, props exportProps, snap tickSnapshot) *sparkproto.SamplerData {
	plugins := p.Server.Plugins()
	data := s.Export(sampler.ExportOptions{
		SeparateParentCalls: props.separateParentCalls,
		Source:              func(pkg string) (string, bool) { return pluginOfModule(plugins, pkg) },
	})
	md := &sparkproto.SamplerMetadata{Comment: props.comment}
	s.Metadata(md)
	p.gatherMetadata(props.creator, s.initialGC, snap).writeSampler(md)
	data.Metadata = md
	return data
}

// handleUpload uploads a stopped profile, or saves it when asked or when the
// upload fails; off the tick goroutine. spark: SamplerModule.handleUpload
func (p *Platform) handleUpload(r *Responder, s *activeSampler, props exportProps, saveToFile bool, snap tickSnapshot) {
	data := p.samplerData(s, props, snap)
	if !saveToFile {
		url, err := p.upload(data, bytebin.SamplerMediaType, "")
		if err == nil {
			r.BroadcastPrefixed(text.T("Profiler stopped & upload complete!", text.Gold))
			r.Broadcast(text.OpenURL(text.T(url, text.Gray), url))
			p.logActivity(r, "Profiler", activitylog.DataURL, url)
			return
		}
		r.BroadcastPrefixed(text.T("An error occurred whilst uploading the results. Attempting to save to disk instead.", text.Red))
		p.Server.Logger().Warn("Error whilst uploading profiler results", "error", err)
	}
	file, err := p.saveProto("profile", "sparkprofile", data)
	if err != nil {
		r.BroadcastPrefixed(text.T("An error occurred whilst saving the data.", text.Red))
		p.Server.Logger().Warn("Error whilst saving profiler results", "error", err)
		return
	}
	r.BroadcastPrefixed(text.T("Profiler stopped & save complete!", text.Gold))
	r.BroadcastPrefixed(text.T("Data has been written to: " + file))
	r.BroadcastPrefixed(text.T("You can view the profile file using the web app @ "+p.ViewerURL, text.Gray))
	p.logActivity(r, "Profiler", activitylog.DataFile, file)
	// A golem extra: the same samples as a pprof profile, every goroutine
	// included, for go tool pprof.
	if pprofFile, err := p.savePprof(s); err == nil {
		r.BroadcastPrefixed(text.T("A pprof profile has been written to: " + pprofFile))
	} else {
		p.Server.Logger().Warn("Error whilst saving the pprof profile", "error", err)
	}
}

// saveFile answers the path of a new file prefix-<date>.ext in spark's
// directory. spark: SparkPlatform.resolveSaveFile
func (p *Platform) saveFile(prefix, ext string) (string, error) {
	dir := p.Server.DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, prefix+"-"+time.Now().Format("2006-01-02_15.04.05")+"."+ext), nil
}

func (p *Platform) saveProto(prefix, ext string, msg proto.Message) (string, error) {
	file, err := p.saveFile(prefix, ext)
	if err != nil {
		return "", err
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return "", err
	}
	return file, os.WriteFile(file, data, 0o644)
}

func (p *Platform) savePprof(s *activeSampler) (string, error) {
	file, err := p.saveFile("profile", "pprof")
	if err != nil {
		return "", err
	}
	f, err := os.Create(file)
	if err != nil {
		return "", err
	}
	if err := s.WritePprof(f); err != nil {
		f.Close()
		os.Remove(file)
		return "", err
	}
	return file, f.Close()
}

// measureWindow is a profile window's statistics.
// spark: WindowStatisticsCollector.measure
func (p *Platform) measureWindow(start, end time.Time, ticks int) *sparkproto.WindowStatistics {
	stats := &sparkproto.WindowStatistics{
		StartTime: start.UnixMilli(), EndTime: end.UnixMilli(), Duration: int32(end.Sub(start).Milliseconds()),
		Ticks: int32(ticks), Tps: p.Ticks.TPS1Min(),
		CpuProcess: p.Monitors.CPU.Process1Min(), CpuSystem: p.Monitors.CPU.System1Min(),
	}
	if p.Ticks.DurationSupported() {
		d := p.Ticks.Duration1Min()
		stats.MsptMedian, stats.MsptMax = d.Median(), d.Max()
	}
	if c := p.latestCounts.Load(); c != nil {
		stats.Players, stats.Entities, stats.TileEntities, stats.Chunks = c.Players, c.Entities, c.TileEntities, c.Chunks
	}
	return stats
}
