// Package activitylog is spark's activity.json: the uploads and files spark
// made, newest first. spark: ActivityLog, Activity
package activitylog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// The kinds of data an activity points at.
const (
	DataURL  = "url"
	DataFile = "file"
)

// urlLifetime is how long an uploaded URL is kept: bytebin forgets content
// after a while.
const urlLifetime = 60 * 24 * time.Hour

// User is who caused an activity, in spark's JSON shape.
type User struct {
	Type     string `json:"type"` // "player" or "other"
	Name     string `json:"name"`
	UniqueID string `json:"uniqueId,omitempty"`
}

// Data is what an activity produced: a URL or a file path.
type Data struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Activity is one entry.
type Activity struct {
	User User `json:"user"`
	// Time is unix milliseconds.
	Time int64  `json:"time"`
	Type string `json:"type"` // "Profiler", "Heap dump summary", ...
	Data Data   `json:"data"`
}

// Expired reports whether a URL entry is past its lifetime; files never
// expire.
func (a Activity) Expired(now time.Time) bool {
	return a.Data.Type == DataURL && now.Sub(time.UnixMilli(a.Time)) > urlLifetime
}

// Log is the activity log of one file, safe from any goroutine.
type Log struct {
	path string
	mu   sync.Mutex
	log  []Activity
}

// Load reads path, dropping expired entries (and saving when it dropped
// any); a missing or broken file is an empty log.
func Load(path string) *Log {
	l := &Log{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return l
	}
	var entries []Activity
	if json.Unmarshal(data, &entries) != nil {
		return l
	}
	now := time.Now()
	l.log = slices.DeleteFunc(entries, func(a Activity) bool { return a.Expired(now) })
	if len(l.log) != len(entries) {
		_ = l.save()
	}
	return l
}

// Add puts a at the front and saves.
func (l *Log) Add(a Activity) error {
	l.mu.Lock()
	l.log = append([]Activity{a}, l.log...)
	l.mu.Unlock()
	return l.save()
}

// Entries answers the entries not expired, newest first.
func (l *Log) Entries() []Activity {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	return slices.DeleteFunc(slices.Clone(l.log), func(a Activity) bool { return a.Expired(now) })
}

func (l *Log) save() error {
	entries := l.Entries()
	if entries == nil {
		entries = []Activity{}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(l.path, data, 0o644)
}
