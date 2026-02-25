// CLAUDE:SUMMARY Defines domwatch config structs and parses YAML configuration files with defaults.
// Package config handles domwatch configuration from YAML files or SQLite.
package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level domwatch configuration.
type Config struct {
	Browser   BrowserConfig `yaml:"browser"`
	Pages     []PageConfig  `yaml:"pages"`
	Debounce  DebounceConfig `yaml:"debounce"`
	Sinks     []SinkConfig  `yaml:"sinks"`
}

// BrowserConfig controls Chrome lifecycle.
type BrowserConfig struct {
	Remote           string        `yaml:"remote"`
	MemoryLimit      int64         `yaml:"memory_limit"`
	RecycleInterval  time.Duration `yaml:"recycle_interval"`
	ResourceBlocking []string      `yaml:"resource_blocking"`
	Stealth          string        `yaml:"stealth"` // headless | headful
	XvfbDisplay      string        `yaml:"xvfb_display"`
}

// PageConfig defines a page to observe.
type PageConfig struct {
	ID               string        `yaml:"id"`
	URL              string        `yaml:"url"`
	StealthLevel     string        `yaml:"stealth_level"`     // 0 | 1 | 2 | auto
	Selectors        []string      `yaml:"selectors"`
	Filters          []string      `yaml:"filters"`
	SnapshotInterval time.Duration `yaml:"snapshot_interval"`
	Profile          bool          `yaml:"profile"`
}

// DebounceConfig controls mutation batching.
type DebounceConfig struct {
	Window    time.Duration `yaml:"window"`
	MaxBuffer int           `yaml:"max_buffer"`
}

// SinkConfig defines an output backend.
type SinkConfig struct {
	Type          string `yaml:"type"`   // stdout | webhook | callback
	URL           string `yaml:"url"`    // for webhook
	SubjectPrefix string `yaml:"subject_prefix"` // for nats
}

// LoadFile reads a YAML configuration file.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Browser.MemoryLimit <= 0 {
		c.Browser.MemoryLimit = 1 << 30
	}
	if c.Browser.RecycleInterval <= 0 {
		c.Browser.RecycleInterval = 4 * time.Hour
	}
	if c.Browser.XvfbDisplay == "" {
		c.Browser.XvfbDisplay = ":99"
	}
	if c.Browser.Stealth == "" {
		c.Browser.Stealth = "headless"
	}
	if c.Debounce.Window <= 0 {
		c.Debounce.Window = 250 * time.Millisecond
	}
	if c.Debounce.MaxBuffer <= 0 {
		c.Debounce.MaxBuffer = 1000
	}
	for i := range c.Pages {
		if c.Pages[i].StealthLevel == "" {
			c.Pages[i].StealthLevel = "auto"
		}
		if c.Pages[i].SnapshotInterval <= 0 {
			c.Pages[i].SnapshotInterval = 4 * time.Hour
		}
	}
}
