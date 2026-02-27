package fetch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// noopValidator allows all URLs (for tests that don't test SSRF).
func noopValidator(_ string) error { return nil }

func TestFetch_Success(t *testing.T) {
	// WHAT: Basic HTTP GET returns body and hash.
	// WHY: Core fetcher functionality.
	body := "Hello, World!"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	f := New(Config{URLValidator: noopValidator})
	result, err := f.Fetch(context.Background(), srv.URL, "", "", "")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.StatusCode != 200 {
		t.Errorf("status: got %d", result.StatusCode)
	}
	if string(result.Body) != body {
		t.Errorf("body: got %q", string(result.Body))
	}
	if result.ETag != `"abc123"` {
		t.Errorf("etag: got %q", result.ETag)
	}
	if !result.Changed {
		t.Error("should be changed (no previous hash)")
	}
	h := sha256.Sum256([]byte(body))
	want := fmt.Sprintf("%x", h)
	if result.Hash != want {
		t.Errorf("hash: got %q, want %q", result.Hash, want)
	}
}

func TestFetch_304NotModified(t *testing.T) {
	// WHAT: Conditional GET returns 304 when ETag matches.
	// WHY: Avoids unnecessary re-processing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"abc123"` {
			w.WriteHeader(304)
			return
		}
		w.Write([]byte("body"))
	}))
	defer srv.Close()

	f := New(Config{URLValidator: noopValidator})
	result, err := f.Fetch(context.Background(), srv.URL, `"abc123"`, "", "")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.StatusCode != 304 {
		t.Errorf("status: got %d, want 304", result.StatusCode)
	}
	if result.Changed {
		t.Error("304 should mean not changed")
	}
}

func TestFetch_UnchangedHash(t *testing.T) {
	// WHAT: Same content hash means Changed=false.
	// WHY: Some servers don't support ETag; hash-based dedup is the fallback.
	body := "same content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	h := sha256.Sum256([]byte(body))
	prevHash := fmt.Sprintf("%x", h)

	f := New(Config{URLValidator: noopValidator})
	result, err := f.Fetch(context.Background(), srv.URL, "", "", prevHash)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.Changed {
		t.Error("same hash should mean unchanged")
	}
}

func TestFetch_Timeout(t *testing.T) {
	// WHAT: Fetch respects context deadline.
	// WHY: Sources must not block the pipeline indefinitely.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("late"))
	}))
	defer srv.Close()

	f := New(Config{Timeout: 100 * time.Millisecond, URLValidator: noopValidator})
	_, err := f.Fetch(context.Background(), srv.URL, "", "", "")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestFetch_MaxBody(t *testing.T) {
	// WHAT: Body is truncated to MaxBytes.
	// WHY: Prevents memory exhaustion from large responses.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 1000; i++ {
			w.Write([]byte("x"))
		}
	}))
	defer srv.Close()

	f := New(Config{MaxBytes: 100, URLValidator: noopValidator})
	result, err := f.Fetch(context.Background(), srv.URL, "", "", "")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(result.Body) > 100 {
		t.Errorf("body too large: %d bytes, max 100", len(result.Body))
	}
}

// --- SSRF protection tests ---

func TestFetch_ValidateURL_PrivateIP(t *testing.T) {
	// WHAT: Private IP URLs are blocked before request.
	// WHY: SSRF prevention — no access to internal network.
	f := New(Config{})
	_, err := f.Fetch(context.Background(), "http://192.168.1.1/data", "", "", "")
	if err == nil {
		t.Fatal("expected error for private IP URL")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("expected SSRF error, got: %v", err)
	}
}

func TestFetch_ValidateURL_Metadata(t *testing.T) {
	// WHAT: Cloud metadata endpoint URLs are blocked.
	// WHY: 169.254.169.254 is the AWS/GCP/Azure metadata service.
	f := New(Config{})
	_, err := f.Fetch(context.Background(), "http://169.254.169.254/latest/", "", "", "")
	if err == nil {
		t.Fatal("expected error for metadata endpoint URL")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("expected SSRF error, got: %v", err)
	}
}

func TestFetch_RedirectToPrivate(t *testing.T) {
	// WHAT: Redirect to private IP is blocked by CheckRedirect.
	// WHY: Open redirect → SSRF is a common attack chain.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.255.255.1/admin", http.StatusFound)
	}))
	defer srv.Close()

	// allowFirst allows the first URL (httptest loopback) but blocks private IPs on redirect.
	first := true
	allowFirst := func(u string) error {
		if first {
			first = false
			return nil
		}
		return fmt.Errorf("SSRF: private IP blocked")
	}

	f := New(Config{URLValidator: allowFirst})
	_, err := f.Fetch(context.Background(), srv.URL, "", "", "")
	if err == nil {
		t.Fatal("expected error for redirect to private IP")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("expected SSRF in error, got: %v", err)
	}
}

func TestFetch_TooManyRedirects(t *testing.T) {
	// WHAT: More than 5 redirects are blocked.
	// WHY: Redirect loop protection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String()+"x", http.StatusFound)
	}))
	defer srv.Close()

	f := New(Config{URLValidator: noopValidator})
	_, err := f.Fetch(context.Background(), srv.URL+"/start", "", "", "")
	if err == nil {
		t.Fatal("expected error for too many redirects")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("expected redirect error, got: %v", err)
	}
}
