// CLAUDE:SUMMARY Input validation for source fields: name, URL, source_type, fetch_interval, config_json.
// CLAUDE:EXPORTS validateSourceInput, MaxSourcesPerSpace, allowedSourceTypes
package veille

import (
	"encoding/json"
	"fmt"
)

const (
	maxNameLen     = 512
	maxURLLen      = 4096
	maxConfigLen   = 8192
	minFetchMs     = 60_000      // 1 minute
	maxFetchMs     = 604_800_000 // 7 days

	// MaxSourcesPerSpace is the maximum number of sources per space.
	MaxSourcesPerSpace = 1000
)

// allowedSourceTypes is the set of valid source_type values.
var allowedSourceTypes = map[string]bool{
	"web":      true,
	"rss":      true,
	"api":      true,
	"document": true,
	"question": true,
}

// validateSourceInput validates a source's mutable fields before insert or update.
// If knownTypes is nil, the built-in allowedSourceTypes set is used.
func validateSourceInput(s *Source, knownTypes ...map[string]bool) error {
	types := allowedSourceTypes
	if len(knownTypes) > 0 && knownTypes[0] != nil {
		types = knownTypes[0]
	}
	if s.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if len(s.Name) > maxNameLen {
		return fmt.Errorf("%w: name exceeds %d characters", ErrInvalidInput, maxNameLen)
	}

	if s.URL == "" {
		return fmt.Errorf("%w: url is required", ErrInvalidInput)
	}
	if len(s.URL) > maxURLLen {
		return fmt.Errorf("%w: url exceeds %d characters", ErrInvalidInput, maxURLLen)
	}

	if !types[s.SourceType] {
		return fmt.Errorf("%w: unknown source_type %q", ErrInvalidInput, s.SourceType)
	}

	if s.FetchInterval < minFetchMs || s.FetchInterval > maxFetchMs {
		return fmt.Errorf("%w: fetch_interval must be between %d and %d ms", ErrInvalidInput, minFetchMs, maxFetchMs)
	}

	if s.ConfigJSON != "" && s.ConfigJSON != "{}" {
		if len(s.ConfigJSON) > maxConfigLen {
			return fmt.Errorf("%w: config_json exceeds %d bytes", ErrInvalidInput, maxConfigLen)
		}
		if !json.Valid([]byte(s.ConfigJSON)) {
			return fmt.Errorf("%w: config_json is not valid JSON", ErrInvalidInput)
		}
	}

	return nil
}
