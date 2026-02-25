// CLAUDE:SUMMARY GitHub connectivity.Handler — fetches GitHub API and returns bridgeResponse.
// CLAUDE:DEPENDS hazyhaar/pkg/connectivity, handler_connectivity.go
// CLAUDE:EXPORTS NewGitHubService
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hazyhaar/pkg/connectivity"
)

// NewGitHubService returns a connectivity.Handler for the "github_fetch" service.
// apiBaseURL overrides the GitHub API base URL (for testing). Empty string uses production.
//
// The handler receives a bridgeRequest (source_id, url, config, source_type),
// parses the GitHub URL, calls the GitHub REST API, and returns a bridgeResponse
// with extractions. The ConnectivityBridge handles dedup, store, and buffer.
func NewGitHubService(apiBaseURL string) connectivity.Handler {
	if apiBaseURL == "" {
		apiBaseURL = "https://api.github.com"
	}
	client := &http.Client{Timeout: 30 * time.Second}

	return func(ctx context.Context, payload []byte) ([]byte, error) {
		var req bridgeRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("github_fetch: unmarshal request: %w", err)
		}

		owner, repo, resource := parseGitHubURL(req.URL)
		if owner == "" || repo == "" {
			return nil, fmt.Errorf("github_fetch: cannot parse URL %q (expected github.com/owner/repo)", req.URL)
		}

		// Parse config.
		var cfg githubConfig
		if len(req.Config) > 0 && string(req.Config) != "{}" {
			json.Unmarshal(req.Config, &cfg)
		}
		if cfg.Resource != "" {
			resource = cfg.Resource
		}
		if resource == "" {
			resource = "commits"
		}
		if cfg.PerPage <= 0 {
			cfg.PerPage = 30
		}
		if cfg.State == "" {
			cfg.State = "open"
		}

		// Build API URL (using injected base for testability).
		apiURL := buildGitHubAPIURLWithBase(apiBaseURL, owner, repo, resource, cfg)

		// Fetch from GitHub API.
		body, err := fetchGitHubAPI(ctx, client, apiURL)
		if err != nil {
			return nil, fmt.Errorf("github_fetch: %w", err)
		}

		// Parse items.
		items, err := parseGitHubItems(body, resource)
		if err != nil {
			return nil, fmt.Errorf("github_fetch: parse: %w", err)
		}

		// Map items to bridge extractions.
		extractions := make([]bridgeExtraction, 0, len(items))
		for _, item := range items {
			extractions = append(extractions, bridgeExtraction{
				Title:       item.Title,
				Content:     item.Body,
				URL:         item.URL,
				ContentHash: ghHash(item.Hash),
			})
		}

		resp := bridgeResponse{Extractions: extractions}
		return json.Marshal(resp)
	}
}

// buildGitHubAPIURLWithBase builds the REST API URL using a configurable base.
func buildGitHubAPIURLWithBase(baseURL, owner, repo, resource string, cfg githubConfig) string {
	base := fmt.Sprintf("%s/repos/%s/%s", baseURL, owner, repo)
	perPage := cfg.PerPage

	switch resource {
	case "issues":
		return fmt.Sprintf("%s/issues?state=%s&per_page=%d&sort=updated&direction=desc", base, cfg.State, perPage)
	case "pulls":
		return fmt.Sprintf("%s/pulls?state=%s&per_page=%d&sort=updated&direction=desc", base, cfg.State, perPage)
	case "releases":
		return fmt.Sprintf("%s/releases?per_page=%d", base, perPage)
	default: // commits
		return fmt.Sprintf("%s/commits?per_page=%d", base, perPage)
	}
}

// fetchGitHubAPI calls the GitHub REST API with token auth.
func fetchGitHubAPI(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
}

// parseGitHubURL extracts owner, repo, and resource from a GitHub URL.
func parseGitHubURL(rawURL string) (owner, repo, resource string) {
	u := rawURL
	matched := false
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		if strings.HasPrefix(u, prefix) {
			u = strings.TrimPrefix(u, prefix)
			matched = true
			break
		}
	}
	if !matched {
		return "", "", ""
	}
	u = strings.TrimRight(u, "/")

	parts := strings.SplitN(u, "/", 4)
	if len(parts) < 2 {
		return "", "", ""
	}
	owner = parts[0]
	repo = parts[1]
	if len(parts) >= 3 {
		resource = parts[2]
	}
	return owner, repo, resource
}

// parseGitHubItems extracts items from the GitHub API JSON response.
func parseGitHubItems(body []byte, resource string) ([]githubItem, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("expected JSON array: %w", err)
	}

	items := make([]githubItem, 0, len(raw))
	for _, r := range raw {
		var obj map[string]any
		if err := json.Unmarshal(r, &obj); err != nil {
			continue
		}

		var item githubItem
		switch resource {
		case "issues", "pulls":
			item = parseIssuePR(obj)
		case "releases":
			item = parseRelease(obj)
		default:
			item = parseCommit(obj)
		}
		if item.Hash != "" {
			items = append(items, item)
		}
	}
	return items, nil
}

func parseCommit(obj map[string]any) githubItem {
	sha := asStr(obj["sha"])
	htmlURL := asStr(obj["html_url"])
	var msg string
	if commit, ok := obj["commit"].(map[string]any); ok {
		msg = asStr(commit["message"])
	}
	title := msg
	if i := strings.IndexByte(title, '\n'); i > 0 {
		title = title[:i]
	}
	return githubItem{
		Title: title,
		Body:  msg,
		URL:   htmlURL,
		Hash:  sha,
	}
}

func parseIssuePR(obj map[string]any) githubItem {
	number := obj["number"]
	title := asStr(obj["title"])
	body := asStr(obj["body"])
	htmlURL := asStr(obj["html_url"])

	var labels []string
	if arr, ok := obj["labels"].([]any); ok {
		for _, l := range arr {
			if lm, ok := l.(map[string]any); ok {
				labels = append(labels, asStr(lm["name"]))
			}
		}
	}
	var text strings.Builder
	text.WriteString(title)
	if len(labels) > 0 {
		text.WriteString("\nLabels: ")
		text.WriteString(strings.Join(labels, ", "))
	}
	if body != "" {
		text.WriteString("\n\n")
		text.WriteString(body)
	}

	return githubItem{
		Title: title,
		Body:  text.String(),
		URL:   htmlURL,
		Hash:  fmt.Sprintf("%v", number),
	}
}

func parseRelease(obj map[string]any) githubItem {
	tagName := asStr(obj["tag_name"])
	name := asStr(obj["name"])
	body := asStr(obj["body"])
	htmlURL := asStr(obj["html_url"])
	id := obj["id"]

	title := name
	if title == "" {
		title = tagName
	}

	var text strings.Builder
	text.WriteString(title)
	if tagName != "" && tagName != title {
		text.WriteString(" (")
		text.WriteString(tagName)
		text.WriteString(")")
	}
	if body != "" {
		text.WriteString("\n\n")
		text.WriteString(body)
	}

	return githubItem{
		Title: title,
		Body:  text.String(),
		URL:   htmlURL,
		Hash:  fmt.Sprintf("%v", id),
	}
}

func asStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func ghHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// githubConfig is parsed from source.config_json (all optional).
type githubConfig struct {
	Resource string `json:"resource"`
	PerPage  int    `json:"per_page"`
	State    string `json:"state"`
}

// githubItem is one item from the GitHub API response.
type githubItem struct {
	Title string
	Body  string
	URL   string
	Hash  string
}
