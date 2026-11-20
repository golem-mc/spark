package spark_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/plugintest"
	"github.com/golem-mc/spark/internal/sparkproto"
)

// busywork is a plugin that spends a few milliseconds of every tick in burn.
type busywork struct{}

func (busywork) Enable(s plugin.Server) error {
	plugin.Listen(s, func(*plugin.ServerTickStartEvent) { burn(15 * time.Millisecond) })
	return nil
}

func (busywork) Disable() {}

//go:noinline
func burn(d time.Duration) int {
	n := 0
	for start := time.Now(); time.Since(start) < d; {
		n++
	}
	return n
}

func hasFrame(t *sparkproto.ThreadNode, method string) bool {
	for _, n := range t.Children {
		if n.MethodName == method {
			return true
		}
	}
	return false
}

// TestProfiler profiles a server whose plugin works on every tick, and
// finds the plugin's frames under the server thread in the upload. It runs
// about two seconds of ticks: CPU samples need real time.
func TestProfiler(t *testing.T) {
	bin := startBytebin(t)
	srv := startWithBackground(t, plugintest.Ops("Alice"),
		plugintest.WithPlugins(append(plugin.Registered(), plugin.Registration{Name: "busywork", Plugin: busywork{}})...))
	alice := srv.Join("Alice")
	say := func(line string, ticks int, want ...string) string {
		t.Helper()
		alice.Run(line)
		got := strings.Join(alice.Messages(ticks), "\n")
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in:\n%s", line, w, got)
			}
		}
		return got
	}

	say("/spark profiler", 2, "Profiler is already running!",
		"It was started automatically when spark enabled and has been running in the background for")
	// As in spark, start stops the background profiler before it checks its flags.
	say("/spark profiler start --timeout 5", 2, "Stopping the background profiler before starting... please wait",
		"The specified timeout is not long enough")
	say("/spark profiler start", 2, "Starting a new profiler, please wait...", "Profiler is now running!", "/spark profiler stop", "/spark profiler open")
	say("/spark profiler start", 25, "Profiler is already running!", "So far, it has profiled for")
	say("/spark profiler stop --comment busy test", 10, "Stopping the profiler & uploading results, please wait...",
		"Profiler stopped & upload complete!", "https://viewer.test/testkey",
		"Restarted the background profiler. (If you don't want this to happen, run: /spark profiler cancel)")

	var data sparkproto.SamplerData
	if !bin.get("application/x-spark-sampler", &data) {
		t.Fatal("no profile uploaded")
	}
	if len(data.Threads) != 1 || data.Threads[0].Name != "Server thread" {
		t.Fatalf("threads = %v", data.Threads)
	}
	if !hasFrame(data.Threads[0], "burn") {
		t.Errorf("the plugin's burn is not under the server thread")
	}
	md := data.Metadata
	if md.Comment != "busy test" || md.SamplerEngine != sparkproto.SamplerMetadata_ASYNC || md.NumberOfTicks <= 0 ||
		md.PlatformMetadata.GetName() != "golem" || md.Creator.GetName() != "Alice" || md.Interval != 10_000 {
		t.Errorf("metadata = %v", md)
	}
	if len(data.TimeWindows) == 0 || data.TimeWindowStatistics[data.TimeWindows[0]] == nil {
		t.Errorf("windows = %v", data.TimeWindows)
	}

	say("/spark profiler start --thread * --only-ticks-over 10", 10, "Stopping the background profiler", "Profiler is now running!")
	say("/spark profiler stop --save-to-file", 10, "Profiler stopped & save complete!", "Data has been written to: ",
		"You can view the profile file using the web app @ https://viewer.test/", "A pprof profile has been written to: ",
		"Restarted the background profiler.")
	dir := filepath.Join(filepath.Dir(srv.Host().DataDir()), "spark")
	for _, pattern := range []string{"profile-*.sparkprofile", "profile-*.pprof"} {
		if files, _ := filepath.Glob(filepath.Join(dir, pattern)); len(files) != 1 {
			t.Errorf("%s: %v", pattern, files)
		} else if info, err := os.Stat(files[0]); err != nil || info.Size() == 0 {
			t.Errorf("%s is empty", files[0])
		}
	}

	say("/spark profiler cancel", 2, "Profiler has been cancelled.") // frees the CPU profile for later tests
	say("/spark profiler stop", 2, "There isn't an active profiler running.")
	say("/spark activity", 2, "Profiler - ", "File: ", "Url: https://viewer.test/testkey")
	if config, err := os.ReadFile(filepath.Join(dir, "config.json")); err != nil || !strings.Contains(string(config), `"backgroundProfiler": true`) {
		t.Errorf("config.json: %s %v", config, err)
	}
}
