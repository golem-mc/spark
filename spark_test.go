package spark_test

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/plugintest"
	_ "github.com/golem-mc/spark"
)

// TestCommands runs the /spark commands that need no upload, as an operator
// and as a player without permission, sharing one server.
func TestCommands(t *testing.T) {
	srv := start(t, plugintest.Ops("Alice"))
	alice := srv.Join("Alice")
	bob := srv.Join("Bob")

	run := func(line string, want ...string) {
		t.Helper()
		alice.Run(line)
		got := strings.Join(alice.Messages(3), "\n")
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in:\n%s", line, w, got)
			}
		}
	}
	run("/spark", "[⚡] spark v", "Run /spark help to view usage information.")
	run("/spark help", "> /spark health upload", "[--memory]", "> /spark ping", "[--player <username>]",
		"For full usage information, please go to: https://spark.lucko.me/docs/Command-Usage")
	run("/spark tps", "TPS from last 5s, 10s, 1m, 5m, 15m:", "Tick durations (min/med/95%ile/max ms) from last 10s, 1m:",
		"CPU usage from last 10s, 1m, 15m:", "(system)", "(process)")
	run("/spark cpu", "CPU usage from last 10s, 1m, 15m:")
	run("/tps", "TPS from last 5s, 10s, 1m, 5m, 15m:")
	run("/spark ping --player bob", "Player Bob has ", " ms ping.")
	run("/spark ping --player nobody", "Ping data is not available for 'nobody'.")
	run("/spark health show --memory", "Generating server health report...", "> TPS from last 5s, 10s, 1m, 5m, 15m:",
		"> Memory usage:", "> Non-heap memory usage:", "> Go heap pool usage:", "Usage at last GC:")
	run("/spark tps --bogus extra", "[⚡] TPS from last 5s")
	run("/spark ping extra", "Expected flag at position 0 but got 'extra' instead!")

	bob.Run("/spark tps")
	if got := strings.Join(bob.Messages(3), "\n"); !strings.Contains(got, "You do not have permission to use this command.") {
		t.Errorf("non-op /spark tps: %q", got)
	}

	if lines := srv.Console("spark tps"); !strings.Contains(strings.Join(lines, "\n"), "TPS from last 5s") {
		t.Errorf("console /spark tps: %q", lines)
	}
}
