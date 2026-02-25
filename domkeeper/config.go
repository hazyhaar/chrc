// CLAUDE:SUMMARY Configuration structs (chunk, scheduler) and YAML loader for domkeeper.
package domkeeper

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all domkeeper configuration.
type Config struct {
	DBPath    string          `yaml:"db_path"`
	Chunk     ChunkConfig     `yaml:"chunk"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
}

// ChunkConfig controls text chunking behaviour.
type ChunkConfig struct {
	MaxTokens      int `yaml:"max_tokens"`
	OverlapTokens  int `yaml:"overlap_tokens"`
	MinChunkTokens int `yaml:"min_chunk_tokens"`
}

// SchedulerConfig controls the freshness scheduler.
type SchedulerConfig struct {
	CheckInterval    time.Duration `yaml:"check_interval"`
	DefaultFreshness time.Duration `yaml:"default_freshness"`
	MaxFailCount     int           `yaml:"max_fail_count"`
	Visibility       time.Duration `yaml:"visibility"`
	PollInterval     time.Duration `yaml:"poll_interval"`
}

func (c *Config) defaults() {
	if c.DBPath == "" {
		c.DBPath = "domkeeper.db"
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
		c.Scheduler.CheckInterval = 5 * time.Minute
	}
	if c.Scheduler.DefaultFreshness <= 0 {
		c.Scheduler.DefaultFreshness = 1 * time.Hour
	}
	if c.Scheduler.MaxFailCount <= 0 {
		c.Scheduler.MaxFailCount = 10
	}
	if c.Scheduler.Visibility <= 0 {
		c.Scheduler.Visibility = 60 * time.Second
	}
	if c.Scheduler.PollInterval <= 0 {
		c.Scheduler.PollInterval = 5 * time.Second
	}
}

// LoadConfigFile reads a YAML config file.
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
