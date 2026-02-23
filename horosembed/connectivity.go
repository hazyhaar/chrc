package horosembed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hazyhaar/pkg/connectivity"
)

// RegisterConnectivity registers horosembed service handlers on a connectivity Router.
//
// Registered services:
//
//	horosembed_embed — embed a single text
//	horosembed_batch — embed multiple texts
func RegisterConnectivity(router *connectivity.Router, emb Embedder) {
	router.RegisterLocal("horosembed_embed", handleEmbed(emb))
	router.RegisterLocal("horosembed_batch", handleBatch(emb))
}

func handleEmbed(emb Embedder) connectivity.Handler {
	return func(ctx context.Context, payload []byte) ([]byte, error) {
		var req struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		vec, err := emb.Embed(ctx, req.Text)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"vector":    vec,
			"dimension": len(vec),
			"model":     emb.Model(),
		})
	}
}

func handleBatch(emb Embedder) connectivity.Handler {
	return func(ctx context.Context, payload []byte) ([]byte, error) {
		var req struct {
			Texts []string `json:"texts"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		vecs, err := emb.EmbedBatch(ctx, req.Texts)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"vectors":   vecs,
			"count":     len(vecs),
			"dimension": emb.Dimension(),
			"model":     emb.Model(),
		})
	}
}
