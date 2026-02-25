package veille

import (
	"context"
	"errors"
	"testing"

	"github.com/hazyhaar/pkg/horosafe"

	_ "modernc.org/sqlite"
)

func TestSSRF_AddSource_RejectsPrivateURLs(t *testing.T) {
	// WHAT: AddSource rejects URLs targeting private/loopback IPs.
	// WHY: Without SSRF validation, an attacker can probe internal services via source URLs.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		url  string
	}{
		{"loopback", "http://127.0.0.1:6379"},
		{"loopback_localhost", "http://localhost:8080"},
		{"private_10", "http://10.0.0.1/admin"},
		{"private_172", "http://172.16.0.1/secret"},
		{"private_192", "http://192.168.1.1/router"},
		{"metadata_aws", "http://169.254.169.254/latest/meta-data/"},
		{"file_scheme", "file:///etc/passwd"},
		{"ftp_scheme", "ftp://internal.server/data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := &Source{Name: "Evil", URL: tc.url, SourceType: "web", Enabled: true}
			err := svc.AddSource(ctx, "d1", src)
			if err == nil {
				t.Errorf("AddSource should reject %q but accepted it", tc.url)
			}
			if err != nil && !errors.Is(err, horosafe.ErrSSRF) && !errors.Is(err, horosafe.ErrUnsafeScheme) {
				t.Errorf("expected SSRF or UnsafeScheme error, got: %v", err)
			}
		})
	}
}

func TestSSRF_AddSource_AcceptsPublicURLs(t *testing.T) {
	// WHAT: AddSource accepts valid public URLs.
	// WHY: SSRF validation must not block legitimate sources.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		url  string
	}{
		{"https", "https://example.com/feed.xml"},
		{"http", "http://example.com/api"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := &Source{Name: tc.name, URL: tc.url, SourceType: "rss", Enabled: true}
			if err := svc.AddSource(ctx, "d1", src); err != nil {
				t.Errorf("AddSource should accept %q, got: %v", tc.url, err)
			}
		})
	}
}

func TestSSRF_UpdateSource_RejectsPrivateURLs(t *testing.T) {
	// WHAT: UpdateSource rejects URLs targeting private IPs.
	// WHY: An attacker could bypass AddSource validation by updating an existing source's URL.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	// Add a valid source first.
	src := &Source{Name: "Good", URL: "https://example.com", SourceType: "web", Enabled: true}
	if err := svc.AddSource(ctx, "d1", src); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Try to update with a private URL.
	src.URL = "http://127.0.0.1:6379"
	err := svc.UpdateSource(ctx, "d1", src)
	if err == nil {
		t.Error("UpdateSource should reject private URL")
	}
}

func TestSSRF_AddSource_SkipsValidationForInternalSchemes(t *testing.T) {
	// WHAT: Internal source types like "question" use question:// scheme and must not be SSRF-checked.
	// WHY: The question pipeline uses synthetic URLs (question://id) that are never fetched via HTTP.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	src := &Source{Name: "Question", URL: "question://abc123", SourceType: "question", Enabled: true}
	if err := svc.AddSource(ctx, "d1", src); err != nil {
		t.Errorf("AddSource should accept question:// URLs for question type, got: %v", err)
	}
}

func TestDocument_PathTraversal_Rejected(t *testing.T) {
	// WHAT: Document sources with path traversal (../../) are rejected at AddSource.
	// WHY: Without path traversal guard, an attacker can read arbitrary files via document sources.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		url  string
	}{
		{"parent_dir", "../../../etc/passwd"},
		{"double_dot", "../../secret/file.txt"},
		{"encoded_dots", "..%2F..%2Fetc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := &Source{Name: "Evil Doc", URL: tc.url, SourceType: "document", Enabled: true}
			err := svc.AddSource(ctx, "d1", src)
			if err == nil {
				t.Errorf("AddSource(document) should reject %q but accepted it", tc.url)
			}
		})
	}
}

func TestDocument_ValidPath_Accepted(t *testing.T) {
	// WHAT: Document sources with valid relative paths are accepted.
	// WHY: Validation must not break legitimate document sources.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	src := &Source{Name: "Valid Doc", URL: "reports/q1.pdf", SourceType: "document", Enabled: true}
	if err := svc.AddSource(ctx, "d1", src); err != nil {
		t.Errorf("AddSource(document) should accept valid path, got: %v", err)
	}
}
