package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hazyhaar/pkg/shield"
)

func TestShield_SecurityHeaders(t *testing.T) {
	// WHAT: Responses contain security headers from shield.DefaultBOStack.
	// WHY: Without shield, no CSP, X-Frame-Options, X-Content-Type-Options, or X-Trace-ID.
	r := chi.NewRouter()
	for _, mw := range shield.DefaultBOStack() {
		r.Use(mw)
	}
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	checks := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
	}
	for header, expected := range checks {
		got := w.Header().Get(header)
		if got != expected {
			t.Errorf("%s: got %q, want %q", header, got, expected)
		}
	}

	// TraceID should be present (8 hex chars).
	traceID := w.Header().Get("X-Trace-ID")
	if traceID == "" {
		t.Error("X-Trace-ID header missing")
	}
	if len(traceID) != 8 {
		t.Errorf("X-Trace-ID: got %q (len %d), want 8 hex chars", traceID, len(traceID))
	}
}
