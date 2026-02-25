// CLAUDE:SUMMARY Config struct for veille service: fetch, scheduler, data directory, and buffer settings.
package veille

import (
	"time"

	fetchpkg "github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/scheduler"
)

// Config configures the veille service.
type Config struct {
	// Fetch settings
	Fetch fetchpkg.Config

	// Scheduler settings
	Scheduler scheduler.Config

	// DataDir is the root directory for shard databases.
	DataDir string

	// BufferDir is the directory for .md buffer output (pending/).
	// If empty, buffer writing is disabled.
	BufferDir string
}

func (c *Config) defaults() {
	if c.Fetch.Timeout <= 0 {
		c.Fetch.Timeout = 30 * time.Second
	}
	if c.Fetch.MaxBytes <= 0 {
		c.Fetch.MaxBytes = 10 * 1024 * 1024
	}
	if c.Fetch.UserAgent == "" {
		c.Fetch.UserAgent = "chrc-veille/1.0"
	}
	if c.Scheduler.CheckInterval <= 0 {
		c.Scheduler.CheckInterval = time.Minute
	}
	if c.Scheduler.MaxFailCount <= 0 {
		c.Scheduler.MaxFailCount = 10
	}
	if c.DataDir == "" {
		c.DataDir = "data"
	}
}

func defaultConfig() *Config {
	return &Config{
		Fetch: fetchpkg.Config{
			Timeout:   30 * time.Second,
			MaxBytes:  10 * 1024 * 1024,
			UserAgent: "chrc-veille/1.0",
		},
		Scheduler: scheduler.Config{
			CheckInterval: time.Minute,
			MaxFailCount:  10,
		},
		DataDir: "data",
	}
}
