package heap

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var retained [][]byte

//go:noinline
func retain() {
	for range 256 {
		retained = append(retained, make([]byte, 64<<10))
	}
}

func TestSummary(t *testing.T) {
	retain()
	runtime.GC()
	entries, err := Summary()
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range entries {
		if e.Order != i+1 || (i > 0 && e.Bytes > entries[i-1].Bytes) {
			t.Fatalf("entry %d out of order: %+v", i, e)
		}
		if e.Type == "github.com/golem-mc/spark/internal/heap.retain" {
			if e.Bytes < 8<<20 || e.Instances < 100 {
				t.Errorf("retain: %+v", e)
			}
			return
		}
	}
	t.Errorf("no entry for retain in %v", entries)
}

func TestDumpAndGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heap.heapdump")
	if err := Dump(path); err != nil {
		t.Fatal(err)
	}
	var last int64
	gz, err := Gzip(path, func(n int64) { last = n })
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if last != info.Size() || gz != path+".gz" {
		t.Errorf("progress %d of %d, file %s", last, info.Size(), gz)
	}
	f, _ := os.Open(gz)
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 16)
	io.ReadFull(zr, head)
	if !strings.HasPrefix(string(head), "go1.7 heap dump") {
		t.Errorf("header %q", head)
	}
}
