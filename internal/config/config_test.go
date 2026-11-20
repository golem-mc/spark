package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"viewerUrl": "https://example.org/", "backgroundProfiler": false, "backgroundProfilerInterval": 7, "trustedKeys": ["a", "b"], "nested": {"x": 1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load(path)
	if got := c.String("viewerUrl", "def"); got != "https://example.org/" {
		t.Errorf("viewerUrl = %q", got)
	}
	if c.Bool("backgroundProfiler", true) {
		t.Error("backgroundProfiler should be false")
	}
	if c.Bool("missing", true) != true || c.String("missing", "d") != "d" {
		t.Error("defaults not used")
	}
	if c.Int("backgroundProfilerInterval", 10) != 7 {
		t.Error("interval")
	}
	if c.Bool("viewerUrl", true) != true {
		t.Error("a string is no boolean")
	}
	if got := c.StringList("trustedKeys"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("trustedKeys = %q", got)
	}
	t.Setenv("SPARK_VIEWERURL", "https://env.example/")
	if got := c.String("viewerUrl", "def"); got != "https://env.example/" {
		t.Errorf("env override = %q", got)
	}

	c.Set("backgroundProfiler", true)
	c.Remove("nested")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "{\n  \"viewerUrl\": \"https://example.org/\",\n  \"backgroundProfiler\": true,\n  \"backgroundProfilerInterval\": 7,\n  \"trustedKeys\": [\n    \"a\",\n    \"b\"\n  ]\n}"
	if string(data) != want {
		t.Errorf("saved:\n%s\nwant:\n%s", data, want)
	}
}

func TestConfigMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	c := Load(path)
	if !c.Contains("_header") {
		t.Error("a new config starts with spark's header")
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
