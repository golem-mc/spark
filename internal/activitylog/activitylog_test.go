package activitylog

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity.json")
	old := time.Now().Add(-61 * 24 * time.Hour).UnixMilli()
	if err := os.WriteFile(path, []byte(`[
  {"user": {"type": "other", "name": "Console"}, "time": `+itoa(old)+`, "type": "Profiler", "data": {"type": "url", "value": "https://spark.lucko.me/old"}},
  {"user": {"type": "other", "name": "Console"}, "time": `+itoa(old)+`, "type": "Heap dump", "data": {"type": "file", "value": "heap.hprof"}}
]`), 0o644); err != nil {
		t.Fatal(err)
	}
	l := Load(path)
	if got := l.Entries(); len(got) != 1 || got[0].Data.Value != "heap.hprof" {
		t.Fatalf("expired URL not dropped: %+v", got)
	}
	if err := l.Add(Activity{User: User{Type: "player", Name: "Alice", UniqueID: "abc"}, Time: time.Now().UnixMilli(),
		Type: "Profiler", Data: Data{DataURL, "https://spark.lucko.me/new"}}); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).Entries(); len(got) != 2 || got[0].User.Name != "Alice" {
		t.Fatalf("reloaded = %+v", got)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"uniqueId": "abc"`) || strings.Contains(string(data), "/old") {
		t.Errorf("file = %s", data)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
