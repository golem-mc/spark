// Package heap summarises and dumps the Go heap. spark: heapdump/*
package heap

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"strings"

	"github.com/golem-mc/spark/internal/sampler"
	"github.com/google/pprof/profile"
)

// Entry is one line of the summary: the objects allocated at one place
// that are still in use. spark: HeapDumpSummary.Entry
type Entry struct {
	// Order is the entry's rank by size, from 1.
	Order     int
	Instances int64
	Bytes     int64
	// Type is where the objects were allocated: the function that
	// allocated them, outside the runtime.
	Type string
}

// Summary answers the heap in use as of the last collection, by allocation
// site, largest first. The runtime samples about one allocation per 512 KiB
// and scales the counts up, so they are estimates.
// spark: HeapDumpSummary.createNew (a JVM's class histogram)
func Summary() ([]Entry, error) {
	var buf bytes.Buffer
	if err := pprof.Lookup("heap").WriteTo(&buf, 0); err != nil {
		return nil, err
	}
	p, err := profile.Parse(&buf)
	if err != nil {
		return nil, err
	}
	objects, space := -1, -1
	for i, st := range p.SampleType {
		switch st.Type {
		case "inuse_objects":
			objects = i
		case "inuse_space":
			space = i
		}
	}
	if objects < 0 || space < 0 {
		return nil, errors.New("heap: the profile has no in-use counts")
	}
	bySite := map[string]*Entry{}
	for _, s := range p.Sample {
		if s.Value[objects] == 0 && s.Value[space] == 0 {
			continue
		}
		site := allocationSite(s)
		e, ok := bySite[site]
		if !ok {
			e = &Entry{Type: site}
			bySite[site] = e
		}
		e.Instances += s.Value[objects]
		e.Bytes += s.Value[space]
	}
	entries := make([]Entry, 0, len(bySite))
	for _, e := range bySite {
		entries = append(entries, *e)
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		return cmp.Or(cmp.Compare(b.Bytes, a.Bytes), cmp.Compare(a.Type, b.Type))
	})
	for i := range entries {
		entries[i].Order = i + 1
	}
	return entries, nil
}

// allocationSite is the innermost function of a sample outside the runtime,
// as package.Function.
func allocationSite(s *profile.Sample) string {
	first := ""
	for _, loc := range s.Location {
		for _, line := range loc.Line {
			if line.Function == nil {
				continue
			}
			name := line.Function.Name
			if first == "" {
				first = name
			}
			if !strings.HasPrefix(name, "runtime.") && !strings.HasPrefix(name, "internal/") {
				_, class, method := sampler.SplitFunction(name)
				return class + "." + method
			}
		}
	}
	if first == "" {
		return "unknown"
	}
	return first
}

// Dump writes the runtime's heap dump (the format of debug.WriteHeapDump,
// not hprof) to path. It stops the world while it writes.
// spark: HeapDump.dumpHeap
func Dump(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	debug.WriteHeapDump(f.Fd())
	return f.Close()
}

// Gzip compresses path into path.gz, calling progress with the bytes read
// so far. spark: Compression.GZIP
func Gzip(path string, progress func(read int64)) (string, error) {
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	outPath := path + ".gz"
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	zw := gzip.NewWriter(out)
	_, err = io.Copy(zw, &progressReader{r: in, progress: progress})
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(outPath)
		return "", err
	}
	return outPath, nil
}

type progressReader struct {
	r        io.Reader
	read     int64
	progress func(int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.progress != nil && n > 0 {
		p.progress(p.read)
	}
	return n, err
}
