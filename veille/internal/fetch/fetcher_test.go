package fetch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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

	f := New(Config{})
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

	f := New(Config{})
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

	f := New(Config{})
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

	f := New(Config{Timeout: 100 * time.Millisecond})
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

	f := New(Config{MaxBytes: 100})
	result, err := f.Fetch(context.Background(), srv.URL, "", "", "")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(result.Body) > 100 {
		t.Errorf("body too large: %d bytes, max 100", len(result.Body))
	}
}
