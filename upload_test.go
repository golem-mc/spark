package spark_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/plugintest"
	"github.com/golem-mc/spark/internal/sparkproto"
	"google.golang.org/protobuf/proto"
)

// fakeBytebin is a bytebin that keeps what it is sent.
type fakeBytebin struct {
	mu      sync.Mutex
	uploads map[string][]byte // content type -> body
}

// start runs a test server in a temporary directory, where the server and
// spark write their files, without the background profiler.
func start(t *testing.T, opts ...plugintest.Option) *plugintest.Server {
	t.Helper()
	// plugintest never disables a server's plugins
	// (golem-mc/golem#83), so a background profiler would keep the
	// process's one CPU profile after its test.
	t.Setenv("SPARK_BACKGROUNDPROFILER", "false")
	return startWithBackground(t, opts...)
}

// startWithBackground is start with the background profiler running; the
// test must cancel it before it ends.
func startWithBackground(t *testing.T, opts ...plugintest.Option) *plugintest.Server {
	t.Helper()
	t.Chdir(t.TempDir())
	return plugintest.Start(t, opts...)
}

func startBytebin(t *testing.T) *fakeBytebin {
	t.Helper()
	b := &fakeBytebin{uploads: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(zr)
		b.mu.Lock()
		b.uploads[r.Header.Get("Content-Type")] = data
		b.mu.Unlock()
		w.Header().Set("Location", "testkey")
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SPARK_BYTEBINURL", srv.URL)
	t.Setenv("SPARK_VIEWERURL", "https://viewer.test/")
	return b
}

func (b *fakeBytebin) get(contentType string, msg proto.Message) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.uploads[contentType]
	return ok && proto.Unmarshal(data, msg) == nil
}

// TestHealthUpload uploads a health report and finds it, with the server's
// metadata, in the activity log.
func TestHealthUpload(t *testing.T) {
	bin := startBytebin(t)
	srv := start(t, plugintest.Ops("Alice"))
	alice := srv.Join("Alice")

	alice.Run("/spark health upload")
	got := strings.Join(alice.Messages(10), "\n")
	for _, want := range []string{"Generating server health report...", "Health Report:", "https://viewer.test/testkey"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	var data sparkproto.HealthData
	if !bin.get("application/x-spark-health", &data) {
		t.Fatal("no health report uploaded")
	}
	md := data.GetMetadata()
	if md.GetPlatformMetadata().GetName() != "golem" || md.GetPlatformMetadata().GetMinecraftVersion() != "26.3" {
		t.Errorf("platform = %v", md.GetPlatformMetadata())
	}
	if md.GetCreator().GetName() != "Alice" || md.GetCreator().GetType() != sparkproto.CommandSenderMetadata_PLAYER {
		t.Errorf("creator = %v", md.GetCreator())
	}
	stats := md.GetPlatformStatistics()
	if stats.GetPlayerCount() != 1 || stats.GetTps().GetGameTargetTps() != 20 || stats.GetMemory().GetHeap().GetUsed() <= 0 {
		t.Errorf("platform statistics = %v", stats)
	}
	if len(stats.GetWorld().GetWorlds()) == 0 || md.GetSystemStatistics().GetCpu().GetThreads() <= 0 {
		t.Errorf("world = %v, system = %v", stats.GetWorld(), md.GetSystemStatistics())
	}
	world := stats.GetWorld()
	var keepInventory *sparkproto.WorldStatistics_GameRule
	for _, rule := range world.GetGameRules() {
		if rule.Name == "keep_inventory" {
			keepInventory = rule
		}
	}
	if keepInventory == nil || keepInventory.DefaultValue != "false" || len(keepInventory.WorldValues) != len(world.GetWorlds()) {
		t.Errorf("keep_inventory = %v", keepInventory)
	}
	if packs := world.GetDataPacks(); len(packs) == 0 || packs[0].Source != "builtin" {
		t.Errorf("data packs = %v", packs)
	}
	if _, ok := md.GetSources()["spark"]; !ok {
		t.Errorf("sources = %v", md.GetSources())
	}

	alice.Run("/spark activity")
	if got := strings.Join(alice.Messages(3), "\n"); !strings.Contains(got, "#1 - Health report - ") ||
		!strings.Contains(got, "Created by: Alice") || !strings.Contains(got, "Url: https://viewer.test/testkey") {
		t.Errorf("activity:\n%s", got)
	}
}
