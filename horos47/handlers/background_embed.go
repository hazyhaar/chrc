package handlers

import (
	"context"
	"net/http"
	"time"

	"horos47/storage"
)

// RunBackgroundEmbedder periodically checks for unembedded chunks and submits
// embedding jobs when the embed server is reachable. Runs until context is cancelled.
func (h *Handlers) RunBackgroundEmbedder(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.embedPendingChunks(ctx)
		}
	}
}

func (h *Handlers) embedPendingChunks(ctx context.Context) {
	if h.GPUSubmitter == nil {
		return
	}

	// Quick check: any unembedded chunks?
	var count int
	err := h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM chunks
		WHERE chunk_id NOT IN (SELECT chunk_id FROM embeddings)
	`).Scan(&count)
	if err != nil || count == 0 {
		return
	}

	// Quick check: embed server reachable?
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:8003/health")
	if err != nil {
		return
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}

	h.Logger.Info("Background embedder: processing", "unembedded", count)

	// Process in batches of 32
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		rows, err := h.DB.QueryContext(ctx, `
			SELECT c.chunk_id, c.document_id, c.chunk_text
			FROM chunks c
			WHERE c.chunk_id NOT IN (SELECT chunk_id FROM embeddings)
			LIMIT 32
		`)
		if err != nil {
			h.Logger.Warn("Background embedder: query failed", "error", err)
			return
		}

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
		rows.Close()

		if len(texts) == 0 {
			return // all done
		}

		embedResult, err := h.GPUSubmitter.Embed(ctx, texts)
		if err != nil {
			h.Logger.Warn("Background embedder: GPU embed failed", "error", err)
			return
		}

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
				continue
			}
			stored++
		}

		h.Logger.Info("Background embedder: batch done",
			"stored", stored,
			"batch", len(texts),
			"dimension", embedResult.Dimension)
	}
}
