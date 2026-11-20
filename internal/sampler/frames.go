// Package sampler is spark's profiler on Go's runtime: it samples with the
// runtime's CPU profiler, recognises the tick goroutine as the "Server
// thread", and aggregates stacks into spark's call tree per one-minute window.
// spark: sampler/*
package sampler

import (
	"regexp"
	"strings"
)

// closureSegment is the name the compiler gives a closure or a go/defer
// wrapper inside a function: F.func1, F.func1.2, F.gowrap1.
var closureSegment = regexp.MustCompile(`^(func|gowrap|deferwrap)?\d+$`)

// SplitFunction splits a Go function name into spark's class and method: the
// class is the package path, with the receiver type for a method
// ("github.com/x/y.(*T)"), the method the rest ("Run", "Run.func1"). The
// viewer prints them as class.method(), which reads back as the Go name.
// Type arguments are shortened to [...], as go tool pprof shows them.
func SplitFunction(name string) (pkg, class, method string) {
	name = shortenTypeArgs(name)
	slash := strings.LastIndex(name, "/")
	dot := indexOutsideBrackets(name[slash+1:], '.')
	if dot < 0 {
		return "", "", name
	}
	pkg = name[:slash+1+dot]
	sym := name[slash+1+dot+1:]
	if strings.HasPrefix(sym, "(") {
		if end := strings.Index(sym, ")."); end > 0 {
			return pkg, pkg + "." + sym[:end+1], sym[end+2:]
		}
	}
	if i := indexOutsideBrackets(sym, '.'); i > 0 {
		rest := sym[i+1:]
		first := rest
		if before, _, ok := strings.Cut(rest, "."); ok {
			first = before
		}
		if !closureSegment.MatchString(first) {
			// T.M: a method with a value receiver.
			return pkg, pkg + "." + sym[:i], rest
		}
	}
	return pkg, pkg, sym
}

// shortenTypeArgs replaces each outermost [...] with "[...]".
func shortenTypeArgs(name string) string {
	if !strings.Contains(name, "[") {
		return name
	}
	var b strings.Builder
	depth := 0
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c == '[':
			if depth == 0 {
				b.WriteString("[...]")
			}
			depth++
		case c == ']':
			depth--
		case depth == 0:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// indexOutsideBrackets is strings.IndexByte ignoring c inside a generic
// instantiation's brackets.
func indexOutsideBrackets(s string, c byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
		case c:
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// shortName is a goroutine's entry function without its package's
// directory, the name its goroutines are grouped under: "server.(*Server).tickLoop".
func shortName(function string) string {
	slash := strings.LastIndex(function, "/")
	return function[slash+1:]
}
