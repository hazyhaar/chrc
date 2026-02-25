// CLAUDE:SUMMARY JSON API fetcher with dot-notation result walker, field mapping, and env var expansion.
// Package apifetch fetches and extracts structured results from JSON APIs.
//
// It supports configurable HTTP method, headers (with ${ENV_VAR} expansion),
// dot-notation path walking for nested results, and field mapping.
package apifetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Config describes how to call and parse a JSON API.
type Config struct {
	Method      string            `json:"method"`        // HTTP method, default GET
	Headers     map[string]string `json:"headers"`       // ${ENV_VAR} expanded
	ResultPath  string            `json:"result_path"`   // dot-notation: "data.results"
	Fields      map[string]string `json:"fields"`        // {"title":"name","text":"body","url":"link"}
	RateLimitMs int64             `json:"rate_limit_ms"` // minimum ms between requests
}

// Result is one extracted item from an API response.
type Result struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	URL   string `json:"url"`
}

// Fetch calls the API at baseURL with the given config, parses the JSON
// response, walks result_path, and extracts fields into Results.
func Fetch(ctx context.Context, client *http.Client, baseURL string, cfg Config) ([]Result, error) {
	method := cfg.Method
	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("apifetch: new request: %w", err)
	}

	for k, v := range cfg.Headers {
		req.Header.Set(k, expandEnv(v))
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apifetch: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("apifetch: http %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("apifetch: read body: %w", err)
	}

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("apifetch: json decode: %w", err)
	}

	// Walk result_path to find the array of items.
	items, err := walkPath(raw, cfg.ResultPath)
	if err != nil {
		return nil, fmt.Errorf("apifetch: walk path %q: %w", cfg.ResultPath, err)
	}

	// Extract fields from each item.
	results := make([]Result, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		results = append(results, extractFields(obj, cfg.Fields))
	}

	return results, nil
}

// walkPath walks a dot-notation path into a JSON value, returning the items
// found at that path. If the path is empty, the root must be an array.
func walkPath(v any, path string) ([]any, error) {
	if path == "" {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("root is not an array")
		}
		return arr, nil
	}

	parts := strings.Split(path, ".")
	current := v
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected object at %q, got %T", part, current)
		}
		current, ok = obj[part]
		if !ok {
			return nil, fmt.Errorf("key %q not found", part)
		}
	}

	arr, ok := current.([]any)
	if !ok {
		return nil, fmt.Errorf("path %q is not an array", path)
	}
	return arr, nil
}

// extractFields maps configured field names to Result.
func extractFields(obj map[string]any, fields map[string]string) Result {
	var r Result
	if fields == nil {
		// Default mapping.
		r.Title = asString(obj["title"])
		r.Text = asString(obj["text"])
		r.URL = asString(obj["url"])
		return r
	}
	if f, ok := fields["title"]; ok {
		r.Title = asString(obj[f])
	}
	if f, ok := fields["text"]; ok {
		r.Text = asString(obj[f])
	}
	if f, ok := fields["url"]; ok {
		r.URL = asString(obj[f])
	}
	return r
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// expandEnv replaces ${ENV_VAR} patterns with their values.
func expandEnv(s string) string {
	return os.Expand(s, os.Getenv)
}
