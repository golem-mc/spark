package platform

import (
	"errors"
	"slices"
	"strings"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/text"
)

// commandName is the command spark registers on a server. spark: getCommandName
const commandName = "spark"

// Command is one /spark sub-command. spark: Command
type Command struct {
	// Aliases are the names it runs under; the first is the primary one,
	// which names its permission node spark.<alias>.
	Aliases []string
	// Usage lists the flags shown by /spark help.
	Usage []ArgInfo
	// AllowSub lets the first word be a sub-command rather than a flag.
	AllowSub bool
	Run      func(p *Platform, sender plugin.CommandSender, r *Responder, args *Arguments) error
	// Complete answers candidates for the last of args, which is "" when
	// the sender has typed a space; nil completes nothing.
	Complete func(p *Platform, sender plugin.CommandSender, args []string) []string
}

func (c *Command) primary() string { return c.Aliases[0] }

// ArgInfo is one flag of a command's usage, under a sub-command when Sub is
// set. Param names the flag's value; empty for a flag with none.
// spark: Command.ArgumentInfo
type ArgInfo struct {
	Sub, Name, Param string
}

func (a ArgInfo) component(padding string) plugin.Text {
	parts := []plugin.Text{text.T(padding), text.T("[", text.DarkGray), text.T("--"+a.Name, text.Gray)}
	if a.Param != "" {
		parts = append(parts, text.Space(), text.T("<"+a.Param+">", text.DarkGray))
	}
	return text.Of(append(parts, text.T("]", text.DarkGray))...)
}

// available answers the commands sender may use: all with spark, else those
// whose spark.<alias> it has. spark: CommandManager.getAvailableCommands
func (p *Platform) available(sender plugin.CommandSender) []*Command {
	if sender.HasPermission("spark") {
		return p.commands
	}
	var out []*Command
	for _, c := range p.commands {
		if sender.HasPermission("spark." + c.primary()) {
			out = append(out, c)
		}
	}
	return out
}

// Permissions answers every node spark checks: spark and spark.<command>.
func (p *Platform) Permissions() []string {
	out := []string{"spark"}
	for _, c := range p.commands {
		out = append(out, "spark."+c.primary())
	}
	return out
}

// Execute runs /spark with args as sender. spark: CommandManager.executeCommand0
func (p *Platform) Execute(sender plugin.CommandSender, args []string) {
	r := &Responder{p: p, sender: sender, data: senderOf(sender)}
	commands := p.available(sender)
	if len(commands) == 0 {
		r.ReplyPrefixed(text.T("You do not have permission to use this command.", text.Red))
		return
	}
	if len(args) == 0 {
		r.ReplyPrefixed(p.versionLine())
		help := "/" + commandName + " help"
		r.ReplyPrefixed(text.Colored(text.Gray,
			text.T("Run "),
			text.RunCommand(text.T(help, text.White), help),
			text.T(" to view usage information.")))
		return
	}
	alias := strings.ToLower(args[0])
	for _, c := range commands {
		if !slices.Contains(c.Aliases, alias) {
			continue
		}
		r.alias = c.primary()
		parsed, err := ParseArguments(args[1:], c.AllowSub)
		if err == nil {
			err = c.Run(p, sender, r, parsed)
		}
		if perr, ok := errors.AsType[ParseError](err); ok {
			r.ReplyPrefixed(text.T(perr.Error(), text.Red))
		} else if err != nil {
			p.Server.Logger().Error("Exception occurred whilst executing a spark command", "error", err)
		}
		return
	}
	p.sendUsage(commands, r)
}

func (p *Platform) versionLine() plugin.Text {
	return text.Of(text.T("spark", text.White), text.Space(), text.T("v"+p.Version, text.Gray))
}

// sendUsage lists the commands and their flags. spark: CommandManager.sendUsage
func (p *Platform) sendUsage(commands []*Command, r *Responder) {
	r.ReplyPrefixed(p.versionLine())
	line := func(usage string) plugin.Text {
		return text.Of(text.T(">", text.Gold).Bold(), text.Space(),
			text.SuggestCommand(text.T(usage, text.Gray), usage))
	}
	for _, c := range commands {
		usage := "/" + commandName + " " + c.primary()
		if !c.AllowSub {
			r.Reply(line(usage))
			for _, a := range c.Usage {
				r.Reply(a.component("    "))
			}
			continue
		}
		// Grouped by sub-command, in the order each first appears.
		var subs []string
		for _, a := range c.Usage {
			if !slices.Contains(subs, a.Sub) {
				subs = append(subs, a.Sub)
			}
		}
		for _, sub := range subs {
			r.Reply(line(usage + " " + sub))
			for _, a := range c.Usage {
				if a.Sub == sub && a.Name != "" {
					r.Reply(a.component("      "))
				}
			}
		}
	}
	r.Reply(text.Empty())
	const docs = "https://spark.lucko.me/docs/Command-Usage"
	r.ReplyPrefixed(text.Of(text.T("For full usage information, please go to: "),
		text.OpenURL(text.T(docs, text.White), docs)))
}

// Complete answers tab completions for /spark args. spark: CommandManager.tabCompleteCommand
func (p *Platform) Complete(sender plugin.CommandSender, args []string) []string {
	commands := p.available(sender)
	if len(commands) == 0 {
		return nil
	}
	if len(args) <= 1 {
		names := make([]string, len(commands))
		for i, c := range commands {
			names[i] = c.primary()
		}
		return CompleteAt(args, map[int][]string{0: names}, nil)
	}
	for _, c := range commands {
		if slices.Contains(c.Aliases, args[0]) {
			if c.Complete == nil {
				return nil
			}
			return c.Complete(p, sender, args[1:])
		}
	}
	return nil
}

// StartsWith answers the candidates that begin with partial, ignoring case.
// spark: CompletionSupplier.startsWith
func StartsWith(candidates []string, partial string) []string {
	var out []string
	for _, c := range candidates {
		if len(c) >= len(partial) && strings.EqualFold(c[:len(partial)], partial) {
			out = append(out, c)
		}
	}
	return out
}

// CompleteAt completes the last of args from the candidates for its
// position, or from from for any position at or after fromIndex (the lowest
// key of from). spark: TabCompleter
func CompleteAt(args []string, at map[int][]string, from map[int][]string) []string {
	last, partial := 0, ""
	if len(args) > 0 {
		last = len(args) - 1
		if strings.TrimSpace(args[last]) != "" {
			partial = args[last]
		}
	}
	for start, candidates := range from {
		if last >= start {
			return StartsWith(candidates, partial)
		}
	}
	return StartsWith(at[last], partial)
}

// CompleteFlags offers each of flags not already among args, at every
// position. spark: TabCompleter.completeForOpts
func CompleteFlags(args []string, flags ...string) []string {
	opts := slices.DeleteFunc(slices.Clone(flags), func(f string) bool { return slices.Contains(args, f) })
	return CompleteAt(args, nil, map[int][]string{0: opts})
}
