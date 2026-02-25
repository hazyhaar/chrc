package veille

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSourceInput_EmptyName(t *testing.T) {
	// WHAT: Empty name is rejected.
	// WHY: A source without a name is unusable in the UI and MCP tools.
	s := &Source{Name: "", URL: "https://example.com", SourceType: "web", FetchInterval: 3600000}
	err := validateSourceInput(s)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestValidateSourceInput_NameTooLong(t *testing.T) {
	// WHAT: Name > 512 chars is rejected.
	// WHY: Prevents DB bloat from absurdly long names.
	s := &Source{Name: strings.Repeat("x", 513), URL: "https://example.com", SourceType: "web", FetchInterval: 3600000}
	err := validateSourceInput(s)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestValidateSourceInput_URLTooLong(t *testing.T) {
	// WHAT: URL > 4096 chars is rejected.
	// WHY: Prevents absurdly long URLs that could cause issues.
	s := &Source{Name: "Test", URL: "https://example.com/" + strings.Repeat("x", 4080), SourceType: "web", FetchInterval: 3600000}
	err := validateSourceInput(s)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestValidateSourceInput_InvalidSourceType(t *testing.T) {
	// WHAT: Unknown source types are rejected.
	// WHY: Unknown types hit unpredictable code paths in the pipeline.
	s := &Source{Name: "Test", URL: "https://example.com", SourceType: "evil", FetchInterval: 3600000}
	err := validateSourceInput(s)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestValidateSourceInput_FetchIntervalTooLow(t *testing.T) {
	// WHAT: fetch_interval < 60000ms (1 min) is rejected.
	// WHY: fetch_interval=0 causes infinite fetch loops (DoS).
	cases := []int64{0, 1, 100, 59999}
	for _, interval := range cases {
		s := &Source{Name: "Test", URL: "https://example.com", SourceType: "web", FetchInterval: interval}
		err := validateSourceInput(s)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("interval=%d: expected ErrInvalidInput, got: %v", interval, err)
		}
	}
}

func TestValidateSourceInput_FetchIntervalTooHigh(t *testing.T) {
	// WHAT: fetch_interval > 604800000ms (7 days) is rejected.
	// WHY: Unreasonably large intervals suggest misconfiguration.
	s := &Source{Name: "Test", URL: "https://example.com", SourceType: "web", FetchInterval: 604800001}
	err := validateSourceInput(s)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestValidateSourceInput_InvalidConfigJSON(t *testing.T) {
	// WHAT: config_json that isn't valid JSON is rejected.
	// WHY: Invalid JSON would cause downstream parsing failures.
	s := &Source{Name: "Test", URL: "https://example.com", SourceType: "web", FetchInterval: 3600000, ConfigJSON: "not json"}
	err := validateSourceInput(s)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestValidateSourceInput_ConfigJSONTooLarge(t *testing.T) {
	// WHAT: config_json > 8192 bytes is rejected.
	// WHY: Prevents DB bloat from large config payloads.
	s := &Source{Name: "Test", URL: "https://example.com", SourceType: "web", FetchInterval: 3600000, ConfigJSON: `{"x":"` + strings.Repeat("a", 8200) + `"}`}
	err := validateSourceInput(s)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestValidateSourceInput_ValidInputAccepted(t *testing.T) {
	// WHAT: Valid input passes validation.
	// WHY: Validation must not block legitimate sources.
	cases := []struct {
		name string
		src  Source
	}{
		{
			"web source",
			Source{Name: "Example", URL: "https://example.com", SourceType: "web", FetchInterval: 3600000},
		},
		{
			"rss source",
			Source{Name: "Feed", URL: "https://example.com/feed", SourceType: "rss", FetchInterval: 60000},
		},
		{
			"api source with config",
			Source{Name: "API", URL: "https://api.example.com/v1", SourceType: "api", FetchInterval: 604800000, ConfigJSON: `{"key":"value"}`},
		},
		{
			"document source",
			Source{Name: "Doc", URL: "reports/q1.pdf", SourceType: "document", FetchInterval: 86400000},
		},
		{
			"question source",
			Source{Name: "Q", URL: "question://abc", SourceType: "question", FetchInterval: 86400000},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSourceInput(&tc.src); err != nil {
				t.Errorf("expected nil error, got: %v", err)
			}
		})
	}
}

func TestValidateSourceInput_EmptyURL(t *testing.T) {
	// WHAT: Empty URL is rejected.
	// WHY: A source must have a URL to fetch.
	s := &Source{Name: "Test", URL: "", SourceType: "web", FetchInterval: 3600000}
	err := validateSourceInput(s)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestValidateSourceInput_DefaultsNotValidated(t *testing.T) {
	// WHAT: Fields with zero values that have defaults (source_type, fetch_interval)
	// are validated before defaults are applied — caller must set them.
	// WHY: validateSourceInput runs before InsertSource sets defaults.
	// If caller doesn't set source_type, validate should still accept empty
	// since AddSource flow sets defaults after validation.

	// Actually, per the plan: source_type must be in the allowed set.
	// An empty source_type will fail validation.
	s := &Source{Name: "Test", URL: "https://example.com", SourceType: "", FetchInterval: 3600000}
	err := validateSourceInput(s)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty source_type should fail: got %v", err)
	}
}
