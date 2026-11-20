package sampler

import (
	"bytes"
	"errors"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"

	"github.com/google/pprof/profile"
)

// Mode is what a sampler measures. spark: SamplerMode
type Mode int

const (
	// Execution samples CPU time: the Go CPU profiler.
	Execution Mode = iota
	// Allocation measures bytes allocated: the runtime's allocation
	// profile, which samples about one allocation per runtime.MemProfileRate
	// bytes and publishes them at each collection.
	Allocation
	// Lock measures time goroutines waited for a sync.Mutex or RWMutex: the
	// runtime's mutex profile.
	Lock
)

// source is where a sampler's samples come from. collect answers the
// samples since the last call, and the type of the value to log (in its
// unit: nanoseconds or bytes); final ends the source.
type source interface {
	collect(final bool) (p *profile.Profile, valueType string, err error)
}

// cpuSource is the CPU profiler, restarted on each collect so that its
// samples can be read.
type cpuSource struct {
	hz  int
	buf *syncBuffer
}

func newCPUSource(hz int) (*cpuSource, error) {
	if !cpuBusy.CompareAndSwap(false, true) {
		return nil, ErrBusy
	}
	c := &cpuSource{hz: hz}
	if err := c.start(); err != nil {
		cpuBusy.Store(false)
		return nil, err
	}
	return c, nil
}

func (c *cpuSource) start() error {
	if c.hz != 100 {
		// StartCPUProfile sets 100 Hz; a rate set first wins, with a notice.
		runtime.SetCPUProfileRate(c.hz)
	}
	c.buf = &syncBuffer{}
	if err := pprof.StartCPUProfile(c.buf); err != nil {
		if c.hz != 100 {
			runtime.SetCPUProfileRate(0)
		}
		return ErrBusy
	}
	return nil
}

func (c *cpuSource) collect(final bool) (*profile.Profile, string, error) {
	pprof.StopCPUProfile()
	data := c.buf.Bytes()
	var err error
	if final {
		cpuBusy.Store(false)
	} else {
		err = c.start()
	}
	if len(data) == 0 {
		return nil, "cpu", err
	}
	p, perr := profile.ParseData(data)
	if perr != nil {
		return nil, "cpu", perr
	}
	return p, "cpu", err
}

// deltaSource reads one of the runtime's cumulative profiles and answers
// what each stack gained since the last read.
type deltaSource struct {
	name, valueType string
	previous        map[string][]int64
	// stop restores the runtime's settings when the source ends.
	stop func()
}

func newDeltaSource(mode Mode, liveOnly bool) (*deltaSource, error) {
	d := &deltaSource{previous: map[string][]int64{}, stop: func() {}}
	switch mode {
	case Allocation:
		d.name, d.valueType = "allocs", "alloc_space"
		if liveOnly {
			d.valueType = "inuse_space"
		}
	case Lock:
		d.name, d.valueType = "mutex", "delay"
		previous := runtime.SetMutexProfileFraction(1)
		d.stop = func() { runtime.SetMutexProfileFraction(previous) }
	default:
		return nil, errors.New("sampler: not a delta mode")
	}
	// The first read is the baseline: only what comes after counts.
	if _, _, err := d.collect(false); err != nil {
		d.stop()
		return nil, err
	}
	return d, nil
}

func (d *deltaSource) collect(final bool) (*profile.Profile, string, error) {
	if final {
		defer d.stop()
	}
	var buf bytes.Buffer
	if err := pprof.Lookup(d.name).WriteTo(&buf, 0); err != nil {
		return nil, d.valueType, err
	}
	p, err := profile.Parse(&buf)
	if err != nil {
		return nil, d.valueType, err
	}
	index := -1
	for i, st := range p.SampleType {
		if st.Type == d.valueType {
			index = i
		}
	}
	if index < 0 {
		return nil, d.valueType, errors.New("sampler: the " + d.name + " profile has no " + d.valueType)
	}
	current := make(map[string][]int64, len(p.Sample))
	kept := p.Sample[:0]
	for _, s := range p.Sample {
		key := stackKey(s)
		values := append([]int64(nil), s.Value...)
		if prev, ok := current[key]; ok {
			// The same stack twice: add them up.
			for i := range values {
				values[i] += prev[i]
			}
		}
		current[key] = values
	}
	for _, s := range p.Sample {
		key := stackKey(s)
		values, ok := current[key]
		if !ok {
			continue // already emitted
		}
		delete(current, key)
		prev := d.previous[key]
		delta := make([]int64, len(values))
		for i := range values {
			delta[i] = values[i]
			if i < len(prev) {
				delta[i] -= prev[i]
			}
		}
		d.previous[key] = values
		if delta[index] > 0 {
			s.Value = delta
			kept = append(kept, s)
		}
	}
	p.Sample = kept
	return p, d.valueType, nil
}

// stackKey identifies a sample's stack across reads of a profile.
func stackKey(s *profile.Sample) string {
	var b strings.Builder
	for _, loc := range s.Location {
		b.WriteString(strconv.FormatUint(loc.Address, 16))
		for _, line := range loc.Line {
			if line.Function != nil {
				b.WriteString(line.Function.Name)
			}
			b.WriteByte(':')
			b.WriteString(strconv.FormatInt(line.Line, 10))
		}
		b.WriteByte(';')
	}
	return b.String()
}
