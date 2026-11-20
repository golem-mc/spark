package spark_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/plugintest"
	"github.com/golem-mc/spark/internal/sparkproto"
	"github.com/golem-mc/spark/internal/viewer/viewertest"
)

// TestLiveViewer opens the profiler and the health dashboard in a fake
// viewer through a fake bytesocks, and trusts the viewer.
func TestLiveViewer(t *testing.T) {
	bin := startBytebin(t)
	bs := viewertest.NewBytesocks(t)
	t.Setenv("SPARK_BYTESOCKSHOST", bs.URL)
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
	isConnect := func(p *sparkproto.PacketWrapper) bool { return p.GetServerConnectResponse() != nil }
	isPong := func(p *sparkproto.PacketWrapper) bool { return p.GetServerPong() != nil }

	say("/spark profiler open", 1, "The profiler isn't running!")
	say("/spark profiler start", 1, "Profiler is now running!")
	say("/spark profiler open", 20, "Profiler live viewer:", "https://viewer.test/testkey",
		"(NOTE: this link is temporary and will expire after a short period of time.", "/spark profiler stop")
	var data sparkproto.SamplerData
	if !bin.get("application/x-spark-sampler", &data) || data.ChannelInfo == nil {
		t.Fatal("no profile with a channel uploaded")
	}
	v := bs.Connect(t, data.ChannelInfo)
	v.Connect(t, "viewer1")
	if got := v.Next(t, 5*time.Second, isConnect).GetServerConnectResponse(); got.State != sparkproto.ServerConnectResponse_UNTRUSTED {
		t.Errorf("connect: %v", got)
	}
	say("/spark profiler trust-viewer", 1, "Please provide a client id with '--id <client id>'.")
	say("/spark profiler trust-viewer --id nobody", 1, "Unable to find pending client with id 'nobody'.")
	say("/spark profiler trust-viewer --id viewer1", 1, "Client connected to the viewer using id 'viewer1' is now trusted.")
	if got := v.Next(t, 5*time.Second, isConnect).GetServerConnectResponse(); got.State != sparkproto.ServerConnectResponse_ACCEPTED {
		t.Errorf("after trust-viewer: %v", got)
	}
	dir := filepath.Join(filepath.Dir(srv.Host().DataDir()), "spark")
	if config, err := os.ReadFile(filepath.Join(dir, "config.json")); err != nil || !strings.Contains(string(config), `"trustedKeys": [`) {
		t.Errorf("config.json: %s %v", config, err)
	}
	say("/spark profiler cancel", 1, "Profiler has been cancelled.")
	if pong := v.Next(t, 5*time.Second, isPong).GetServerPong(); pong.Ok {
		t.Error("the profiler's socket did not close with it")
	}

	say("/spark health", 20, "Generating server health report...", "Health Report:")
	var health sparkproto.HealthData
	if !bin.get("application/x-spark-health", &health) || health.ChannelInfo == nil {
		t.Fatal("no dashboard with a channel uploaded")
	}
	dash := bs.Connect(t, health.ChannelInfo)
	dash.Ping(t, 7)
	if pong := dash.Next(t, 5*time.Second, isPong).GetServerPong(); !pong.Ok || pong.Data != 7 {
		t.Errorf("dashboard pong: %v", pong)
	}
	say("/spark health upload", 20, "Health Report:")
	if !bin.get("application/x-spark-health", &health) || health.ChannelInfo != nil {
		t.Error("health upload opened a socket")
	}
	say("/spark activity", 1, "Profiler (live)")
}
