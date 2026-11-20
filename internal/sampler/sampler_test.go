package sampler

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/spark/internal/sparkproto"
	"github.com/google/pprof/profile"
)

//go:noinline
func serverWork(d time.Duration) int {
	n := 0
	for start := time.Now(); time.Since(start) < d; {
		n++
	}
	return n
}

//go:noinline
func otherWork(d time.Duration) int {
	n := 0
	for start := time.Now(); time.Since(start) < d; {
		n++
	}
	return n
}

// runTicks plays a tick loop on its own goroutine: each tick works for
// long() and then idles, until stop.
func runTicks(s *Sampler, long func(tick int64) bool, stop <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for tick := int64(1); ; tick++ {
			select {
			case <-stop:
				return
			default:
			}
			start := time.Now()
			s.OnTickStart(tick)
			d := 2 * time.Millisecond
			if long(tick) {
				d = 25 * time.Millisecond
			}
			serverWork(d)
			s.OnTickEnd(tick, time.Since(start))
			time.Sleep(time.Millisecond)
		}
	}()
	return done
}

func names(data *sparkproto.SamplerData) []string {
	var out []string
	for _, t := range data.Threads {
		out = append(out, t.Name)
	}
	return out
}

func methods(t *sparkproto.ThreadNode) string {
	var b strings.Builder
	for _, n := range t.Children {
		b.WriteString(n.ClassName + "." + n.MethodName + "\n")
	}
	return b.String()
}

// profileFor samples a tick loop and a busy goroutine for 600ms, the time a
// few dozen samples at 100 Hz need.
func profileFor(t *testing.T, settings Settings, long func(int64) bool) (*Sampler, *sparkproto.SamplerData) {
	t.Helper()
	s, err := Start(settings, Env{})
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	ticks := runTicks(s, long, stop)
	other := make(chan struct{})
	go func() {
		defer close(other)
		for {
			select {
			case <-stop:
				return
			default:
				otherWork(5 * time.Millisecond)
			}
		}
	}()
	time.Sleep(600 * time.Millisecond)
	s.Stop(false)
	close(stop)
	<-ticks
	<-other
	return s, s.Export(ExportOptions{Source: func(pkg string) (string, bool) {
		return "testplugin", pkg == "github.com/golem-mc/spark/internal/sampler"
	}})
}

func TestServerThread(t *testing.T) {
	_, data := profileFor(t, Settings{Threads: ServerThreadOnly(), Grouper: ByPool}, func(int64) bool { return true })
	if got := names(data); len(got) != 1 || got[0] != ServerThread {
		t.Fatalf("threads = %q, want only the server thread", got)
	}
	m := methods(data.Threads[0])
	if !strings.Contains(m, "internal/sampler.serverWork") || strings.Contains(m, "otherWork") {
		t.Errorf("server thread frames:\n%s", m)
	}
	if data.ClassSources["github.com/golem-mc/spark/internal/sampler"] != "testplugin" {
		t.Errorf("class sources = %v", data.ClassSources)
	}
	if len(data.TimeWindows) != 1 || data.Threads[0].Times[0] <= 0 {
		t.Errorf("windows = %v, times = %v", data.TimeWindows, data.Threads[0].Times)
	}
}

func TestAllThreads(t *testing.T) {
	s, data := profileFor(t, Settings{Threads: AllThreads(), Grouper: ByName}, func(int64) bool { return true })
	all := strings.Join(names(data), "\n")
	if !strings.Contains(all, ServerThread) || !strings.Contains(all, "sampler.profileFor.func1") {
		t.Errorf("threads:\n%s", all)
	}
	var buf bytes.Buffer
	if err := s.WritePprof(&buf); err != nil {
		t.Fatal(err)
	}
	if p, err := profile.Parse(&buf); err != nil || len(p.Sample) == 0 {
		t.Errorf("pprof: %v", err)
	}
}

func TestOnlyTicksOver(t *testing.T) {
	s, data := profileFor(t, Settings{Threads: ServerThreadOnly(), OnlyTicksOver: true, TicksOver: 10 * time.Millisecond},
		func(tick int64) bool { return tick%4 == 0 })
	var md sparkproto.SamplerMetadata
	s.Metadata(&md)
	agg := md.DataAggregator
	if agg.Type != sparkproto.SamplerMetadata_DataAggregator_TICKED || agg.NumberOfIncludedTicks == 0 || agg.TickLengthThreshold != 10_000 {
		t.Errorf("aggregator = %v", agg)
	}
	if md.NumberOfTicks <= agg.NumberOfIncludedTicks {
		t.Errorf("ticks %d, included %d", md.NumberOfTicks, agg.NumberOfIncludedTicks)
	}
	if len(data.Threads) != 1 || !strings.Contains(methods(data.Threads[0]), "serverWork") {
		t.Errorf("threads = %q", names(data))
	}
}

func TestBusy(t *testing.T) {
	s, err := Start(Settings{Threads: AllThreads()}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(true)
	if _, err := Start(Settings{}, Env{}); err != ErrBusy {
		t.Errorf("second sampler: %v", err)
	}
}

func TestAutoEnd(t *testing.T) {
	done := make(chan error, 1)
	s, err := Start(Settings{Threads: AllThreads(), AutoEnd: time.Now().Add(100 * time.Millisecond)},
		Env{OnComplete: func(_ *Sampler, err error) { done <- err }})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil || !s.Stopped() {
			t.Errorf("completed with %v, stopped %v", err, s.Stopped())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the sampler did not stop at its end time")
	}
}

// TestHistoryPruned moves a sampler an hour on and finds its first window
// gone from the trees, the statistics and the pprof samples.
func TestHistoryPruned(t *testing.T) {
	s, err := Start(Settings{Threads: AllThreads(), Background: true}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(true)
	serverWork(100 * time.Millisecond)
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(s.Export(ExportOptions{}).Threads) == 0 {
		t.Fatal("no samples")
	}
	s.rotateWindow(time.Now().Add((historySize + 1) * windowSize))
	if data := s.Export(ExportOptions{}); len(data.Threads) != 0 || len(data.TimeWindows) != 0 {
		t.Errorf("after an hour: %d threads, windows %v", len(data.Threads), data.TimeWindows)
	}
	if len(s.WindowStatistics()) != 0 || s.WritePprof(io.Discard) == nil {
		t.Errorf("statistics %v, or pprof samples left", s.WindowStatistics())
	}
}

var allocated [][]byte

//go:noinline
func allocWork() {
	for range 512 {
		allocated = append(allocated, make([]byte, 64<<10))
	}
}

//go:noinline
func holdLock(mu *sync.Mutex) {
	mu.Lock()
	time.Sleep(2 * time.Millisecond)
	mu.Unlock()
}

func findFrame(data *sparkproto.SamplerData, method string) (thread string, value float64) {
	for _, t := range data.Threads {
		for _, n := range t.Children {
			if n.MethodName == method {
				for _, v := range n.Times {
					value += v
				}
				return t.Name, value
			}
		}
	}
	return "", 0
}

func TestAllocation(t *testing.T) {
	s, err := Start(Settings{Mode: Allocation, Threads: AllThreads()}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { allocWork(); close(done) }()
	<-done
	s.Stop(false) // collects garbage, which publishes the allocations
	allocated = nil
	data := s.Export(ExportOptions{})
	thread, size := findFrame(data, "allocWork")
	if !strings.Contains(thread, "TestAllocation.func") || size < 16<<20 {
		t.Errorf("allocWork: thread %q, %v bytes; threads %q", thread, size, names(data))
	}
	var md sparkproto.SamplerMetadata
	s.Metadata(&md)
	if md.SamplerMode != sparkproto.SamplerMetadata_ALLOCATION || md.Interval != int32(runtime.MemProfileRate) {
		t.Errorf("metadata: %v", &md)
	}
}

func TestLock(t *testing.T) {
	before := runtime.SetMutexProfileFraction(-1)
	s, err := Start(Settings{Mode: Lock, Threads: AllThreads()}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 10 {
				holdLock(&mu)
			}
		})
	}
	wg.Wait()
	s.Stop(false)
	if runtime.SetMutexProfileFraction(-1) != before {
		t.Error("the mutex profile fraction was not restored")
	}
	data := s.Export(ExportOptions{})
	if _, ms := findFrame(data, "holdLock"); ms <= 1 {
		t.Errorf("holdLock: %vms of contention; threads %q", ms, names(data))
	}
	var md sparkproto.SamplerMetadata
	s.Metadata(&md)
	if md.SamplerMode != sparkproto.SamplerMetadata_LOCK {
		t.Errorf("metadata: %v", &md)
	}
}
