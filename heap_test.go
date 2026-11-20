package spark_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/plugintest"
	"github.com/golem-mc/spark/internal/sparkproto"
)

func TestHeap(t *testing.T) {
	bin := startBytebin(t)
	srv := start(t, plugintest.Ops("Alice"))
	alice := srv.Join("Alice")
	say := func(line string, ticks int, want ...string) {
		t.Helper()
		alice.Run(line)
		got := strings.Join(alice.Messages(ticks), "\n")
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in:\n%s", line, w, got)
			}
		}
	}

	say("/spark heapsummary --run-gc-before", 10, "Running garbage collector...", "Creating a new heap dump summary, please wait...",
		"Heap dump summmary output:", "https://viewer.test/testkey")
	var data sparkproto.HeapData
	if !bin.get("application/x-spark-heap", &data) {
		t.Fatal("no heap summary uploaded")
	}
	if len(data.Entries) == 0 || data.Entries[0].Order != 1 || data.Entries[0].Size <= 0 || data.Metadata.GetPlatformMetadata().GetName() != "golem" {
		t.Errorf("heap data: %d entries, first %v", len(data.Entries), data.Entries[0])
	}

	say("/spark heapsummary --save-to-file", 10, "Heap dump summary written to: ", "You can read the heap dump summary file using the viewer web-app")
	say("/spark heapdump --compress gzip", 40, "Creating a new heap dump, please wait...", "Heap dump written to: ",
		"Compressing heap dump, please wait...", "Compression complete: ", "Compressed heap dump written to: ")
	dir := filepath.Join(filepath.Dir(srv.Host().DataDir()), "spark")
	for _, pattern := range []string{"heapsummary-*.sparkheap", "heap-*.heapdump", "heap-*.heapdump.gz"} {
		if files, _ := filepath.Glob(filepath.Join(dir, pattern)); len(files) != 1 {
			t.Errorf("%s: %v", pattern, files)
		} else if info, err := os.Stat(files[0]); err != nil || info.Size() == 0 {
			t.Errorf("%s is empty", files[0])
		}
	}
}
