package viewer_test

import (
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/spark/internal/sparkproto"
	"github.com/golem-mc/spark/internal/viewer"
	"github.com/golem-mc/spark/internal/viewer/viewertest"
)

func connectResponse(p *sparkproto.PacketWrapper) bool { return p.GetServerConnectResponse() != nil }

func TestSocket(t *testing.T) {
	bs := viewertest.NewBytesocks(t)
	var saved []string
	keys := viewer.NewKeyStore(nil, func(encoded []string) { saved = encoded })
	var logs []string
	socket, err := viewer.Open(viewer.NewClient(bs.URL, "spark-plugin"), keys, func(msg string) { logs = append(logs, msg) })
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	if !socket.CheckShouldClose() {
		t.Error("a socket no viewer has pinged has nothing to send")
	}

	v := bs.Connect(t, socket.ChannelInfo())
	v.Connect(t, "client1")
	resp := v.Next(t, 5*time.Second, connectResponse).GetServerConnectResponse()
	if resp.ClientId != "client1" || resp.State != sparkproto.ServerConnectResponse_UNTRUSTED ||
		resp.Settings.GetSamplerInterval() != 60 || resp.Settings.GetStatisticsInterval() != 10 {
		t.Errorf("connect response = %v", resp)
	}
	if keys.TrustPending("nobody") {
		t.Error("trusted a client that never connected")
	}
	if !keys.TrustPending("client1") || len(saved) != 1 {
		t.Fatalf("trusting client1: saved %v", saved)
	}
	socket.SendClientTrusted("client1")
	if got := v.Next(t, 5*time.Second, connectResponse).GetServerConnectResponse(); got.State != sparkproto.ServerConnectResponse_ACCEPTED {
		t.Errorf("after trusting: %v", got)
	}
	// A viewer signing with the trusted key is accepted straight away.
	v.Connect(t, "client2")
	if got := v.Next(t, 5*time.Second, connectResponse).GetServerConnectResponse(); got.ClientId != "client2" || got.State != sparkproto.ServerConnectResponse_ACCEPTED {
		t.Errorf("trusted key: %v", got)
	}

	v.Ping(t, 42)
	if pong := v.Next(t, 5*time.Second, func(p *sparkproto.PacketWrapper) bool { return p.GetServerPong() != nil }).GetServerPong(); !pong.Ok || pong.Data != 42 {
		t.Errorf("pong = %v", pong)
	}
	if socket.CheckShouldClose() {
		t.Error("a pinged socket should send")
	}
	socket.SendSamplerUpdate("payload1")
	if got := v.Next(t, 5*time.Second, func(p *sparkproto.PacketWrapper) bool { return p.GetServerUpdateSampler() != nil }); got.GetServerUpdateSampler().PayloadId != "payload1" {
		t.Errorf("sampler update = %v", got)
	}
	socket.SendStatistics(&sparkproto.PlatformStatistics{PlayerCount: 3}, nil, nil)
	if got := v.Next(t, 5*time.Second, func(p *sparkproto.PacketWrapper) bool { return p.GetServerUpdateStatistics() != nil }); got.GetServerUpdateStatistics().Platform.PlayerCount != 3 {
		t.Errorf("statistics = %v", got)
	}

	socket.Close()
	if pong := v.Next(t, 5*time.Second, func(p *sparkproto.PacketWrapper) bool { return p.GetServerPong() != nil }).GetServerPong(); pong.Ok {
		t.Error("closing should send a failed pong")
	}
	if socket.IsOpen() {
		t.Error("closed socket is open")
	}
	if !strings.Contains(strings.Join(logs, "\n"), "Client connected: clientId=client1") {
		t.Errorf("logs: %q", logs)
	}
}
