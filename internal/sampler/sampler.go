package sampler

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"math/rand/v2"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golem-mc/spark/internal/sparkproto"
	"github.com/google/pprof/profile"
)

const (
	// windowSize is the length of one of the viewer's time windows.
	windowSize = time.Minute
	// historySize is how many windows a long-running profile keeps.
	// spark: ProfilingWindowUtils.HISTORY_SIZE
	historySize = 60

	// DefaultInterval is the sampling interval when none is given: the Go
	// runtime's own CPU profiling rate, 100 Hz. Any other rate makes the
	// runtime print a notice each time profiling starts.
	DefaultInterval = 10 * time.Millisecond

	labelThread = "spark-thread"
	labelTick   = "spark-tick"
)

// windowAdjustment shifts window boundaries by a random amount, so servers do
// not all rotate on the minute. spark: ProfilingWindowUtils.WINDOW_ADJUSTMENT_MILLIS
var windowAdjustment = time.Duration(rand.Int64N(int64(windowSize))) - windowSize/2

// windowOf answers the window t falls in.
func windowOf(t time.Time) int {
	return int((t.UnixMilli() + windowAdjustment.Milliseconds()) / windowSize.Milliseconds())
}

// Settings configure a sampler. spark: SamplerSettings, SamplerBuilder
type Settings struct {
	// Interval is the time between samples; 0 is DefaultInterval.
	Interval time.Duration
	Threads  Threads
	Grouper  Grouper
	// AutoEnd stops the sampler at this time when set (--timeout).
	AutoEnd time.Time
	// Background marks the background profiler, which nobody started.
	Background bool
	// TicksOver keeps only the ticks that took at least this long, when
	// OnlyTicksOver is set.
	TicksOver     time.Duration
	OnlyTicksOver bool
}

// Env is what a sampler needs from the server.
type Env struct {
	// MeasureWindow answers the statistics of a window from start to end
	// with ticks counted; called off the tick goroutine.
	MeasureWindow func(start, end time.Time, ticks int) *sparkproto.WindowStatistics
	// OnComplete is called when the sampler stops by itself at AutoEnd, or
	// fails; err is nil for the former. Called on the sampler's goroutine.
	OnComplete func(s *Sampler, err error)
}

// Sampler is a running profiler.
type Sampler struct {
	settings Settings
	env      Env
	interval time.Duration
	hz       int
	start    time.Time

	// tickRoot is the tick goroutine's entry function, learned on its first
	// tick, which tells the server thread's samples from its children's.
	tickRoot atomic.Pointer[string]

	// mu guards the trees and windows. The tick goroutine never takes it,
	// so parsing a segment cannot hold a tick up.
	mu            sync.Mutex
	threads       map[string]*node           // group -> tree
	members       map[string]map[string]bool // group -> names merged into it
	pprofs        map[int]*profile.Profile   // window -> its segments merged, for a pprof file
	windowStarts  map[int]time.Time
	windowStats   map[int]*sparkproto.WindowStatistics
	pendingTicked []pendingSample // ticked mode: samples of ticks not yet ended

	// window is the current window.
	window atomic.Int64

	// tickMu guards the tick counts, which the tick goroutine updates.
	tickMu sync.Mutex
	// firstTick and lastTick are the ticks seen, windowTicks the ticks
	// counted in the current window.
	firstTick, lastTick int64
	windowTicks         int
	// Ticked mode: the ticks over the threshold (tick -> window) and the
	// ticks known to be under it.
	overTicks  map[int64]int
	underTicks map[int64]bool

	stopping chan bool // true: cancelled
	flush    chan chan error
	done     chan struct{}
	err      error
	stopped  atomic.Bool
}

type pendingSample struct {
	tick   int64
	group  string
	name   string
	stack  []Frame
	micros int64
	window int
}

// cpuBusy guards the process's one CPU profile between samplers.
var cpuBusy atomic.Bool

// ErrBusy is starting a sampler while another one, or something else in the
// process, holds the CPU profiler.
var ErrBusy = errors.New("the Go CPU profiler is already in use")

// Start starts sampling. spark: SamplerBuilder.start
func Start(settings Settings, env Env) (*Sampler, error) {
	interval := settings.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	hz := max(1, int(time.Second/interval))
	if !cpuBusy.CompareAndSwap(false, true) {
		return nil, ErrBusy
	}
	now := time.Now()
	s := &Sampler{
		settings: settings, env: env, interval: interval, hz: hz, start: now,
		threads: map[string]*node{}, members: map[string]map[string]bool{},
		windowStarts: map[int]time.Time{}, windowStats: map[int]*sparkproto.WindowStatistics{}, pprofs: map[int]*profile.Profile{},
		firstTick: -1, lastTick: -1,
		overTicks: map[int64]int{}, underTicks: map[int64]bool{},
		stopping: make(chan bool, 1), flush: make(chan chan error), done: make(chan struct{}),
	}
	s.window.Store(int64(windowOf(now)))
	s.windowStarts[windowOf(now)] = now
	seg, err := s.startSegment()
	if err != nil {
		cpuBusy.Store(false)
		return nil, err
	}
	go s.run(seg)
	return s, nil
}

// segment is one CPU profile, from start to stop.
type segment struct{ buf *syncBuffer }

func (s *Sampler) startSegment() (*segment, error) {
	if s.hz != 100 {
		// StartCPUProfile sets 100 Hz; a rate set first wins, with a notice.
		runtime.SetCPUProfileRate(s.hz)
	}
	seg := &segment{buf: &syncBuffer{}}
	if err := pprof.StartCPUProfile(seg.buf); err != nil {
		if s.hz != 100 {
			runtime.SetCPUProfileRate(0)
		}
		return nil, ErrBusy
	}
	return seg, nil
}

// run is the sampler's goroutine: it rotates the profile at each window
// boundary and on demand, and stops it at the end.
func (s *Sampler) run(seg *segment) {
	defer close(s.done)
	defer cpuBusy.Store(false)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	// A rate other than the runtime's rotates only when asked, so the
	// runtime's notice is not printed every minute.
	rotateOnWindow := s.hz == 100
	for {
		var reply chan error
		cancelled, stop := false, false
		select {
		case <-ticker.C:
			now := time.Now()
			if !s.settings.AutoEnd.IsZero() && !now.Before(s.settings.AutoEnd) {
				stop = true
			} else if windowOf(now) == s.currentWindow() || !rotateOnWindow {
				continue
			}
		case reply = <-s.flush:
		case cancelled = <-s.stopping:
			stop = true
		}
		pprof.StopCPUProfile()
		err := s.addSegment(seg)
		if !stop && err == nil {
			s.rotateWindow(time.Now())
			seg, err = s.startSegment()
		}
		if reply != nil {
			reply <- err
		}
		if err != nil {
			s.err = err
			s.stopped.Store(true)
			if s.env.OnComplete != nil {
				s.env.OnComplete(s, err)
			}
			return
		}
		if stop {
			auto := s.stopped.CompareAndSwap(false, true)
			if !cancelled {
				s.measure(s.currentWindow(), time.Now())
			}
			if auto && s.env.OnComplete != nil {
				s.env.OnComplete(s, nil)
			}
			return
		}
	}
}

func (s *Sampler) currentWindow() int { return int(s.window.Load()) }

// rotateWindow moves to the window of now, measuring the one that ended
// and pruning windows past the history. spark: JavaSampler.InsertDataTask
func (s *Sampler) rotateWindow(now time.Time) {
	w := windowOf(now)
	previous := s.currentWindow()
	if w == previous {
		return
	}
	s.mu.Lock()
	s.windowStarts[w] = now
	s.mu.Unlock()
	s.tickMu.Lock()
	s.window.Store(int64(w))
	s.tickMu.Unlock()

	s.measure(previous, now)

	drop := func(window int) bool { return window < w-historySize }
	s.mu.Lock()
	for name, root := range s.threads {
		if root.prune(drop) {
			delete(s.threads, name)
		}
	}
	for window := range s.windowStats {
		if drop(window) {
			delete(s.windowStats, window)
		}
	}
	for window := range s.windowStarts {
		if drop(window) {
			delete(s.windowStarts, window)
		}
	}
	for window := range s.pprofs {
		if drop(window) {
			delete(s.pprofs, window)
		}
	}
	s.mu.Unlock()
}

// measure records the statistics of window, which ends at end.
// spark: WindowStatisticsCollector.measureNow
func (s *Sampler) measure(window int, end time.Time) {
	s.mu.Lock()
	if _, done := s.windowStats[window]; done {
		s.mu.Unlock()
		return
	}
	start, ok := s.windowStarts[window]
	if !ok {
		start = end.Add(-windowSize)
	}
	s.mu.Unlock()
	s.tickMu.Lock()
	ticks := s.windowTicks
	s.windowTicks = 0
	s.tickMu.Unlock()
	var stats *sparkproto.WindowStatistics
	if s.env.MeasureWindow != nil {
		stats = s.env.MeasureWindow(start, end, ticks)
	} else {
		stats = &sparkproto.WindowStatistics{StartTime: start.UnixMilli(), EndTime: end.UnixMilli(), Duration: int32(end.Sub(start).Milliseconds()), Ticks: int32(ticks)}
	}
	s.mu.Lock()
	s.windowStats[window] = stats
	s.mu.Unlock()
}

// Flush folds the samples so far into the tree, for an export while the
// sampler runs.
func (s *Sampler) Flush() error {
	if s.stopped.Load() {
		<-s.done
		return s.err
	}
	reply := make(chan error, 1)
	select {
	case s.flush <- reply:
		return <-reply
	case <-s.done:
		return s.err
	}
}

// Stop stops sampling and waits for the last samples; cancelled skips
// measuring the last window. It does nothing after the sampler stopped.
// spark: Sampler.stop
func (s *Sampler) Stop(cancelled bool) {
	if !s.stopped.CompareAndSwap(false, true) {
		<-s.done
		return
	}
	s.stopping <- cancelled
	<-s.done
}

// Stopped reports whether the sampler has stopped.
func (s *Sampler) Stopped() bool { return s.stopped.Load() }

// StartTime is when sampling began.
func (s *Sampler) StartTime() time.Time { return s.start }

// AutoEnd is when the sampler stops by itself; zero for never.
func (s *Sampler) AutoEnd() time.Time { return s.settings.AutoEnd }

// Background reports whether this is the background profiler.
func (s *Sampler) Background() bool { return s.settings.Background }

// Interval is the time between samples.
func (s *Sampler) Interval() time.Duration { return s.interval }

// OnTickStart is called at the start of each tick on the tick goroutine: it
// labels the goroutine as the server thread, with the tick number when only
// long ticks are kept, and counts the tick.
func (s *Sampler) OnTickStart(tick int64) {
	if s.tickRoot.Load() == nil {
		root := goroutineRoot()
		s.tickRoot.Store(&root)
		if !s.settings.OnlyTicksOver {
			pprof.SetGoroutineLabels(pprof.WithLabels(context.Background(), pprof.Labels(labelThread, ServerThread)))
		}
	}
	if s.settings.OnlyTicksOver {
		pprof.SetGoroutineLabels(pprof.WithLabels(context.Background(),
			pprof.Labels(labelThread, ServerThread, labelTick, strconv.FormatInt(tick, 10))))
	}
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	if s.firstTick < 0 {
		s.firstTick = tick
	}
	s.lastTick = tick
	if !s.settings.OnlyTicksOver {
		s.windowTicks++
	}
}

// OnTickEnd is called at the end of each tick with its duration.
func (s *Sampler) OnTickEnd(tick int64, d time.Duration) {
	if !s.settings.OnlyTicksOver {
		return
	}
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	if d >= s.settings.TicksOver {
		s.overTicks[tick] = s.currentWindow()
		s.windowTicks++
	} else {
		s.underTicks[tick] = true
	}
}

// goroutineRoot answers the function the calling goroutine was started with.
func goroutineRoot() string {
	pcs := make([]uintptr, 512)
	n := runtime.Callers(0, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	last, previous := "", ""
	for {
		f, more := frames.Next()
		previous, last = last, f.Function
		if !more {
			break
		}
	}
	if last == "runtime.goexit" {
		return previous
	}
	return last
}

// TotalTicks answers the ticks the sampler saw, -1 for none.
func (s *Sampler) TotalTicks() int {
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	if s.firstTick < 0 {
		return -1
	}
	return int(s.lastTick - s.firstTick)
}

// addSegment parses a stopped segment's profile into the trees, under the
// current window.
func (s *Sampler) addSegment(seg *segment) error {
	data := seg.buf.Bytes()
	if len(data) == 0 {
		return nil
	}
	p, err := profile.ParseData(data)
	if err != nil {
		return err
	}
	cpu := -1
	for i, st := range p.SampleType {
		if st.Type == "cpu" {
			cpu = i
		}
	}
	if cpu < 0 {
		return errors.New("sampler: the CPU profile has no cpu samples")
	}
	root := ""
	if r := s.tickRoot.Load(); r != nil {
		root = *r
	}
	window := s.currentWindow()
	s.mu.Lock()
	previous := s.pprofs[window]
	s.mu.Unlock()
	merged := p
	if previous != nil {
		if m, err := profile.Merge([]*profile.Profile{previous, p}); err == nil {
			merged = m
		}
	}
	samples := make([]pendingSample, 0, len(p.Sample))
	for _, sample := range p.Sample {
		if ps, ok := s.describe(sample, sample.Value[cpu]/1000, window, root); ok {
			samples = append(samples, ps)
		}
	}
	over, under := s.tickSets()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pprofs[window] = merged
	if !s.settings.OnlyTicksOver {
		for _, ps := range samples {
			s.log(ps.group, ps.name, ps.stack, ps.micros, ps.window)
		}
		return nil
	}
	s.pendingTicked = append(s.pendingTicked, samples...)
	s.resolvePending(over, under)
	return nil
}

// tickSets copies the ticks known to be over and under the threshold.
func (s *Sampler) tickSets() (over map[int64]int, under map[int64]bool) {
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	// Ticks under the threshold matter only while samples may still come
	// in for them.
	for t := range s.underTicks {
		if t < s.lastTick-200 {
			delete(s.underTicks, t)
		}
	}
	return maps.Clone(s.overTicks), maps.Clone(s.underTicks)
}

// describe turns a sample of micros microseconds into the stack and the
// thread it is logged under; false for a sample not kept.
func (s *Sampler) describe(sample *profile.Sample, micros int64, window int, tickRoot string) (pendingSample, bool) {
	// Frames run from the innermost call out; a location's lines from the
	// inlined callee out to its caller.
	var functions []string
	var lines []int32
	for _, loc := range sample.Location {
		for _, line := range loc.Line {
			if line.Function == nil {
				continue
			}
			functions = append(functions, line.Function.Name)
			lines = append(lines, int32(line.Line))
		}
	}
	if len(functions) == 0 {
		return pendingSample{}, false
	}
	outer := len(functions) - 1
	if functions[outer] == "runtime.goexit" && outer > 0 {
		functions, lines, outer = functions[:outer], lines[:outer], outer-1
	}
	name := s.threadName(functions[outer], firstLabel(sample.Label[labelThread]), len(sample.Location) >= maxCPUProfStack, tickRoot)
	if !s.settings.Threads.includes(name) {
		return pendingSample{}, false
	}
	stack := make([]Frame, len(functions))
	parentLine := int32(-1)
	for i := range functions {
		fn := functions[outer-i]
		_, class, method := SplitFunction(fn)
		stack[i] = Frame{Class: class, Method: method, Line: lines[outer-i], ParentLine: parentLine}
		parentLine = lines[outer-i]
	}
	ps := pendingSample{tick: -1, group: s.settings.Grouper.group(name), name: name, stack: stack, micros: micros, window: window}
	if s.settings.OnlyTicksOver {
		tick, err := strconv.ParseInt(firstLabel(sample.Label[labelTick]), 10, 64)
		if err != nil {
			return pendingSample{}, false // not part of a tick
		}
		ps.tick = tick
	}
	return ps, true
}

// maxCPUProfStack is the most calls the runtime records for a CPU sample;
// a stack that long may have lost its outermost calls.
const maxCPUProfStack = 64

// threadName names the goroutine a sample came from: ServerThread for the
// tick goroutine, else the function it was started with (root, the
// outermost call). Labels are inherited by the goroutines a goroutine
// starts, so a label alone decides only when the root may be cut off (a
// truncated stack) or is the runtime's (its traceback failed).
func (s *Sampler) threadName(root, label string, truncated bool, tickRoot string) string {
	labelled := label == ServerThread
	switch {
	case truncated && labelled:
		return ServerThread
	case truncated:
		return "unknown goroutine (deep stack)"
	case tickRoot != "" && root == tickRoot:
		return ServerThread
	case labelled && (tickRoot == "" || strings.HasPrefix(root, "runtime.")):
		return ServerThread
	}
	return shortName(root)
}

func firstLabel(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// log adds a sample to its group's tree; call it with mu held.
func (s *Sampler) log(group, name string, stack []Frame, micros int64, window int) {
	root, ok := s.threads[group]
	if !ok {
		root = newNode(Frame{})
		s.threads[group] = root
		s.members[group] = map[string]bool{}
	}
	s.members[group][name] = true
	root.log(stack, micros, window)
}

// resolvePending logs the samples of ticks that ended over the threshold,
// under the window the tick ended in, and drops those of ticks under it;
// call it with mu held.
func (s *Sampler) resolvePending(over map[int64]int, under map[int64]bool) {
	s.pendingTicked = slices.DeleteFunc(s.pendingTicked, func(p pendingSample) bool {
		if w, ok := over[p.tick]; ok {
			s.log(p.group, p.name, p.stack, p.micros, w)
			return true
		}
		return under[p.tick]
	})
}

// ExportOptions are the choices of one export. spark: Sampler.ExportProps
type ExportOptions struct {
	// SeparateParentCalls keeps calls of a method from different lines
	// apart (--separate-parent-calls).
	SeparateParentCalls bool
	// Source answers the plugin a frame's package belongs to.
	Source func(pkg string) (string, bool)
}

// Export answers the call trees, windows and their statistics; the caller
// fills in the metadata. Flush first for the latest samples.
// spark: AbstractSampler.writeDataToProto
func (s *Sampler) Export(opts ExportOptions) *sparkproto.SamplerData {
	s.mu.Lock()
	defer s.mu.Unlock()
	windows := map[int]bool{}
	type thread struct {
		label string
		root  *node
	}
	var threads []thread
	for group, root := range s.threads {
		root.windows(windows)
		threads = append(threads, thread{s.settings.Grouper.label(group, len(s.members[group])), root})
	}
	slices.SortFunc(threads, func(a, b thread) int { return cmp.Compare(a.label, b.label) })
	enc := newTimeEncoder(windows)
	data := &sparkproto.SamplerData{TimeWindowStatistics: map[int32]*sparkproto.WindowStatistics{}}
	for _, w := range enc.keys {
		data.TimeWindows = append(data.TimeWindows, int32(w))
		stats, ok := s.windowStats[w]
		if !ok {
			stats = &sparkproto.WindowStatistics{Duration: int32(windowSize.Milliseconds())}
		}
		data.TimeWindowStatistics[int32(w)] = stats
	}
	classSources := map[string]string{}
	for _, t := range threads {
		data.Threads = append(data.Threads, exportThread(t.label, t.root, enc, opts.SeparateParentCalls))
	}
	if opts.Source != nil {
		for _, t := range data.Threads {
			for _, n := range t.Children {
				if _, seen := classSources[n.ClassName]; seen {
					continue
				}
				pkg, _, _ := SplitFunction(n.ClassName + ".x")
				if src, ok := opts.Source(pkg); ok {
					classSources[n.ClassName] = src
				} else {
					classSources[n.ClassName] = ""
				}
			}
		}
		for class, src := range classSources {
			if src == "" {
				delete(classSources, class)
			}
		}
		if len(classSources) > 0 {
			data.ClassSources = classSources
		}
	}
	return data
}

// WindowStatistics answers the statistics of the windows measured so far.
func (s *Sampler) WindowStatistics() map[int32]*sparkproto.WindowStatistics {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int32]*sparkproto.WindowStatistics, len(s.windowStats))
	for w, stats := range s.windowStats {
		out[int32(w)] = stats
	}
	return out
}

// Metadata fills the sampler's own part of a profile's metadata.
// spark: AbstractSampler.writeMetadataToProto
func (s *Sampler) Metadata(m *sparkproto.SamplerMetadata) {
	m.SamplerEngine = sparkproto.SamplerMetadata_ASYNC
	m.SamplerEngineVersion = "Go " + runtime.Version() + " CPU profiler"
	m.SamplerMode = sparkproto.SamplerMetadata_EXECUTION
	m.StartTime = s.start.UnixMilli()
	m.Interval = int32(s.interval.Microseconds())
	m.ThreadDumper = s.settings.Threads.proto()
	agg := &sparkproto.SamplerMetadata_DataAggregator{Type: sparkproto.SamplerMetadata_DataAggregator_SIMPLE, ThreadGrouper: s.settings.Grouper.proto()}
	if s.settings.OnlyTicksOver {
		s.tickMu.Lock()
		agg.Type = sparkproto.SamplerMetadata_DataAggregator_TICKED
		agg.TickLengthThreshold = s.settings.TicksOver.Microseconds()
		agg.NumberOfIncludedTicks = int32(len(s.overTicks))
		s.tickMu.Unlock()
	}
	m.DataAggregator = agg
	if ticks := s.TotalTicks(); ticks >= 0 {
		m.NumberOfTicks = int32(ticks)
	}
}

// WritePprof writes the samples so far as a gzipped pprof profile, every
// goroutine included, with the tick goroutine labelled spark-thread.
func (s *Sampler) WritePprof(w interface{ Write([]byte) (int, error) }) error {
	s.mu.Lock()
	windows := slices.Sorted(maps.Keys(s.pprofs))
	var ps []*profile.Profile
	for _, window := range windows {
		ps = append(ps, s.pprofs[window])
	}
	s.mu.Unlock()
	if len(ps) == 0 {
		return errors.New("sampler: no samples yet")
	}
	p, err := profile.Merge(ps)
	if err != nil {
		return err
	}
	return p.Write(w)
}

// syncBuffer is the bytes a CPU profile is written into, read once the
// profile stopped.
type syncBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data
}
