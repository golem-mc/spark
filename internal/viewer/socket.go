package viewer

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/golem-mc/spark/internal/sparkproto"
	"google.golang.org/protobuf/proto"
)

const (
	// packetVersion is the RawPacket format spark speaks.
	packetVersion = 1
	// initialTimeout is how long a socket waits for its first viewer, and
	// establishedTimeout how long a viewer may go without a ping.
	// spark: ViewerSocket.SOCKET_INITIAL_TIMEOUT, SOCKET_ESTABLISHED_TIMEOUT
	initialTimeout     = 60 * time.Second
	establishedTimeout = 30 * time.Second
	// WindowSeconds and StatisticsSeconds tell the viewer how often the
	// profile and the statistics are sent.
	WindowSeconds     = 60
	StatisticsSeconds = 10
)

// Client opens channels on a bytesocks server. spark: BytesocksClient
type Client struct {
	httpURL, wsURL, userAgent string
	http                      *http.Client
}

// NewClient answers a client of the bytesocks server at host. host is a
// host name, reached over TLS, or a URL ("http://localhost:8080") for one
// without.
func NewClient(host, userAgent string) *Client {
	httpURL, wsURL := "https://"+host+"/", "wss://"+host+"/"
	if scheme, rest, ok := strings.Cut(host, "://"); ok {
		rest = strings.TrimSuffix(rest, "/") + "/"
		httpURL, wsURL = scheme+"://"+rest, "ws://"+rest
		if scheme == "https" {
			wsURL = "wss://" + rest
		}
	}
	return &Client{httpURL: httpURL, wsURL: wsURL, userAgent: userAgent, http: &http.Client{Timeout: 30 * time.Second}}
}

// create asks for a new channel and answers its id.
func (c *Client) create(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.httpURL+"create", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("bytesocks: request failed with status code %d", resp.StatusCode)
	}
	id := resp.Header.Get("Location")
	if id == "" {
		return "", errors.New("bytesocks: Location header not returned in response")
	}
	return id, nil
}

// Socket is one channel the viewer connects to. spark: ViewerSocket,
// ViewerSocketConnection
type Socket struct {
	keys      *KeyStore
	conn      *websocket.Conn
	channelID string
	publicKey []byte
	logf      func(msg string)
	opened    time.Time
	ctx       context.Context
	cancel    context.CancelFunc

	mu            sync.Mutex
	closed        bool
	lastPing      time.Time
	lastPayloadID string
}

// Open creates a channel and connects to it; logf receives the socket's
// log lines.
func Open(client *Client, keys *KeyStore, logf func(msg string)) (*Socket, error) {
	ctx, cancel := context.WithCancel(context.Background())
	dialCtx, dialCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dialCancel()
	pub, err := keys.PublicKey()
	if err != nil {
		cancel()
		return nil, err
	}
	id, err := client.create(dialCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	conn, _, err := websocket.Dial(dialCtx, client.wsURL+id, &websocket.DialOptions{
		HTTPClient: client.http,
		HTTPHeader: http.Header{"User-Agent": {client.userAgent}},
	})
	if err != nil {
		cancel()
		return nil, err
	}
	s := &Socket{keys: keys, conn: conn, channelID: id, publicKey: pub, logf: logf, opened: time.Now(), ctx: ctx, cancel: cancel}
	go s.read()
	return s, nil
}

func (s *Socket) log(msg string) { s.logf("[Viewer - " + s.channelID + "] " + msg) }

// ChannelInfo is what a payload carries for the viewer to find the socket.
func (s *Socket) ChannelInfo() *sparkproto.SocketChannelInfo {
	return &sparkproto.SocketChannelInfo{ChannelId: s.channelID, PublicKey: s.publicKey}
}

// IsOpen reports whether the socket is still connected.
func (s *Socket) IsOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.ctx.Err() == nil
}

// CheckShouldClose closes the socket when no viewer has pinged for a while,
// and reports whether nothing should be sent: it is closed, or no viewer
// has connected yet. spark: ViewerSocket.checkShouldClose
func (s *Socket) CheckShouldClose() bool {
	s.mu.Lock()
	closed, lastPing := s.closed, s.lastPing
	s.mu.Unlock()
	if closed {
		return true
	}
	if time.Since(s.opened) > initialTimeout && time.Since(lastPing) > establishedTimeout {
		s.log("No clients have pinged for 30s, closing socket")
		s.Close()
		return true
	}
	return lastPing.IsZero()
}

// Close tells the viewer the server has gone and closes the socket.
func (s *Socket) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.send(&sparkproto.PacketWrapper{Packet: &sparkproto.PacketWrapper_ServerPong{ServerPong: &sparkproto.ServerPong{Ok: false}}})
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.conn.Close(websocket.StatusGoingAway, "spark plugin disconnected")
	s.cancel()
}

// SendClientTrusted tells a waiting viewer it is now trusted.
func (s *Socket) SendClientTrusted(clientID string) {
	s.send(&sparkproto.PacketWrapper{Packet: &sparkproto.PacketWrapper_ServerConnectResponse{ServerConnectResponse: &sparkproto.ServerConnectResponse{
		ClientId: clientID, State: sparkproto.ServerConnectResponse_ACCEPTED,
	}}})
}

// SendStatistics sends the viewer fresh statistics.
func (s *Socket) SendStatistics(platform *sparkproto.PlatformStatistics, system *sparkproto.SystemStatistics, metrics *sparkproto.Metrics) {
	s.send(&sparkproto.PacketWrapper{Packet: &sparkproto.PacketWrapper_ServerUpdateStatistics{ServerUpdateStatistics: &sparkproto.ServerUpdateStatistics{
		Platform: platform, System: system, Metrics: metrics,
	}}})
}

// SendSamplerUpdate tells the viewer to load the profile uploaded as
// payloadID. spark: SamplerViewerSocket.sendUpdatedSamplerData
func (s *Socket) SendSamplerUpdate(payloadID string) {
	s.send(&sparkproto.PacketWrapper{Packet: &sparkproto.PacketWrapper_ServerUpdateSampler{ServerUpdateSampler: &sparkproto.ServerUpdateSamplerData{PayloadId: payloadID}}})
	s.mu.Lock()
	s.lastPayloadID = payloadID
	s.mu.Unlock()
}

// send signs and sends a packet. spark: ViewerSocketConnection.sendPacket
func (s *Socket) send(packet *sparkproto.PacketWrapper) {
	if err := s.trySend(packet); err != nil {
		s.logf("Exception occurred while sending data to the socket: " + err.Error())
	}
}

func (s *Socket) trySend(packet *sparkproto.PacketWrapper) error {
	msg, err := proto.Marshal(packet)
	if err != nil {
		return err
	}
	sig, err := s.keys.sign(msg)
	if err != nil {
		return err
	}
	raw, err := proto.Marshal(&sparkproto.RawPacket{Version: packetVersion, Signature: sig, Message: msg})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	return s.conn.Write(ctx, websocket.MessageText, []byte(base64.StdEncoding.EncodeToString(raw)))
}

// read handles the viewer's packets until the socket closes.
func (s *Socket) read() {
	defer s.cancel()
	for {
		typ, data, err := s.conn.Read(s.ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		if err := s.handle(string(data)); err != nil {
			s.logf("Exception occurred while reading data from the socket: " + err.Error())
		}
	}
}

// handle reads one packet: its signature counts only from a trusted key.
// spark: ViewerSocketConnection.handleRawPacket
func (s *Socket) handle(data string) error {
	buf, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return err
	}
	var raw sparkproto.RawPacket
	if err := proto.Unmarshal(buf, &raw); err != nil {
		return err
	}
	if raw.Version != packetVersion {
		return fmt.Errorf("unsupported packet version %d", raw.Version)
	}
	var key *rsa.PublicKey
	if len(raw.PublicKey) > 0 {
		if key, err = parsePublicKey(raw.PublicKey); err != nil {
			return fmt.Errorf("parsing public key: %w", err)
		}
	}
	verified := false
	if len(raw.Signature) > 0 && key != nil && s.keys.isTrusted(key) {
		sum := sha256.Sum256(raw.Message)
		verified = rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], raw.Signature) == nil
	}
	var packet sparkproto.PacketWrapper
	if err := proto.Unmarshal(raw.Message, &packet); err != nil {
		return err
	}
	switch p := packet.Packet.(type) {
	case *sparkproto.PacketWrapper_ClientPing:
		s.onPing(p.ClientPing)
	case *sparkproto.PacketWrapper_ClientConnect:
		return s.onConnect(p.ClientConnect, verified, key, raw.PublicKey)
	default:
		return fmt.Errorf("unexpected packet %T", packet.Packet)
	}
	return nil
}

func (s *Socket) onPing(p *sparkproto.ClientPing) {
	s.mu.Lock()
	s.lastPing = time.Now()
	ok := !s.closed
	s.mu.Unlock()
	s.send(&sparkproto.PacketWrapper{Packet: &sparkproto.PacketWrapper_ServerPong{ServerPong: &sparkproto.ServerPong{Ok: ok, Data: p.Data}}})
}

// onConnect answers a viewer: accepted when it signed with a trusted key,
// otherwise untrusted until /spark profiler trust-viewer.
// spark: ViewerSocket.onClientConnect
func (s *Socket) onConnect(p *sparkproto.ClientConnect, verified bool, key *rsa.PublicKey, der []byte) error {
	if key == nil {
		return errors.New("missing public key")
	}
	s.mu.Lock()
	s.lastPing = time.Now()
	closed, lastPayload := s.closed, s.lastPayloadID
	s.mu.Unlock()
	h := fnv.New32a()
	h.Write(der)
	s.log(fmt.Sprintf("Client connected: clientId=%s, keyhash=%x, desc=%s", p.ClientId, h.Sum32(), p.Description))
	resp := &sparkproto.ServerConnectResponse{
		ClientId:      p.ClientId,
		Settings:      &sparkproto.ServerConnectResponse_Settings{SamplerInterval: WindowSeconds, StatisticsInterval: StatisticsSeconds},
		LastPayloadId: lastPayload,
	}
	switch {
	case closed:
		resp.State = sparkproto.ServerConnectResponse_REJECTED
	case verified:
		resp.State = sparkproto.ServerConnectResponse_ACCEPTED
	default:
		resp.State = sparkproto.ServerConnectResponse_UNTRUSTED
		s.keys.addPending(p.ClientId, key)
	}
	s.send(&sparkproto.PacketWrapper{Packet: &sparkproto.PacketWrapper_ServerConnectResponse{ServerConnectResponse: resp}})
	return nil
}
