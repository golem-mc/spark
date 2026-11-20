// Package config is spark's config.json, overridden by SPARK_* environment
// variables. spark: FileConfiguration, RuntimeConfiguration, CombinedConfiguration
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const header = "spark configuration file - https://spark.lucko.me/docs/Configuration"

// Config is config.json with its keys in file order, safe from any goroutine.
// Java system properties (-Dspark.x) have no Go counterpart; the environment
// variables remain.
type Config struct {
	path string

	mu     sync.Mutex
	keys   []string
	values map[string]json.RawMessage
}

// Load reads path; a missing or unreadable file starts from spark's header.
func Load(path string) *Config {
	c := &Config{path: path}
	c.load()
	return c
}

func (c *Config) load() {
	c.keys, c.values = nil, map[string]json.RawMessage{}
	data, err := os.ReadFile(c.path)
	if err == nil {
		if keys, values, err := parseObject(data); err == nil {
			c.keys, c.values = keys, values
			return
		}
	}
	c.setRaw("_header", mustJSON(header))
}

// parseObject reads a JSON object keeping its key order.
func parseObject(data []byte) ([]string, map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, errors.New("config: not a JSON object")
	}
	var keys []string
	values := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, nil, err
		}
		if _, seen := values[key]; !seen {
			keys = append(keys, key)
		}
		values[key] = raw
	}
	return keys, values, nil
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func (c *Config) setRaw(key string, raw json.RawMessage) {
	if _, ok := c.values[key]; !ok {
		c.keys = append(c.keys, key)
	}
	c.values[key] = raw
}

// Save writes the file, pretty-printed as Gson does.
func (c *Config) Save() error {
	c.mu.Lock()
	var buf bytes.Buffer
	buf.WriteString("{")
	for i, k := range c.keys {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString("\n  ")
		buf.Write(mustJSON(k))
		buf.WriteString(": ")
		var v bytes.Buffer
		if err := json.Indent(&v, c.values[k], "  ", "  "); err != nil {
			v.Reset()
			v.Write(c.values[k])
		}
		buf.Write(v.Bytes())
	}
	buf.WriteString("\n}")
	c.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.path, buf.Bytes(), 0o644)
}

// envName is the variable that overrides key: SPARK_ and the key upper-cased,
// dots and dashes as underscores. spark: RuntimeConfiguration.ENVIRONMENT_VARIABLES
func envName(key string) string {
	return "SPARK_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key))
}

// fileValue answers the file's value for key when it is a JSON primitive.
func (c *Config) fileValue(key string) (any, bool) {
	c.mu.Lock()
	raw, ok := c.values[key]
	c.mu.Unlock()
	if !ok {
		return nil, false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	switch v.(type) {
	case string, bool, float64:
		return v, true
	}
	return nil, false
}

// String answers key's value, def when neither the file nor the environment
// sets it.
func (c *Config) String(key, def string) string {
	result := def
	if v, ok := c.fileValue(key); ok {
		switch v := v.(type) {
		case string:
			result = v
		case bool:
			result = strconv.FormatBool(v)
		case float64:
			result = strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	if env, ok := os.LookupEnv(envName(key)); ok {
		result = env
	}
	return result
}

// Bool answers key's value; the file's must be a JSON boolean, the
// environment's "true" (any case) or anything else for false.
func (c *Config) Bool(key string, def bool) bool {
	result := def
	if v, ok := c.fileValue(key); ok {
		if b, ok := v.(bool); ok {
			result = b
		}
	}
	if env, ok := os.LookupEnv(envName(key)); ok {
		result = strings.EqualFold(env, "true")
	}
	return result
}

// Int answers key's value; the file's must be a JSON number (truncated), the
// environment's an integer.
func (c *Config) Int(key string, def int) int {
	result := def
	if v, ok := c.fileValue(key); ok {
		if f, ok := v.(float64); ok {
			result = int(f)
		}
	}
	if env, ok := os.LookupEnv(envName(key)); ok {
		if n, err := strconv.Atoi(env); err == nil {
			result = n
		}
	}
	return result
}

// StringList answers key's array of strings; the environment's value is
// comma-separated and wins when it is not empty.
func (c *Config) StringList(key string) []string {
	if env := os.Getenv(envName(key)); env != "" {
		return strings.Split(env, ",")
	}
	c.mu.Lock()
	raw, ok := c.values[key]
	c.mu.Unlock()
	if !ok {
		return nil
	}
	var items []any
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []string
	for _, it := range items {
		switch it := it.(type) {
		case string:
			out = append(out, it)
		case bool:
			out = append(out, strconv.FormatBool(it))
		case float64:
			out = append(out, strconv.FormatFloat(it, 'f', -1, 64))
		}
	}
	return out
}

// Contains reports whether the file or the environment sets key.
func (c *Config) Contains(key string) bool {
	if _, ok := os.LookupEnv(envName(key)); ok {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.values[key]
	return ok
}

// Set sets key in the file (call Save to write it).
func (c *Config) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setRaw(key, mustJSON(value))
}

// Remove deletes key from the file (call Save to write it).
func (c *Config) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.values[key]; !ok {
		return
	}
	delete(c.values, key)
	for i, k := range c.keys {
		if k == key {
			c.keys = append(c.keys[:i], c.keys[i+1:]...)
			break
		}
	}
}
