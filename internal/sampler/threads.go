package sampler

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/spark/internal/sparkproto"
)

// ServerThread is the name of the tick goroutine's samples, which spark
// calls the server thread.
const ServerThread = "Server thread"

// Threads chooses which goroutines' samples are kept. A goroutine is named
// ServerThread when it is the tick goroutine, else by the function it was
// started with. spark: ThreadDumper
type Threads struct {
	kind     sparkproto.SamplerMetadata_ThreadDumper_Type
	names    []string // lower case, for SPECIFIC
	patterns []*regexp.Regexp
}

// ServerThreadOnly keeps the tick goroutine, spark's default.
func ServerThreadOnly() Threads {
	return Threads{kind: sparkproto.SamplerMetadata_ThreadDumper_SPECIFIC, names: []string{strings.ToLower(ServerThread)}}
}

// AllThreads keeps every goroutine (--thread *).
func AllThreads() Threads { return Threads{kind: sparkproto.SamplerMetadata_ThreadDumper_ALL} }

// NamedThreads keeps the goroutines with one of names, ignoring case.
func NamedThreads(names []string) Threads {
	t := Threads{kind: sparkproto.SamplerMetadata_ThreadDumper_SPECIFIC}
	for _, n := range names {
		t.names = append(t.names, strings.ToLower(n))
	}
	return t
}

// MatchingThreads keeps the goroutines whose whole name matches one of the
// patterns, ignoring case (--regex).
func MatchingThreads(patterns []string) (Threads, error) {
	t := Threads{kind: sparkproto.SamplerMetadata_ThreadDumper_REGEX}
	for _, p := range patterns {
		re, err := regexp.Compile("(?i)^(?:" + p + ")$")
		if err != nil {
			return Threads{}, err
		}
		t.patterns = append(t.patterns, re)
	}
	return t, nil
}

// ParseThreads reads the backgroundProfilerThreadDumper setting: "default"
// (the server thread), "all", or a comma-separated list of names.
// spark: ThreadDumper.parseConfigSetting
func ParseThreads(setting string) Threads {
	switch setting {
	case "default":
		return ServerThreadOnly()
	case "all":
		return AllThreads()
	}
	return NamedThreads(strings.Split(setting, ","))
}

func (t Threads) includes(name string) bool {
	switch t.kind {
	case sparkproto.SamplerMetadata_ThreadDumper_ALL:
		return true
	case sparkproto.SamplerMetadata_ThreadDumper_REGEX:
		return slices.ContainsFunc(t.patterns, func(re *regexp.Regexp) bool { return re.MatchString(name) })
	}
	return slices.Contains(t.names, strings.ToLower(name))
}

func (t Threads) proto() *sparkproto.SamplerMetadata_ThreadDumper {
	p := &sparkproto.SamplerMetadata_ThreadDumper{Type: t.kind}
	for _, re := range t.patterns {
		s := re.String()
		p.Patterns = append(p.Patterns, s[len("(?i)^(?:"):len(s)-len(")$")])
	}
	return p
}

// Grouper merges goroutines' samples into the viewer's threads.
// spark: ThreadGrouper
type Grouper int

const (
	// ByName keeps each name apart.
	ByName Grouper = iota
	// ByPool merges names that differ only by a trailing number, spark's
	// default.
	ByPool
	// AsOne merges everything into one thread.
	AsOne
)

// ParseGrouper reads the backgroundProfilerThreadGrouper setting.
func ParseGrouper(setting string) Grouper {
	switch setting {
	case "as-one":
		return AsOne
	case "by-name":
		return ByName
	}
	return ByPool
}

var poolSuffix = regexp.MustCompile(`^(.*?)[-# ]+(?:\d+|\(#?\d+\))$`)

func (g Grouper) group(name string) string {
	switch g {
	case AsOne:
		return "root"
	case ByPool:
		if m := poolSuffix.FindStringSubmatch(name); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return name
}

// label names a group in the viewer; members are the distinct names it
// merged.
func (g Grouper) label(group string, members int) string {
	switch {
	case g == AsOne:
		return "All (x" + strconv.Itoa(members) + ")"
	case g == ByPool && members > 1:
		return group + " (x" + strconv.Itoa(members) + ")"
	}
	return group
}

func (g Grouper) proto() sparkproto.SamplerMetadata_DataAggregator_ThreadGrouper {
	switch g {
	case AsOne:
		return sparkproto.SamplerMetadata_DataAggregator_AS_ONE
	case ByName:
		return sparkproto.SamplerMetadata_DataAggregator_BY_NAME
	}
	return sparkproto.SamplerMetadata_DataAggregator_BY_POOL
}
