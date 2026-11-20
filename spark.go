// Package spark is spark, the performance profiler, ported to golem.
package spark

import "github.com/golem-mc/golem/plugin"

// Spark is the plugin.
type Spark struct {
	server plugin.Server
}

func init() { plugin.Register("spark", &Spark{}) }

// Enable starts the plugin.
func (s *Spark) Enable(srv plugin.Server) error {
	s.server = srv
	return nil
}

// Disable stops the plugin.
func (s *Spark) Disable() {}
