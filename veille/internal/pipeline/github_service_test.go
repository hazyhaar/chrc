package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/store"
	"github.com/hazyhaar/pkg/connectivity"
)

// --- Unit tests: GitHub service (connectivity.Handler) ---

func TestGitHubService_Commits(t *testing.T) {
	// WHAT: Parse commits API response → bridgeResponse.
	// WHY: Commits are the default resource — must map sha→hash, message→title+content.

	apiResponse := `[
		{
			"sha": "abc123",
			"html_url": "https://github.com/owner/repo/commit/abc123",
			"commit": {"message": "feat: add new feature\n\nDetailed description of the change."}
		},
		{
			"sha": "def456",
			"html_url": "https://github.com/owner/repo/commit/def456",
			"commit": {"message": "fix: resolve bug in parser"}
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/commits" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(apiResponse))
	}))
	defer srv.Close()

	handler := NewGitHubService(srv.URL)

	req := bridgeRequest{
		SourceID:   "src-gh-1",
		URL:        "https://github.com/owner/repo",
		SourceType: "github",
	}
	payload, _ := json.Marshal(req)

	respData, err := handler(context.Background(), payload)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var resp bridgeResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Extractions) != 2 {
		t.Fatalf("extractions: got %d, want 2", len(resp.Extractions))
	}

	// First commit: title should be first line only.
	ext := resp.Extractions[0]
	if ext.Title != "feat: add new feature" {
		t.Errorf("title: got %q", ext.Title)
	}
	if !strings.Contains(ext.Content, "Detailed description") {
		t.Errorf("content should contain full message, got %q", ext.Content)
	}
	if ext.URL != "https://github.com/owner/repo/commit/abc123" {
		t.Errorf("url: got %q", ext.URL)
	}
	if ext.ContentHash == "" {
		t.Error("content_hash should not be empty")
	}
}

func TestGitHubService_Issues(t *testing.T) {
	// WHAT: Parse issues API response → bridgeResponse with title+body+labels.
	// WHY: Issues have richer text (title + labels + body).

	apiResponse := `[
		{
			"number": 42,
			"title": "Bug: crash on startup",
			"body": "The app crashes when launched with --debug flag.",
			"html_url": "https://github.com/owner/repo/issues/42",
			"labels": [{"name": "bug"}, {"name": "critical"}]
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(apiResponse))
	}))
	defer srv.Close()

	handler := NewGitHubService(srv.URL)

	req := bridgeRequest{
		SourceID:   "src-gh-2",
		URL:        "https://github.com/owner/repo/issues",
		Config:     json.RawMessage(`{"resource":"issues"}`),
		SourceType: "github",
	}
	payload, _ := json.Marshal(req)

	respData, err := handler(context.Background(), payload)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var resp bridgeResponse
	json.Unmarshal(respData, &resp)

	if len(resp.Extractions) != 1 {
		t.Fatalf("extractions: got %d, want 1", len(resp.Extractions))
	}

	ext := resp.Extractions[0]
	if ext.Title != "Bug: crash on startup" {
		t.Errorf("title: got %q", ext.Title)
	}
	if !strings.Contains(ext.Content, "bug") || !strings.Contains(ext.Content, "critical") {
		t.Errorf("content should contain labels, got %q", ext.Content)
	}
	if !strings.Contains(ext.Content, "crashes when launched") {
		t.Errorf("content should contain body, got %q", ext.Content)
	}
}

func TestGitHubService_Releases(t *testing.T) {
	// WHAT: Parse releases API response → bridgeResponse with tag+name+body.
	// WHY: Releases have tag_name, name, and body.

	apiResponse := `[
		{
			"id": 12345,
			"tag_name": "v1.2.0",
			"name": "Release 1.2.0",
			"body": "## Changelog\n- Added feature X\n- Fixed bug Y",
			"html_url": "https://github.com/owner/repo/releases/tag/v1.2.0"
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(apiResponse))
	}))
	defer srv.Close()

	handler := NewGitHubService(srv.URL)

	req := bridgeRequest{
		SourceID:   "src-gh-3",
		URL:        "https://github.com/owner/repo/releases",
		Config:     json.RawMessage(`{"resource":"releases"}`),
		SourceType: "github",
	}
	payload, _ := json.Marshal(req)

	respData, err := handler(context.Background(), payload)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var resp bridgeResponse
	json.Unmarshal(respData, &resp)

	if len(resp.Extractions) != 1 {
		t.Fatalf("extractions: got %d, want 1", len(resp.Extractions))
	}

	ext := resp.Extractions[0]
	if ext.Title != "Release 1.2.0" {
		t.Errorf("title: got %q", ext.Title)
	}
	if !strings.Contains(ext.Content, "Changelog") {
		t.Errorf("content should contain body, got %q", ext.Content)
	}
	if !strings.Contains(ext.Content, "v1.2.0") {
		t.Errorf("content should contain tag, got %q", ext.Content)
	}
}

func TestGitHubService_InvalidURL(t *testing.T) {
	// WHAT: Invalid GitHub URL returns an error.
	// WHY: Bad URLs must fail early, not silently produce no results.

	handler := NewGitHubService("") // base URL doesn't matter — URL parse should fail first.

	req := bridgeRequest{
		SourceID:   "src-bad",
		URL:        "https://example.com/not-github",
		SourceType: "github",
	}
	payload, _ := json.Marshal(req)

	_, err := handler(context.Background(), payload)
	if err == nil {
		t.Fatal("expected error for invalid GitHub URL")
	}
	if !strings.Contains(err.Error(), "cannot parse") {
		t.Errorf("error should mention parse failure: %v", err)
	}
}

func TestGitHubService_ParseGitHubURL(t *testing.T) {
	// WHAT: parseGitHubURL extracts owner/repo/resource from various URL formats.
	// WHY: URL parsing is the foundation — all handlers depend on it.

	cases := []struct {
		url      string
		owner    string
		repo     string
		resource string
	}{
		{"https://github.com/owner/repo", "owner", "repo", ""},
		{"https://github.com/owner/repo/", "owner", "repo", ""},
		{"https://github.com/owner/repo/issues", "owner", "repo", "issues"},
		{"https://github.com/owner/repo/pulls", "owner", "repo", "pulls"},
		{"https://github.com/owner/repo/releases", "owner", "repo", "releases"},
		{"https://github.com/owner/repo/commits", "owner", "repo", "commits"},
		{"http://github.com/owner/repo", "owner", "repo", ""},
		{"github.com/owner/repo", "owner", "repo", ""},
		{"https://example.com/foo", "", "", ""},
		{"foo", "", "", ""},
	}

	for _, tc := range cases {
		owner, repo, resource := parseGitHubURL(tc.url)
		if owner != tc.owner || repo != tc.repo || resource != tc.resource {
			t.Errorf("parseGitHubURL(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.url, owner, repo, resource, tc.owner, tc.repo, tc.resource)
		}
	}
}

// --- Bridge integration tests: ConnectivityBridge + github_fetch ---

func TestGitHubBridge_Pipeline(t *testing.T) {
	// WHAT: Pipeline dispatches github → bridge → service → extractions stored.
	// WHY: The full flow must work via connectivity, not just the service in isolation.

	apiResponse := `[
		{
			"sha": "aaa111",
			"html_url": "https://github.com/test/repo/commit/aaa111",
			"commit": {"message": "Initial commit with project setup and configuration."}
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(apiResponse))
	}))
	defer srv.Close()

	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	router := connectivity.New()
	router.RegisterLocal("github_fetch", NewGitHubService(srv.URL))

	s.InsertSource(ctx, &store.Source{
		ID: "src-ghb", Name: "GitHub Test", URL: "https://github.com/test/repo",
		SourceType: "github", Enabled: true,
	})
	src, _ := s.GetSource(ctx, "src-ghb")

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	bridge := NewConnectivityBridge(router, "github_fetch", "github")
	p.currentJob = &Job{DossierID: "u1_s1", SourceID: "src-ghb", URL: src.URL}

	err := bridge.Handle(ctx, s, src, p)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	exts, _ := s.ListExtractions(ctx, "src-ghb", 10)
	if len(exts) != 1 {
		t.Fatalf("extractions: got %d, want 1", len(exts))
	}
	if exts[0].Title != "Initial commit with project setup and configuration." {
		t.Errorf("title: got %q", exts[0].Title)
	}
}

func TestGitHubBridge_Dedup(t *testing.T) {
	// WHAT: Second fetch with same hashes produces 0 new extractions.
	// WHY: Dedup prevents duplicates — same pattern as other handlers.

	apiResponse := `[
		{
			"sha": "stable-sha",
			"html_url": "https://github.com/test/repo/commit/stable-sha",
			"commit": {"message": "Stable commit that does not change between fetches."}
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(apiResponse))
	}))
	defer srv.Close()

	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	router := connectivity.New()
	router.RegisterLocal("github_fetch", NewGitHubService(srv.URL))

	s.InsertSource(ctx, &store.Source{
		ID: "src-ghd", Name: "Dedup Test", URL: "https://github.com/test/repo",
		SourceType: "github", Enabled: true,
	})
	src, _ := s.GetSource(ctx, "src-ghd")

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	bridge := NewConnectivityBridge(router, "github_fetch", "github")
	p.currentJob = &Job{DossierID: "u1_s1", SourceID: "src-ghd"}

	// First call.
	bridge.Handle(ctx, s, src, p)
	exts1, _ := s.ListExtractions(ctx, "src-ghd", 10)
	if len(exts1) != 1 {
		t.Fatalf("first call: got %d extractions, want 1", len(exts1))
	}

	// Second call — same sha.
	bridge.Handle(ctx, s, src, p)
	exts2, _ := s.ListExtractions(ctx, "src-ghd", 10)
	if len(exts2) != 1 {
		t.Errorf("second call: got %d extractions, want 1 (dedup)", len(exts2))
	}
}

func TestGitHubBridge_Buffer(t *testing.T) {
	// WHAT: Handler writes .md files to the buffer directory.
	// WHY: RAG island consumes the buffer — github must produce .md like other handlers.

	apiResponse := `[
		{
			"sha": "buf123",
			"html_url": "https://github.com/test/repo/commit/buf123",
			"commit": {"message": "Buffer test commit with enough content for extraction and RAG consumption."}
		}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(apiResponse))
	}))
	defer srv.Close()

	s, cleanup := setupTest(t)
	defer cleanup()
	ctx := context.Background()

	router := connectivity.New()
	router.RegisterLocal("github_fetch", NewGitHubService(srv.URL))

	s.InsertSource(ctx, &store.Source{
		ID: "src-ghbuf", Name: "Buffer Test", URL: "https://github.com/test/repo",
		SourceType: "github", Enabled: true,
	})
	src, _ := s.GetSource(ctx, "src-ghbuf")

	bufDir := filepath.Join(t.TempDir(), "pending")

	f := fetch.New(fetch.Config{})
	p := New(f, nil)
	p.SetBuffer(buffer.NewWriter(bufDir))

	bridge := NewConnectivityBridge(router, "github_fetch", "github")
	p.currentJob = &Job{DossierID: "u1_s1", SourceID: "src-ghbuf", URL: src.URL}

	err := bridge.Handle(ctx, s, src, p)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	// Verify .md file was created.
	entries, err := os.ReadDir(bufDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no .md files in buffer pending dir")
	}

	// Read the .md and verify frontmatter.
	data, _ := os.ReadFile(filepath.Join(bufDir, entries[0].Name()))
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		t.Error("file should start with frontmatter ---")
	}
	if !strings.Contains(content, "source_type: github") {
		t.Error("frontmatter missing source_type: github")
	}
}

func TestGitHubBridge_Discovery(t *testing.T) {
	// WHAT: DiscoverHandlers discovers github_fetch → registers handler "github".
	// WHY: Auto-discovery is the wiring mechanism — github must be found like any other.

	router := connectivity.New()
	router.RegisterLocal("github_fetch", func(_ context.Context, _ []byte) ([]byte, error) {
		return []byte(`{"extractions":[]}`), nil
	})

	f := fetch.New(fetch.Config{})
	p := New(f, nil)

	// Remove built-in github handler so discovery can register.
	delete(p.handlers, "github")

	DiscoverHandlers(p, router)

	if _, ok := p.handlers["github"]; !ok {
		t.Fatal("github handler not registered via discovery")
	}

	// Verify it's a ConnectivityBridge, not the old GitHubHandler.
	if _, ok := p.handlers["github"].(*ConnectivityBridge); !ok {
		t.Error("github handler should be a ConnectivityBridge after discovery")
	}
}
