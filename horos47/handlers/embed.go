package handlers

import (
	"context"
	"fmt"
	"time"

	"horos47/storage"
)

// HandleEmbedChunks generates embeddings for unembedded chunks via GPU Feeder.
// Processes up to 32 chunks per invocation.
func (h *Handlers) HandleEmbedChunks(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("embed_chunks: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)

	if h.GPUSubmitter == nil {
		result := map[string]interface{}{"status": "gpu_unavailable", "handler": "embed_chunks"}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("embed_chunks: submit next step: %w", err)
		}
		return result, nil
	}

	// Find chunks without embeddings
	rows, err := h.DB.QueryContext(ctx, `
		SELECT c.chunk_id, c.document_id, c.chunk_text
		FROM chunks c
		WHERE c.chunk_id NOT IN (SELECT chunk_id FROM embeddings)
		LIMIT 32
	`)
	if err != nil {
		h.GW.FailEnvelope(envelopeID, err.Error())
		return nil, fmt.Errorf("embed_chunks: query chunks: %w", err)
	}
	defer rows.Close()

	var chunkIDs, docIDs [][]byte
	var texts []string
	for rows.Next() {
		var chunkID, docID []byte
		var text string
		if err := rows.Scan(&chunkID, &docID, &text); err != nil {
			continue
		}
		chunkIDs = append(chunkIDs, chunkID)
		docIDs = append(docIDs, docID)
		texts = append(texts, text)
	}

	if len(texts) == 0 {
		result := map[string]interface{}{
			"status":  "no_chunks",
			"handler": "embed_chunks",
		}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("embed_chunks: submit next step: %w", err)
		}
		return result, nil
	}

	h.Logger.Info("Embedding chunks", "count", len(texts))

	// Submit to GPU via embed pipeline
	embedResult, err := h.GPUSubmitter.Embed(ctx, texts)
	if err != nil {
		h.GW.FailEnvelope(envelopeID, err.Error())
		return nil, fmt.Errorf("embed_chunks: gpu: %w", err)
	}

	// Store embeddings in DB
	stored := 0
	now := time.Now().Unix()
	for i, emb := range embedResult.Embeddings {
		if i >= len(chunkIDs) {
			break
		}
		blob := storage.SerializeVector(emb)
		norm := storage.CalculateNorm(emb)
		_, err := h.DB.ExecContext(ctx, `
			INSERT OR IGNORE INTO embeddings (chunk_id, document_id, embedding, dimension, norm, model_name, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, chunkIDs[i], docIDs[i], blob, len(emb), norm, embedResult.Model, now)
		if err != nil {
			h.Logger.Warn("Failed to store embedding",
				"chunk_index", i,
				"error", err)
			continue
		}
		stored++
	}

	h.Logger.Info("Embeddings stored",
		"stored", stored,
		"total", len(texts),
		"dimension", embedResult.Dimension,
		"model", embedResult.Model)

	result := map[string]interface{}{
		"status":    "embedded",
		"handler":   "embed_chunks",
		"embedded":  stored,
		"total":     len(texts),
		"dimension": embedResult.Dimension,
		"model":     embedResult.Model,
	}
	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("embed_chunks: submit next step: %w", err)
	}
	return result, nil
}
