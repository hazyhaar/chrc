// CLAUDE:SUMMARY URL normalization for source dedup: lowercase scheme/host, remove fragment, sort query params, strip trailing slash.
// CLAUDE:EXPORTS NormalizeSourceURL
package veille

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// NormalizeSourceURL normalizes a source URL for dedup comparison.
// For http/https URLs: lowercases scheme and host, removes fragment,
// strips trailing slash (except root), sorts query params.
// Internal schemes (question://, file paths) are returned as-is.
// Does NOT upgrade http to https (different servers, different resources).
func NormalizeSourceURL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%w: empty URL", ErrInvalidInput)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	scheme := strings.ToLower(parsed.Scheme)

	// No scheme at all and contains spaces → clearly not a URL.
	if scheme == "" && strings.Contains(raw, " ") {
		return "", fmt.Errorf("%w: malformed URL", ErrInvalidInput)
	}

	// No scheme and no path separator → ambiguous, reject.
	if scheme == "" && !strings.Contains(raw, "/") && !strings.Contains(raw, ".") {
		return "", fmt.Errorf("%w: malformed URL", ErrInvalidInput)
	}

	// Internal/synthetic schemes or relative paths — return as-is.
	if scheme != "http" && scheme != "https" {
		return raw, nil
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidInput)
	}

	// Lowercase scheme and host.
	parsed.Scheme = scheme
	parsed.Host = strings.ToLower(parsed.Host)

	// Remove fragment.
	parsed.Fragment = ""

	// Strip trailing slash from path (unless empty/root).
	parsed.Path = strings.TrimRight(parsed.Path, "/")

	// Sort query params by key for stable comparison.
	if parsed.RawQuery != "" {
		params := parsed.Query()
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var buf strings.Builder
		for i, k := range keys {
			vals := params[k]
			sort.Strings(vals)
			for j, v := range vals {
				if i > 0 || j > 0 {
					buf.WriteByte('&')
				}
				buf.WriteString(url.QueryEscape(k))
				buf.WriteByte('=')
				buf.WriteString(url.QueryEscape(v))
			}
		}
		parsed.RawQuery = buf.String()
	}

	return parsed.String(), nil
}
