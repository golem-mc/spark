package platform

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/activitylog"
	"github.com/golem-mc/spark/internal/text"
)

const (
	pageWidth   = 45
	pageEntries = 4
)

// activityCommands is /spark activity. spark: ActivityLogModule
func activityCommands(*Platform) []*Command {
	return []*Command{{
		Aliases: []string{"activity", "activitylog", "log"},
		Usage:   []ArgInfo{{Name: "page", Param: "page no"}},
		Run: func(p *Platform, _ plugin.CommandSender, r *Responder, args *Arguments) error {
			entries := p.Activity.Entries()
			if len(entries) == 0 {
				r.ReplyPrefixed(text.T("There are no entries present in the log."))
				return nil
			}
			page, err := args.Int("page")
			if err != nil {
				return err
			}
			r.Reply(activityPage(entries, max(1, page), time.Now())...)
			return nil
		},
		Complete: func(_ *Platform, _ plugin.CommandSender, args []string) []string {
			return CompleteFlags(args, "--page")
		},
	}}
}

// activityPage renders one page of the log the way adventure's pagination
// lays it out: a titled rule, the rows and a rule with page buttons.
func activityPage(entries []activitylog.Activity, page int, now time.Time) []plugin.Text {
	pages := (len(entries) + pageEntries - 1) / pageEntries
	if page > pages {
		return []plugin.Text{text.Prefix(text.T("Unknown page selected. " + strconv.Itoa(pages) + " total pages."))}
	}
	out := []plugin.Text{rule(text.T("Recent spark activity", text.Gold))}
	for i := (page - 1) * pageEntries; i < min(len(entries), page*pageEntries); i++ {
		out = append(out, activityRow(entries[i], i, now)...)
	}
	cmd := func(n int) string { return "/" + commandName + " activity --page " + strconv.Itoa(n) }
	if pages == 1 {
		return append(out, text.T(strings.Repeat("-", pageWidth), text.DarkGray))
	}
	prev := text.T("«", text.DarkGray)
	if page > 1 {
		prev = text.RunCommand(text.T("«", text.Red).OnHover(text.T("Previous Page", text.Red)), cmd(page-1))
	}
	next := text.T("»", text.DarkGray)
	if page < pages {
		next = text.RunCommand(text.T("»", text.Green).OnHover(text.T("Next Page", text.Green)), cmd(page+1))
	}
	return append(out, rule(text.Of(prev, text.T(fmt.Sprintf(" Page %d/%d ", page, pages), text.Gray), next)))
}

// rule centres label in a line of dashes pageWidth wide.
func rule(label plugin.Text) plugin.Text {
	n := max(0, pageWidth-utf8.RuneCountInString(label.PlainString())-2)
	return text.Of(text.T(strings.Repeat("-", n/2)+" ", text.DarkGray), label, text.T(" "+strings.Repeat("-", n-n/2), text.DarkGray))
}

// activityRow is one entry. spark: ActivityLogModule.renderRow
func activityRow(a activitylog.Activity, index int, now time.Time) []plugin.Text {
	value := text.T(a.Data.Value, text.White)
	if a.Data.Type == activitylog.DataURL {
		value = text.OpenURL(value, a.Data.Value)
	}
	kind := a.Data.Type
	if kind != "" {
		kind = strings.ToUpper(kind[:1]) + kind[1:]
	}
	return []plugin.Text{
		text.Of(text.T(">", text.DarkGray).Bold(), text.Space(), text.T("#"+strconv.Itoa(index+1), text.White),
			text.T(" - ", text.DarkGray), text.T(a.Type, text.Yellow), text.T(" - ", text.DarkGray),
			text.T(dateDiff(time.UnixMilli(a.Time), now), text.Gray)),
		text.Of(text.T("  "), text.T("Created by: ", text.Gray), text.T(a.User.Name, text.White)),
		text.Of(text.T("  "), text.T(kind+": ", text.Gray), value),
		text.Space(),
	}
}

// dateDiff is how long ago t was, as "1d 2h 3m 4s ago". spark: ActivityLogModule.formatDateDiff
func dateDiff(t, now time.Time) string {
	seconds := int64(now.Sub(t) / time.Second)
	if seconds <= 0 {
		return "now"
	}
	parts := []struct {
		n    int64
		unit string
	}{{seconds / 86400, "d"}, {seconds / 3600 % 24, "h"}, {seconds / 60 % 60, "m"}, {seconds % 60, "s"}}
	var sb strings.Builder
	for _, p := range parts {
		if p.n != 0 {
			fmt.Fprintf(&sb, "%d%s ", p.n, p.unit)
		}
	}
	return strings.TrimSpace(sb.String()) + " ago"
}

// logActivity records an upload (a URL) or a saved file made for r's sender.
func (p *Platform) logActivity(r *Responder, kind, dataType, value string) {
	s := r.Sender()
	user := activitylog.User{Type: "other", Name: s.Name}
	if s.IsPlayer() {
		user = activitylog.User{Type: "player", Name: s.Name, UniqueID: s.UUID}
	}
	if err := p.Activity.Add(activitylog.Activity{User: user, Time: time.Now().UnixMilli(), Type: kind,
		Data: activitylog.Data{Type: dataType, Value: value}}); err != nil {
		p.Server.Logger().Warn("Could not save the activity log", "error", err)
	}
}
