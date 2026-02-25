// CLAUDE:SUMMARY Error classifier: maps (sourceType, statusCode, errMsg) to (ErrorClass, Action).
// CLAUDE:DEPENDS (none — pure logic)
// CLAUDE:EXPORTS Classify, ErrorClass, Action
package repair

import (
	"strconv"
	"strings"
)

// ErrorClass categorizes a fetch error.
type ErrorClass string

const (
	ClassTemporary ErrorClass = "temporary"  // 5xx, timeout, DNS transient
	ClassRedirect  ErrorClass = "redirect"   // 301, 302
	ClassForbidden ErrorClass = "forbidden"  // 403
	ClassNotFound  ErrorClass = "not_found"  // 404, 410
	ClassAuth      ErrorClass = "auth"       // 401
	ClassRateLimit ErrorClass = "rate_limit" // 429
	ClassParse     ErrorClass = "parse"      // XML/JSON invalid
	ClassUnknown   ErrorClass = "unknown"
)

// Action is a recommended repair action for a classified error.
type Action string

const (
	ActionBackoff        Action = "backoff"         // increase fetch interval temporarily
	ActionFollowRedirect Action = "follow_redirect" // update URL from Location header
	ActionRotateUA       Action = "rotate_ua"       // try a different User-Agent
	ActionIncreaseRate   Action = "increase_rate"    // increase rate_limit_ms (search engines)
	ActionMarkBroken     Action = "mark_broken"      // disable, requires intervention
	ActionNone           Action = "none"             // do nothing (fail_count suffices)
)

// Classify determines the error class and recommended action from a fetch failure.
func Classify(sourceType string, statusCode int, errMsg string) (ErrorClass, Action) {
	// Redirects.
	if statusCode == 301 || statusCode == 302 || statusCode == 307 || statusCode == 308 {
		return ClassRedirect, ActionFollowRedirect
	}

	// Rate limiting.
	if statusCode == 429 {
		return ClassRateLimit, ActionIncreaseRate
	}

	// Auth failure.
	if statusCode == 401 {
		return ClassAuth, ActionMarkBroken
	}

	// Forbidden — web sources can try rotating UA.
	if statusCode == 403 {
		if sourceType == "web" || sourceType == "rss" {
			return ClassForbidden, ActionRotateUA
		}
		return ClassForbidden, ActionMarkBroken
	}

	// Not found / gone — broken.
	if statusCode == 404 || statusCode == 410 {
		return ClassNotFound, ActionMarkBroken
	}

	// Server errors — temporary, backoff.
	if statusCode >= 500 && statusCode < 600 {
		return ClassTemporary, ActionBackoff
	}

	// Parse errors (detected from error message).
	msg := strings.ToLower(errMsg)
	if isParseError(msg) {
		return ClassParse, ActionMarkBroken
	}

	// Network errors — temporary, backoff.
	if isNetworkError(msg) {
		return ClassTemporary, ActionBackoff
	}

	return ClassUnknown, ActionNone
}

// ExtractStatusCode extracts an HTTP status code from an error message.
// Returns 0 if no code found. Handles "http 503", "http: 404", "status 429", etc.
func ExtractStatusCode(errMsg string) int {
	msg := strings.ToLower(errMsg)
	for _, prefix := range []string{"http ", "http: ", "status ", "status: "} {
		idx := strings.Index(msg, prefix)
		if idx < 0 {
			continue
		}
		numStr := strings.TrimSpace(msg[idx+len(prefix):])
		// Take first word (the code).
		if sp := strings.IndexByte(numStr, ' '); sp > 0 {
			numStr = numStr[:sp]
		}
		if code, err := strconv.Atoi(numStr); err == nil && code >= 100 && code < 600 {
			return code
		}
	}
	return 0
}

func isParseError(msg string) bool {
	return strings.Contains(msg, "xml") && (strings.Contains(msg, "parse") || strings.Contains(msg, "syntax") || strings.Contains(msg, "unexpected")) ||
		strings.Contains(msg, "json") && (strings.Contains(msg, "unmarshal") || strings.Contains(msg, "invalid") || strings.Contains(msg, "unexpected")) ||
		strings.Contains(msg, "encoding") && strings.Contains(msg, "invalid")
}

func isNetworkError(msg string) bool {
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dns") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "tls handshake")
}
