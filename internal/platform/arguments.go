package platform

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var flagPattern = regexp.MustCompile(`^--(.+)$`)

// ParseError is a malformed flag, shown to the sender in red.
type ParseError string

func (e ParseError) Error() string { return string(e) }

// Arguments are a command's words parsed as an optional sub-command followed
// by --flags, each flag's value being the words up to the next flag.
// spark: Arguments
type Arguments struct {
	raw        []string
	subCommand string
	flags      map[string][]string // flag -> values, in order, duplicates dropped
}

// ParseArguments parses raw; allowSub lets a first word that is no flag be
// the sub-command.
func ParseArguments(raw []string, allowSub bool) (*Arguments, error) {
	a := &Arguments{raw: raw, flags: map[string][]string{}}
	flag := ""
	var value []string
	put := func() {
		v := strings.Join(value, " ")
		if slices.Contains(a.flags[flag], v) {
			return
		}
		a.flags[flag] = append(a.flags[flag], v)
	}
	for i, arg := range raw {
		m := flagPattern.FindStringSubmatch(arg)
		switch {
		case i == 0 && allowSub && m == nil:
			a.subCommand = arg
		case flag == "" || m != nil:
			if m == nil {
				return nil, ParseError(fmt.Sprintf("Expected flag at position %d but got '%s' instead!", i, arg))
			}
			if flag != "" {
				put()
			}
			flag = strings.ToLower(m[1])
			value = nil
		default:
			value = append(value, arg)
		}
	}
	if flag != "" {
		put()
	}
	return a, nil
}

// Raw answers the words as given.
func (a *Arguments) Raw() []string { return a.raw }

// SubCommand answers the sub-command, empty for none.
func (a *Arguments) SubCommand() string { return a.subCommand }

// Int answers a flag's first value as a non-negative integer, -1 when the
// flag is absent. spark: Arguments.intFlag
func (a *Arguments) Int(key string) (int, error) {
	values := a.flags[key]
	if len(values) == 0 {
		return -1, nil
	}
	n, err := strconv.ParseInt(values[0], 10, 32)
	if err != nil {
		return 0, ParseError("Invalid input for '" + key + "' argument. Please specify a number!")
	}
	if n < 0 {
		n = -n
	}
	return int(n), nil
}

// Float answers a flag's first value as a non-negative number, -1 when the
// flag is absent. spark: Arguments.doubleFlag
func (a *Arguments) Float(key string) (float64, error) {
	values := a.flags[key]
	if len(values) == 0 {
		return -1, nil
	}
	f, err := strconv.ParseFloat(values[0], 64)
	if err != nil || math.IsNaN(f) {
		return 0, ParseError("Invalid input for '" + key + "' argument. Please specify a number!")
	}
	return math.Abs(f), nil
}

// Strings answers every value a flag was given.
func (a *Arguments) Strings(key string) []string { return a.flags[key] }

// Bool reports whether a flag was given.
func (a *Arguments) Bool(key string) bool {
	_, ok := a.flags[key]
	return ok
}
