package vecbridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/hazyhaar/pkg/connectivity"
)

// RegisterConnectivity registers vecbridge service handlers on a connectivity Router.
//
// Registered services:
//
//	horosvec_search — ANN search by query vector
//	horosvec_insert — insert vectors into the index
//	horosvec_stats  — index statistics
func (s *Service) RegisterConnectivity(router *connectivity.Router) {
	router.RegisterLocal("horosvec_search", s.handleSearch)
	router.RegisterLocal("horosvec_insert", s.handleInsert)
	router.RegisterLocal("horosvec_stats", s.handleStats)
}

func (s *Service) handleSearch(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		Vector []float32 `json:"vector"`
		TopK   int       `json:"top_k"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if req.TopK <= 0 {
		req.TopK = 10
	}

	results, err := s.Index.Search(req.Vector, req.TopK)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]any, len(results))
	for i, res := range results {
		out[i] = map[string]any{
			"id":    hex.EncodeToString(res.ID),
			"score": res.Score,
		}
	}
	return json.Marshal(map[string]any{"results": out})
}

func (s *Service) handleInsert(_ context.Context, payload []byte) ([]byte, error) {
	var req struct {
		IDs     []string    `json:"ids"`
		Vectors [][]float32 `json:"vectors"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	ids := make([][]byte, len(req.IDs))
	for i, id := range req.IDs {
		b, err := hex.DecodeString(id)
		if err != nil {
			ids[i] = []byte(id)
		} else {
			ids[i] = b
		}
	}

	if err := s.Index.Insert(req.Vectors, ids); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"inserted": len(req.Vectors), "count": s.Index.Count()})
}

func (s *Service) handleStats(_ context.Context, _ []byte) ([]byte, error) {
	return json.Marshal(map[string]any{
		"count":         s.Index.Count(),
		"needs_rebuild": s.Index.NeedsRebuild(),
	})
}
