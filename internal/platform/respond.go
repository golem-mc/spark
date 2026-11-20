package platform

import (
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/text"
)

// Sender is who ran a command, as recorded in uploads and the activity log.
// spark: CommandSender.Data
type Sender struct {
	Name string
	// UUID is empty for the console and other non-players.
	UUID string
}

// IsPlayer reports whether the sender was a player.
func (s Sender) IsPlayer() bool { return s.UUID != "" }

func senderOf(cs plugin.CommandSender) Sender {
	if p, ok := cs.(plugin.Player); ok {
		return Sender{Name: p.Name(), UUID: p.UUID().String()}
	}
	return Sender{Name: cs.Name()}
}

// Responder answers the sender of one command, and broadcasts to everyone
// allowed to use it. A Responder made by the command manager must be used on
// the tick goroutine; Async gives one for other goroutines, which delivers on
// the next tick. spark: CommandResponseHandler
type Responder struct {
	p      *Platform
	sender plugin.CommandSender
	data   Sender
	// alias is the command's primary alias, whose permission decides who
	// receives a broadcast.
	alias string
	async bool
}

// Sender answers who ran the command.
func (r *Responder) Sender() Sender { return r.data }

// Async answers a Responder that is safe from any goroutine.
func (r *Responder) Async() *Responder {
	c := *r
	c.async = true
	return &c
}

func (r *Responder) deliver(fn func()) {
	if r.async {
		r.p.Server.Scheduler().RunTask(fn)
		return
	}
	fn()
}

// target is who replies go to. The console's sender in a command is the
// dispatch's own, which collects output only while the command runs, so a
// later reply goes to the server console instead.
func (r *Responder) target() plugin.CommandSender {
	if _, player := r.sender.(plugin.Player); r.async && !player {
		if console := r.p.Server.Console(); r.sender.Name() == console.Name() {
			return console
		}
	}
	return r.sender
}

// Reply sends msgs to the sender, joined by newlines into one message.
func (r *Responder) Reply(msgs ...plugin.Text) {
	msg := text.Lines(msgs)
	r.deliver(func() { r.target().SendMessage(msg) })
}

// ReplyPrefixed sends msg to the sender after spark's prefix.
func (r *Responder) ReplyPrefixed(msg plugin.Text) { r.Reply(text.Prefix(msg)) }

// Broadcast sends msgs to every player allowed to use the command, the
// console and the sender, or only to the sender when disableResponseBroadcast
// is set.
func (r *Responder) Broadcast(msgs ...plugin.Text) {
	if r.p.disableBroadcast {
		r.Reply(msgs...)
		return
	}
	msg := text.Lines(msgs)
	r.deliver(func() {
		for _, s := range r.p.recipients(r.alias, r.target()) {
			s.SendMessage(msg)
		}
	})
}

// BroadcastPrefixed broadcasts msg after spark's prefix.
func (r *Responder) BroadcastPrefixed(msg plugin.Text) { r.Broadcast(text.Prefix(msg)) }

// recipients are the online players with spark or spark.<alias>, the
// console, and sender, each once. spark: CommandResponseHandler.allSenders
func (p *Platform) recipients(alias string, sender plugin.CommandSender) []plugin.CommandSender {
	var out []plugin.CommandSender
	senderPlayer, senderIsPlayer := sender.(plugin.Player)
	for _, pl := range p.Server.Players() {
		if senderIsPlayer && pl.UUID() == senderPlayer.UUID() {
			continue
		}
		if pl.HasPermission("spark") || pl.HasPermission("spark."+alias) {
			out = append(out, pl)
		}
	}
	console := p.Server.Console()
	// Names, not ==: a sender's dynamic type need not be comparable.
	if senderIsPlayer || sender.Name() != console.Name() {
		out = append(out, console)
	}
	return append(out, sender)
}
