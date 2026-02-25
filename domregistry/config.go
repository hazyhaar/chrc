// CLAUDE:SUMMARY Configuration struct and defaults for domregistry — DB path, auto-accept, degraded threshold.
package domregistry

// Config holds the domregistry configuration.
type Config struct {
	DBPath string `json:"db_path" yaml:"db_path"`

	// AutoAccept enables automatic correction scoring and acceptance.
	AutoAccept bool `json:"auto_accept" yaml:"auto_accept"`

	// DegradedThreshold is the success_rate below which a profile is marked degraded.
	// Default: 0.5
	DegradedThreshold float64 `json:"degraded_threshold" yaml:"degraded_threshold"`
}

func (c *Config) defaults() {
	if c.DBPath == "" {
		c.DBPath = "domregistry.db"
	}
	if c.DegradedThreshold == 0 {
		c.DegradedThreshold = 0.5
	}
}
