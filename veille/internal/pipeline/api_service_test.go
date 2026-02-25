package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIService_FetchAndBridge(t *testing.T) {
	// WHAT: NewAPIService returns a connectivity.Handler that fetches JSON API and returns bridgeResponse.
	// WHY: API handler must conform to the connectivity bridge protocol.

	// Mock API server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]string{
				{"title": "Item 1", "url": "https://example.com/1", "text": "First item content"},
				{"title": "Item 2", "url": "https://example.com/2", "text": "Second item content"},
			},
		})
	}))
	defer srv.Close()

	handler := NewAPIService()

	// Build bridge request with API config.
	cfg := map[string]string{
		"result_path": "results",
		"title_field": "title",
		"url_field":   "url",
		"text_field":  "text",
	}
	cfgJSON, _ := json.Marshal(cfg)

	req := bridgeRequest{
		SourceID:   "src-test",
		URL:        srv.URL,
		Config:     cfgJSON,
		SourceType: "api",
	}
	reqJSON, _ := json.Marshal(req)

	respJSON, err := handler(context.Background(), reqJSON)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var resp bridgeResponse
	if err := json.Unmarshal(respJSON, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Extractions) != 2 {
		t.Fatalf("expected 2 extractions, got %d", len(resp.Extractions))
	}

	if resp.Extractions[0].Title != "Item 1" {
		t.Errorf("title: got %q, want %q", resp.Extractions[0].Title, "Item 1")
	}
	if resp.Extractions[0].ContentHash == "" {
		t.Error("content_hash should not be empty")
	}
}
