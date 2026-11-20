// Package text builds chat components the way spark's adventure calls do:
// a literal with an optional colour, joined children inheriting the parent's
// style.
package text

import "github.com/golem-mc/golem/plugin"

// The named colours spark uses (adventure's NamedTextColor).
const (
	Black       = "black"
	DarkGray    = "dark_gray"
	Gray        = "gray"
	White       = "white"
	Red         = "red"
	DarkRed     = "dark_red"
	Gold        = "gold"
	Yellow      = "yellow"
	Green       = "green"
	DarkGreen   = "dark_green"
	Aqua        = "aqua"
	DarkAqua    = "dark_aqua"
	Blue        = "blue"
	LightPurple = "light_purple"
)

// T is a literal, coloured when color is given. adventure: text(content, color)
func T(content string, color ...string) plugin.Text {
	t := plugin.Plain(content)
	if len(color) > 0 {
		t = t.Color(color[0])
	}
	return t
}

// Of joins parts under an empty parent, so they render one after another.
// adventure: text().append(...).build()
func Of(parts ...plugin.Text) plugin.Text { return plugin.Text{Children: parts} }

// Colored joins parts under a parent of the given colour, which the parts
// inherit unless they set their own. adventure: text().color(c).append(...)
func Colored(color string, parts ...plugin.Text) plugin.Text {
	return plugin.Text{Children: parts}.Color(color)
}

// Empty is an empty line. adventure: Component.empty()
func Empty() plugin.Text { return plugin.Plain("") }

// Space is a single space. adventure: Component.space()
func Space() plugin.Text { return plugin.Plain(" ") }

// Lines joins messages with newlines into one message, as spark's
// reply(Iterable) does.
func Lines(msgs []plugin.Text) plugin.Text {
	out := plugin.Text{}
	for i, m := range msgs {
		if i > 0 {
			out.Children = append(out.Children, plugin.Plain("\n"))
		}
		out.Children = append(out.Children, m)
	}
	return out
}

// RunCommand makes t run cmd when clicked.
func RunCommand(t plugin.Text, cmd string) plugin.Text { return t.OnClick(plugin.RunCommand, cmd) }

// SuggestCommand makes t put cmd in the chat box when clicked.
func SuggestCommand(t plugin.Text, cmd string) plugin.Text {
	return t.OnClick(plugin.SuggestCommand, cmd)
}

// OpenURL makes t open url when clicked.
func OpenURL(t plugin.Text, url string) plugin.Text { return t.OnClick(plugin.OpenURL, url) }

// Prefix is spark's message prefix "&8[&e&l⚡&8] &7"; the message after it is
// grey unless it sets its own colour. spark: CommandResponseHandler.PREFIX
func Prefix(msg plugin.Text) plugin.Text {
	return Colored(Gray,
		T("[", DarkGray),
		T("⚡", Yellow).Bold(),
		T("]", DarkGray),
		T(" "),
		msg,
	)
}
