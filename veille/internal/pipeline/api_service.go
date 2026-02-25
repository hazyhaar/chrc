// CLAUDE:SUMMARY API connectivity.Handler — fetches JSON API via apifetch and returns bridgeResponse.
// CLAUDE:DEPENDS hazyhaar/pkg/connectivity, apifetch
// CLAUDE:EXPORTS NewAPIService
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/apifetch"
	"github.com/hazyhaar/pkg/connectivity"
)

// NewAPIService returns a connectivity.Handler for the "api_fetch" service.
// It wraps the apifetch package in the connectivity bridge protocol.
//
// The handler receives a bridgeRequest (source_id, url, config, source_type),
// calls the API via apifetch.Fetch, and returns a bridgeResponse with extractions.
// The ConnectivityBridge handles dedup, store, and buffer.
func NewAPIService() connectivity.Handler {
	client := &http.Client{Timeout: 30 * time.Second}

	return func(ctx context.Context, payload []byte) ([]byte, error) {
		var req bridgeRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("api_fetch: unmarshal request: %w", err)
		}

		// Parse API config from the source config.
		var cfg apifetch.Config
		if len(req.Config) > 0 && string(req.Config) != "{}" {
			if err := json.Unmarshal(req.Config, &cfg); err != nil {
				return nil, fmt.Errorf("api_fetch: config parse: %w", err)
			}
		}

		results, err := apifetch.Fetch(ctx, client, req.URL, cfg)
		if err != nil {
			return nil, fmt.Errorf("api_fetch: %w", err)
		}

		var extractions []bridgeExtraction
		for _, r := range results {
			url := r.URL
			if url == "" {
				url = req.URL
			}
			extractions = append(extractions, bridgeExtraction{
				Title:       r.Title,
				Content:     r.Text,
				URL:         url,
				ContentHash: hashString(r.URL + "|" + r.Title),
			})
		}

		resp := bridgeResponse{Extractions: extractions}
		return json.Marshal(resp)
	}
}
