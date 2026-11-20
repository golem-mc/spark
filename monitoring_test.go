package spark_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/golem-mc/golem/plugintest"
)

func TestMonitoring(t *testing.T) {
	srv := start(t, plugintest.Ops("Alice"))
	alice := srv.Join("Alice")
	expect := func(got []string, want ...string) {
		t.Helper()
		all := strings.Join(got, "\n")
		for _, w := range want {
			if !strings.Contains(all, w) {
				t.Errorf("missing %q in:\n%s", w, all)
			}
		}
	}
	say := func(line string, ticks int, want ...string) {
		t.Helper()
		alice.Run(line)
		expect(alice.Messages(ticks), want...)
	}

	say("/spark gc", 1, "Calculating GC statistics...", "Garbage Collector statistics", "Go collector:")

	say("/spark gcmonitor", 1, "GC monitor enabled.")
	runtime.GC()
	expect(alice.Messages(3), "Go GC lasting", "ms. (cause = forced)", "Go heap")
	say("/spark gcmonitor", 1, "GC monitor disabled.")
	runtime.GC()
	if got := alice.Messages(3); len(got) != 0 {
		t.Errorf("after disabling: %q", got)
	}

	say("/spark tickmonitor --threshold-tick 100", 3, "Tick monitor started. Before the monitor becomes fully active")
	say("/spark tickmonitor", 1, "Tick monitor disabled.")
}
