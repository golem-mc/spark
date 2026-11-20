package sampler

import "testing"

func TestSplitFunction(t *testing.T) {
	cases := []struct{ name, pkg, class, method string }{
		{"github.com/golem-mc/golem/internal/server.(*Server).tickLoop", "github.com/golem-mc/golem/internal/server", "github.com/golem-mc/golem/internal/server.(*Server)", "tickLoop"},
		{"github.com/golem-mc/golem/internal/server.tickWorlds", "github.com/golem-mc/golem/internal/server", "github.com/golem-mc/golem/internal/server", "tickWorlds"},
		{"github.com/golem-mc/golem/internal/server.tickWorlds.func1", "github.com/golem-mc/golem/internal/server", "github.com/golem-mc/golem/internal/server", "tickWorlds.func1"},
		{"github.com/golem-mc/golem/internal/server.(*Server).run.func2.1", "github.com/golem-mc/golem/internal/server", "github.com/golem-mc/golem/internal/server.(*Server)", "run.func2.1"},
		{"example.com/p.point.String", "example.com/p", "example.com/p.point", "String"},
		{"runtime.mallocgc", "runtime", "runtime", "mallocgc"},
		{"runtime._GC", "runtime", "runtime", "_GC"},
		{"main.main", "main", "main", "main"},
		{"slices.SortFunc[go.shape.[]string,go.shape.string]", "slices", "slices", "SortFunc[...]"},
		{"example.com/p.(*Set[go.shape.int]).Add", "example.com/p", "example.com/p.(*Set[...])", "Add"},
		{"example.com/p.Listen[go.shape.struct { example.com/p.Base; Tick int64 },go.shape.*example.com/p.E].func1", "example.com/p", "example.com/p", "Listen[...].func1"},
		{"example.com/p.Run.gowrap1", "example.com/p", "example.com/p", "Run.gowrap1"},
	}
	for _, c := range cases {
		pkg, class, method := SplitFunction(c.name)
		if pkg != c.pkg || class != c.class || method != c.method {
			t.Errorf("%s: (%q, %q, %q), want (%q, %q, %q)", c.name, pkg, class, method, c.pkg, c.class, c.method)
		}
	}
}

func TestThreads(t *testing.T) {
	if !ServerThreadOnly().includes("server THREAD") || ServerThreadOnly().includes("x") {
		t.Error("server thread only")
	}
	re, err := MatchingThreads([]string{"server.*"})
	if err != nil || !re.includes("Server thread") || re.includes("a server") {
		t.Errorf("regex: %v", err)
	}
	if got := re.proto().Patterns; len(got) != 1 || got[0] != "server.*" {
		t.Errorf("patterns = %q", got)
	}
	if ByPool.group("Worker-Main-3") != "Worker-Main" || ByPool.label("Worker-Main", 2) != "Worker-Main (x2)" || AsOne.label("root", 3) != "All (x3)" {
		t.Error("grouping")
	}
}
