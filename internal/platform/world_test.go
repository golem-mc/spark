package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/spark/internal/activitylog"
)

func TestGroupIntoRegions(t *testing.T) {
	c := func(x, z, n int32) chunkInfo { return chunkInfo{x: x, z: z, total: n} }
	regions := groupIntoRegions([]chunkInfo{c(0, 0, 1), c(5, 5, 2), c(1, 1, 3), c(2, 0, 0), c(2, 2, 4), c(6, 5, 1)})
	if len(regions) != 2 {
		t.Fatalf("regions = %v", regions)
	}
	total := func(r []chunkInfo) (n int32) {
		for _, c := range r {
			n += c.total
		}
		return n
	}
	// (0,0), (1,1) and (2,2) touch diagonally; (2,0) has no entities.
	if len(regions[0]) != 3 || total(regions[0]) != 8 || len(regions[1]) != 2 || total(regions[1]) != 3 {
		t.Errorf("regions = %v", regions)
	}
}

func TestTypedProperty(t *testing.T) {
	if typedProperty("true") != true || typedProperty("25565") != int64(25565) || typedProperty("-1") != "-1" || typedProperty("abc") != "abc" {
		t.Error("typed properties")
	}
}

func TestDateDiff(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{{0, "now"}, {5 * time.Second, "5s ago"}, {90 * time.Minute, "1h 30m ago"}, {49*time.Hour + 3*time.Second, "2d 1h 3s ago"}} {
		if got := dateDiff(now.Add(-c.ago), now); got != c.want {
			t.Errorf("%v: %q, want %q", c.ago, got, c.want)
		}
	}
}

func TestActivityPage(t *testing.T) {
	var entries []activitylog.Activity
	for i := range 5 {
		entries = append(entries, activitylog.Activity{User: activitylog.User{Name: "Alice"}, Time: int64(i), Type: "Profiler",
			Data: activitylog.Data{Type: activitylog.DataURL, Value: "https://spark.lucko.me/x"}})
	}
	page := activityPage(entries, 2, time.UnixMilli(10_000))
	if len(page) != 1+4+1 {
		t.Fatalf("page 2 has %d lines", len(page))
	}
	if got := page[1].PlainString(); got != "> #5 - Profiler - 9s ago" {
		t.Errorf("row = %q", got)
	}
	if got := page[5].PlainString(); !strings.Contains(got, "- « Page 2/2 » -") || len([]rune(got)) != pageWidth {
		t.Errorf("footer = %q", got)
	}
	if got := activityPage(entries, 3, time.Now())[0].PlainString(); got != "[⚡] Unknown page selected. 2 total pages." {
		t.Errorf("unknown page = %q", got)
	}
}
