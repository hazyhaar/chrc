package domwatch

import (
	"github.com/hazyhaar/chrc/domwatch/internal/config"
)

// Config is the top-level domwatch configuration. Re-exported from internal.
type Config = config.Config

// BrowserConfig controls Chrome lifecycle.
type BrowserConfig = config.BrowserConfig

// PageConfig defines a page to observe.
type PageConfig = config.PageConfig

// DebounceConfig controls mutation batching.
type DebounceConfig = config.DebounceConfig

// SinkConfig defines an output backend.
type SinkConfig = config.SinkConfig

// LoadConfigFile reads a YAML configuration file.
func LoadConfigFile(path string) (*Config, error) {
	return config.LoadFile(path)
}
