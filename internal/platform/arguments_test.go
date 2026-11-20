package platform

import (
	"slices"
	"testing"
)

func TestParseArguments(t *testing.T) {
	a, err := ParseArguments([]string{"start", "--timeout", "30", "--thread", "Server", "thread", "--thread", "*", "--combine-all"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if a.SubCommand() != "start" {
		t.Errorf("sub = %q", a.SubCommand())
	}
	if n, _ := a.Int("timeout"); n != 30 {
		t.Errorf("timeout = %d", n)
	}
	if got := a.Strings("thread"); !slices.Equal(got, []string{"Server thread", "*"}) {
		t.Errorf("thread = %q", got)
	}
	if !a.Bool("combine-all") || a.Bool("alloc") {
		t.Error("bool flags wrong")
	}
	if n, _ := a.Int("interval"); n != -1 {
		t.Errorf("absent int = %d, want -1", n)
	}
	if f, _ := a.Float("missing"); f != -1 {
		t.Errorf("absent float = %v", f)
	}
}

func TestParseArgumentsErrors(t *testing.T) {
	if _, err := ParseArguments([]string{"start"}, false); err == nil || err.Error() != "Expected flag at position 0 but got 'start' instead!" {
		t.Errorf("err = %v", err)
	}
	a, err := ParseArguments([]string{"--timeout", "soon"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Int("timeout"); err == nil || err.Error() != "Invalid input for 'timeout' argument. Please specify a number!" {
		t.Errorf("err = %v", err)
	}
	a, _ = ParseArguments([]string{"--interval", "-2.5", "--Page", "-3"}, false)
	if f, _ := a.Float("interval"); f != 2.5 {
		t.Errorf("interval = %v, want the absolute value", f)
	}
	if n, _ := a.Int("page"); n != 3 {
		t.Errorf("page = %d, want flags lower-cased and the absolute value", n)
	}
}

func TestCompletion(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{""}, []string{"--player"}},
		{[]string{"--pl"}, []string{"--player"}},
		{[]string{"--player", "x", ""}, nil},
	}
	for _, c := range cases {
		if got := CompleteFlags(c.args, "--player"); !slices.Equal(got, c.want) {
			t.Errorf("CompleteFlags(%q) = %q, want %q", c.args, got, c.want)
		}
	}
	at := map[int][]string{0: {"dashboard", "upload", "show"}}
	from := map[int][]string{1: {"--memory", "--network"}}
	if got := CompleteAt([]string{"sh"}, at, from); !slices.Equal(got, []string{"show"}) {
		t.Errorf("sub = %q", got)
	}
	if got := CompleteAt([]string{"show", "--memory", "--n"}, at, from); !slices.Equal(got, []string{"--network"}) {
		t.Errorf("flag = %q", got)
	}
	if got := withPrefix("profiler start --ti", []string{"--timeout"}); !slices.Equal(got, []string{"profiler start --timeout"}) {
		t.Errorf("withPrefix = %q", got)
	}
}
