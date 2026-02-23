package fetcher

import (
	"bytes"
	"strings"
)

// IsSufficient returns true if the HTML body has enough text content
// relative to markup, indicating a browser isn't needed.
// Heuristic: compute text-to-markup ratio and check minimums.
func IsSufficient(html []byte) bool {
	if len(html) < 256 {
		return false
	}

	textLen, markupLen := textMarkupRatio(html)
	total := textLen + markupLen
	if total == 0 {
		return false
	}

	ratio := float64(textLen) / float64(total)

	// If less than 10% text, likely an SPA shell.
	if ratio < 0.10 {
		return false
	}

	// Must have at least 200 chars of visible text.
	if textLen < 200 {
		return false
	}

	// Check for common SPA indicators.
	lower := bytes.ToLower(html)
	spaIndicators := []string{
		"<div id=\"root\"></div>",
		"<div id=\"app\"></div>",
		"<div id=\"__next\"></div>",
		"<noscript>you need to enable javascript",
		"<noscript>enable javascript",
	}
	for _, ind := range spaIndicators {
		if bytes.Contains(lower, []byte(ind)) {
			return false
		}
	}

	return true
}

// textMarkupRatio computes the approximate byte count of text vs markup.
func textMarkupRatio(html []byte) (text, markup int) {
	inTag := false
	inScript := false
	inStyle := false

	s := string(html)
	i := 0
	for i < len(s) {
		if inScript {
			idx := strings.Index(s[i:], "</script")
			if idx == -1 {
				break
			}
			markup += idx + len("</script>")
			i += idx
			// Find closing >
			end := strings.IndexByte(s[i:], '>')
			if end >= 0 {
				i += end + 1
			}
			inScript = false
			continue
		}
		if inStyle {
			idx := strings.Index(s[i:], "</style")
			if idx == -1 {
				break
			}
			markup += idx + len("</style>")
			i += idx
			end := strings.IndexByte(s[i:], '>')
			if end >= 0 {
				i += end + 1
			}
			inStyle = false
			continue
		}

		ch := s[i]
		if ch == '<' {
			inTag = true
			markup++
			// Check for script/style opening tags.
			rest := strings.ToLower(s[i:])
			if strings.HasPrefix(rest, "<script") {
				inScript = true
			} else if strings.HasPrefix(rest, "<style") {
				inStyle = true
			}
			i++
			continue
		}
		if ch == '>' {
			inTag = false
			markup++
			i++
			continue
		}
		if inTag {
			markup++
		} else {
			// Count non-whitespace text.
			if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
				text++
			}
		}
		i++
	}
	return text, markup
}
