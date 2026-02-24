package veille

import (
	"time"

	"github.com/hazyhaar/chrc/chunk"
	fetchpkg "github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/scheduler"
)

// Config configures the veille service.
type Config struct {
	// Fetch settings
	Fetch fetchpkg.Config

	// Chunk splitting parameters
	Chunk chunk.Options

	// Scheduler settings
	Scheduler scheduler.Config

	// DataDir is the root directory for shard databases.
	DataDir string
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
	if c.Chunk.MaxTokens <= 0 {
		c.Chunk.MaxTokens = 512
	}
	if c.Chunk.OverlapTokens <= 0 {
		c.Chunk.OverlapTokens = 64
	}
	if c.Chunk.MinChunkTokens <= 0 {
		c.Chunk.MinChunkTokens = 32
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
		Chunk: chunk.Options{
			MaxTokens:      512,
			OverlapTokens:  64,
			MinChunkTokens: 32,
		},
		Scheduler: scheduler.Config{
			CheckInterval: time.Minute,
			MaxFailCount:  10,
		},
		DataDir: "data",
	}
}
