// Package viewertest fakes bytesocks and spark's web viewer, for tests of
// the live viewer.
package viewertest

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golem-mc/spark/internal/sparkproto"
	"google.golang.org/protobuf/proto"
)

// Bytesocks is a bytesocks server: GET /create makes a channel, and every
// text message on a channel's socket goes to its other sockets.
type Bytesocks struct {
	URL string

	mu       sync.Mutex
	next     int
	channels map[string][]*websocket.Conn
}

// NewBytesocks starts a fake bytesocks server for the test.
func NewBytesocks(t *testing.T) *Bytesocks {
	t.Helper()
	b := &Bytesocks{channels: map[string][]*websocket.Conn{}}
	srv := httptest.NewServer(http.HandlerFunc(b.serve))
	t.Cleanup(srv.Close)
	b.URL = srv.URL
	return b
}

func (b *Bytesocks) serve(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/")
	if id == "create" {
		b.mu.Lock()
		b.next++
		id = "channel" + strconv.Itoa(b.next)
		b.channels[id] = nil
		b.mu.Unlock()
		w.Header().Set("Location", id)
		w.WriteHeader(http.StatusCreated)
		return
	}
	b.mu.Lock()
	_, ok := b.channels[id]
	b.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	b.mu.Lock()
	b.channels[id] = append(b.channels[id], conn)
	b.mu.Unlock()
	for {
		typ, data, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		b.mu.Lock()
		peers := append([]*websocket.Conn(nil), b.channels[id]...)
		b.mu.Unlock()
		for _, peer := range peers {
			if peer != conn {
				_ = peer.Write(context.Background(), typ, data)
			}
		}
	}
}

// Viewer is the web viewer on one channel, with its own key.
type Viewer struct {
	conn      *websocket.Conn
	key       *rsa.PrivateKey
	serverKey *rsa.PublicKey
	packets   chan *sparkproto.PacketWrapper
}

// Connect joins the channel in info as a viewer.
func (b *Bytesocks) Connect(t *testing.T, info *sparkproto.SocketChannelInfo) *Viewer {
	t.Helper()
	pub, err := x509.ParsePKIXPublicKey(info.PublicKey)
	if err != nil {
		t.Fatalf("server key: %v", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(b.URL, "http")+"/"+info.ChannelId, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	v := &Viewer{conn: conn, key: key, serverKey: pub.(*rsa.PublicKey), packets: make(chan *sparkproto.PacketWrapper, 64)}
	go v.read()
	return v
}

func (v *Viewer) read() {
	defer close(v.packets)
	for {
		_, data, err := v.conn.Read(context.Background())
		if err != nil {
			return
		}
		if p, err := v.decode(string(data)); err == nil {
			v.packets <- p
		}
	}
}

// decode checks the server's signature and answers its packet.
func (v *Viewer) decode(data string) (*sparkproto.PacketWrapper, error) {
	buf, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, err
	}
	var raw sparkproto.RawPacket
	if err := proto.Unmarshal(buf, &raw); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw.Message)
	if err := rsa.VerifyPKCS1v15(v.serverKey, crypto.SHA256, sum[:], raw.Signature); err != nil {
		return nil, errors.New("bad server signature")
	}
	var p sparkproto.PacketWrapper
	return &p, proto.Unmarshal(raw.Message, &p)
}

// Send signs and sends a packet, with the viewer's public key.
func (v *Viewer) Send(t *testing.T, p *sparkproto.PacketWrapper) {
	t.Helper()
	msg, _ := proto.Marshal(p)
	sum := sha256.Sum256(msg)
	sig, err := rsa.SignPKCS1v15(rand.Reader, v.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&v.key.PublicKey)
	raw, _ := proto.Marshal(&sparkproto.RawPacket{Version: 1, PublicKey: der, Signature: sig, Message: msg})
	if err := v.conn.Write(context.Background(), websocket.MessageText, []byte(base64.StdEncoding.EncodeToString(raw))); err != nil {
		t.Fatal(err)
	}
}

// Connect sends ClientConnect as clientID.
func (v *Viewer) Connect(t *testing.T, clientID string) {
	t.Helper()
	v.Send(t, &sparkproto.PacketWrapper{Packet: &sparkproto.PacketWrapper_ClientConnect{ClientConnect: &sparkproto.ClientConnect{ClientId: clientID, Description: "test viewer"}}})
}

// Ping sends ClientPing with data.
func (v *Viewer) Ping(t *testing.T, data int32) {
	t.Helper()
	v.Send(t, &sparkproto.PacketWrapper{Packet: &sparkproto.PacketWrapper_ClientPing{ClientPing: &sparkproto.ClientPing{Ok: true, Data: data}}})
}

// Next waits up to timeout for the server's next packet that match accepts.
func (v *Viewer) Next(t *testing.T, timeout time.Duration, match func(*sparkproto.PacketWrapper) bool) *sparkproto.PacketWrapper {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case p, ok := <-v.packets:
			if !ok {
				t.Fatal("the socket closed")
			}
			if match(p) {
				return p
			}
		case <-deadline:
			t.Fatal("no matching packet from the server")
		}
	}
}
