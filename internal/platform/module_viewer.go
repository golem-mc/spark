package platform

import (
	"slices"
	"sync"
	"time"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/activitylog"
	"github.com/golem-mc/spark/internal/bytebin"
	"github.com/golem-mc/spark/internal/monitor"
	"github.com/golem-mc/spark/internal/sparkproto"
	"github.com/golem-mc/spark/internal/text"
	"github.com/golem-mc/spark/internal/viewer"
)

// samplerSocket is a live viewer of a profiler, with the choices its
// profiles are exported with. spark: SamplerViewerSocket
type samplerSocket struct {
	*viewer.Socket
	props exportProps
}

// viewers are the live viewers attached to a sampler.
type viewers struct {
	mu      sync.Mutex
	sockets []samplerSocket
	stats   plugin.Task // sends statistics every 10s while there are sockets
}

func (p *Platform) openSocket() (*viewer.Socket, error) {
	return viewer.Open(p.Bytesocks, p.Keys, func(msg string) { p.Server.Logger().Info(msg) })
}

// open returns the sockets still open, dropping the rest.
func (v *viewers) open() []samplerSocket {
	v.mu.Lock()
	defer v.mu.Unlock()
	kept := v.sockets[:0]
	for _, s := range v.sockets {
		if s.IsOpen() {
			kept = append(kept, s)
		}
	}
	v.sockets = kept
	return append([]samplerSocket(nil), kept...)
}

// attachSocket adds a live viewer to s. spark: AbstractSampler.attachSocket
func (p *Platform) attachSocket(s *activeSampler, socket samplerSocket) {
	s.viewers.mu.Lock()
	defer s.viewers.mu.Unlock()
	s.viewers.sockets = append(s.viewers.sockets, socket)
	if s.viewers.stats == nil {
		period := viewer.StatisticsSeconds * time.Second
		s.viewers.stats = p.Server.Scheduler().RunAsyncTimer(period, period, func() { p.sendSamplerStatistics(s) })
	}
}

// sendSamplerStatistics sends a sampler's viewers fresh statistics.
// spark: AbstractSampler.sendStatisticsToSocket
func (p *Platform) sendSamplerStatistics(s *activeSampler) {
	sockets := s.viewers.open()
	if len(sockets) == 0 {
		return
	}
	p.withStatistics(s.initialGC, func(platform *sparkproto.PlatformStatistics, system *sparkproto.SystemStatistics, metrics *sparkproto.Metrics) {
		for _, socket := range sockets {
			socket.SendStatistics(platform, system, metrics)
		}
	})
}

// withStatistics gathers the statistics a viewer is sent, reading players
// and worlds on the tick goroutine, and passes them to send on another.
func (p *Platform) withStatistics(initialGC map[string]monitor.GCStats, send func(*sparkproto.PlatformStatistics, *sparkproto.SystemStatistics, *sparkproto.Metrics)) {
	p.Server.Scheduler().RunTask(func() {
		snap := p.snapshot(false)
		p.Server.Scheduler().RunAsync(func() {
			send(p.platformStatistics(initialGC, snap), p.systemStatistics(), p.Metrics.Proto())
		})
	})
}

// onWindowRotate sends each viewer the profile so far.
// spark: SamplerViewerSocket.processWindowRotate
func (p *Platform) onWindowRotate(s *activeSampler) {
	for _, socket := range s.viewers.open() {
		if socket.CheckShouldClose() {
			continue
		}
		p.Server.Scheduler().RunTask(func() {
			snap := p.snapshot(true)
			p.Server.Scheduler().RunAsync(func() {
				key, err := p.uploadKey(p.samplerData(s, socket.props, snap), bytebin.SamplerMediaType, "live")
				if err != nil {
					p.Server.Logger().Warn("Error whilst sending updated sampler data to the socket", "error", err)
					return
				}
				socket.SendSamplerUpdate(key)
			})
		})
	}
}

// onSamplerStopped closes a stopped sampler's viewers.
// spark: SamplerViewerSocket.processSamplerStopped
func (s *activeSampler) onSamplerStopped() {
	s.viewers.mu.Lock()
	sockets := s.viewers.sockets
	s.viewers.sockets = nil
	if s.viewers.stats != nil {
		s.viewers.stats.Cancel()
	}
	s.viewers.mu.Unlock()
	for _, socket := range sockets {
		socket.Close()
	}
}

// profilerOpen opens the live viewer of the running profiler.
// spark: SamplerModule.profilerOpen, handleOpen
func (p *Platform) profilerOpen(r *Responder, args *Arguments) {
	s := p.activeSampler()
	if s == nil {
		r.ReplyPrefixed(text.T("The profiler isn't running!"))
		r.ReplyPrefixed(text.T("To start a new one, run:"))
		r.ReplyPrefixed(cmdPrompt("/" + commandName + " profiler start"))
		return
	}
	props := p.exportProps(r, args)
	snap := p.snapshot(true)
	async := r.Async()
	p.Server.Scheduler().RunAsync(func() {
		url, err := p.openSampler(s, props, snap)
		if err != nil {
			async.ReplyPrefixed(text.T("An error occurred whilst opening the live profiler.", text.Red))
			p.Server.Logger().Warn("Error whilst opening live profiler", "error", err)
			return
		}
		async.BroadcastPrefixed(text.T("Profiler live viewer:", text.Gold))
		async.Broadcast(text.OpenURL(text.T(url, text.Gray), url))
		cmd := "/" + commandName + " profiler stop"
		async.Broadcast(text.Empty())
		async.Broadcast(text.Of(
			text.T("(NOTE: this link is temporary and will expire after a short period of time. "+
				"If you need a link to share with other people (e.g. in a bug report), please use ", text.Gray),
			text.RunCommand(text.T(cmd, text.White), cmd),
			text.T(" instead.)", text.Gray)))
		p.logActivity(async, "Profiler (live)", activitylog.DataURL, url)
	})
}

// openSampler attaches a new socket to s and uploads the profile so far
// with the socket's channel, answering the viewer's URL.
func (p *Platform) openSampler(s *activeSampler, props exportProps, snap tickSnapshot) (string, error) {
	socket, err := p.openSocket()
	if err != nil {
		return "", err
	}
	if err := s.Flush(); err != nil {
		socket.Close()
		return "", err
	}
	props.channelInfo = socket.ChannelInfo()
	p.attachSocket(s, samplerSocket{Socket: socket, props: props})
	return p.upload(p.samplerData(s, props, snap), bytebin.SamplerMediaType, "live")
}

// profilerTrustViewer trusts the viewers waiting with the given ids.
// spark: SamplerModule.profilerTrustViewer
func (p *Platform) profilerTrustViewer(r *Responder, args *Arguments) {
	ids := args.Strings("id")
	if len(ids) == 0 {
		r.ReplyPrefixed(text.T("Please provide a client id with '--id <client id>'."))
		return
	}
	for _, id := range ids {
		if !p.Keys.TrustPending(id) {
			r.ReplyPrefixed(text.T("Unable to find pending client with id '" + id + "'."))
			continue
		}
		if s := p.activeSampler(); s != nil {
			for _, socket := range s.viewers.open() {
				socket.SendClientTrusted(id)
			}
		}
		r.ReplyPrefixed(text.T("Client connected to the viewer using id '" + id + "' is now trusted."))
	}
}

// openHealthSocket opens the health dashboard's socket, which sends the
// viewer statistics every 10 seconds until no viewer pings it.
// spark: HealthReportViewerSocket
func (p *Platform) openHealthSocket() (*viewer.Socket, error) {
	socket, err := p.openSocket()
	if err != nil {
		return nil, err
	}
	var task plugin.Task
	period := viewer.StatisticsSeconds * time.Second
	task = p.Server.Scheduler().RunAsyncTimer(period, period, func() {
		if !socket.IsOpen() {
			task.Cancel()
			return
		}
		if socket.CheckShouldClose() {
			return
		}
		p.withStatistics(p.startupGC, socket.SendStatistics)
	})
	p.socketsMu.Lock()
	p.healthSockets = slices.DeleteFunc(p.healthSockets, func(s *viewer.Socket) bool { return !s.IsOpen() })
	p.healthSockets = append(p.healthSockets, socket)
	p.socketsMu.Unlock()
	return socket, nil
}
