// Package spark is spark, the performance profiler, ported to golem.
package spark

import (
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/platform"
)

// Spark is the plugin. spark: BukkitSparkPlugin
type Spark struct {
	platform *platform.Platform
}

func init() { plugin.Register("spark", &Spark{}) }

// Enable starts spark.
func (s *Spark) Enable(srv plugin.Server) error {
	s.platform = platform.New(srv)
	return s.platform.Enable()
}

// Disable stops spark.
func (s *Spark) Disable() {
	if s.platform != nil {
		s.platform.Disable()
		s.platform = nil
	}
}
