package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"horos47/core/data"
	"horos47/storage"
)

var mentionRe = regexp.MustCompile(`@[a-zA-Z0-9_-]+`)

// QueryResult represents a RAG search result with score.
type QueryResult struct {
	ChunkID       data.UUID `json:"chunk_id"`
	DocumentID    data.UUID `json:"document_id"`
	ChunkText     string    `json:"chunk_text"`
	ChunkIndex    int       `json:"chunk_index"`
	DocumentTitle string    `json:"document_title"`
	Score         float64   `json:"score"`
}

// HandleRAGRetrieve performs hybrid search (FTS5 BM25 + vector) and advances workflow.
func (h *Handlers) HandleRAGRetrieve(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("rag_retrieve: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)

	content := ExtractContent(payload)
	if content == "" {
		result := map[string]interface{}{
			"status":  "no_query",
			"results": []interface{}{},
		}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("submit next step: %w", err)
		}
		return result, nil
	}

	// Strip @mentions from query before FTS5 search
	content = strings.TrimSpace(mentionRe.ReplaceAllString(content, ""))
	if content == "" {
		result := map[string]interface{}{
			"status":  "no_query",
			"results": []interface{}{},
		}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("submit next step: %w", err)
		}
		return result, nil
	}

	// Hybrid search: FTS5 BM25 + vector (when embeddings available)
	results, err := h.hybridSearch(ctx, content, 5, 0.3)
	if err != nil {
		h.Logger.Warn("RAG search failed", "error", err)
		results = nil
	}

	result := map[string]interface{}{
		"status":  "retrieved",
		"query":   content, // cleaned (no @mentions)
		"count":   len(results),
		"results": results,
	}

	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("submit next step: %w", err)
	}
	return result, nil
}

// sanitizeFTS5 strips characters that FTS5 interprets as syntax operators.
func sanitizeFTS5(q string) string {
	var b strings.Builder
	for _, r := range q {
		switch r {
		case '"', '*', '(', ')', '+', '-', '^', ':', ',', '{', '}', '!', '~', '?':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func (h *Handlers) hybridSearch(ctx context.Context, query string, topK int, minScore float64) ([]QueryResult, error) {
	// 1. FTS5 search
	ftsResults, err := h.ftsSearch(query, topK*2, minScore)
	if err != nil {
		h.Logger.Warn("FTS5 search failed", "error", err)
	}

	// 2. Vector search (if embeddings exist and embed server is reachable)
	var vecResults []QueryResult
	var embCount int
	if err := h.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM embeddings").Scan(&embCount); err == nil && embCount > 0 {
		queryEmb, err := h.getQueryEmbedding(ctx, query)
		if err != nil {
			h.Logger.Debug("Query embedding failed, FTS5 only", "error", err)
		} else {
			vecResults, _ = h.vectorSearchByEmbedding(queryEmb, topK*2, minScore)
		}
	}

	// 3. Merge results (union, deduplicate by chunk_id, take max score)
	if len(vecResults) == 0 {
		return ftsResults, nil
	}
	if len(ftsResults) == 0 {
		return vecResults, nil
	}

	seen := make(map[string]QueryResult)
	for _, r := range ftsResults {
		key := r.ChunkID.String()
		seen[key] = r
	}
	for _, r := range vecResults {
		key := r.ChunkID.String()
		if existing, ok := seen[key]; ok {
			if r.Score > existing.Score {
				seen[key] = r
			}
		} else {
			seen[key] = r
		}
	}

	merged := make([]QueryResult, 0, len(seen))
	for _, r := range seen {
		merged = append(merged, r)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})

	if len(merged) > topK {
		merged = merged[:topK]
	}
	return merged, nil
}

// ftsSearch performs FTS5 BM25 search.
func (h *Handlers) ftsSearch(query string, topK int, minScore float64) ([]QueryResult, error) {
	ftsQuery := sanitizeFTS5(query)
	if ftsQuery == "" {
		return nil, nil
	}

	rows, err := h.DB.Query(`
		SELECT c.chunk_id, c.document_id, c.chunk_text, c.chunk_index, d.title, chunks_fts.rank
		FROM chunks_fts
		INNER JOIN chunks c ON c.chunk_id = chunks_fts.chunk_id
		INNER JOIN documents d ON d.document_id = c.document_id
		WHERE chunks_fts MATCH ?
		ORDER BY chunks_fts.rank
		LIMIT ?
	`, ftsQuery, topK)
	if err != nil {
		return nil, err
	}
	defer data.SafeClose(rows, "fts search")

	var results []QueryResult
	for rows.Next() {
		var result QueryResult
		var rank float64
		err := rows.Scan(&result.ChunkID, &result.DocumentID, &result.ChunkText,
			&result.ChunkIndex, &result.DocumentTitle, &rank)
		if err != nil {
			continue
		}
		result.Score = normalizeRank(rank)
		if result.Score >= minScore {
			results = append(results, result)
		}
	}
	return results, nil
}

// getQueryEmbedding calls the embed server directly (no gpu_jobs pipeline) for fast query embedding.
func (h *Handlers) getQueryEmbedding(ctx context.Context, query string) ([]float32, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"model": "/models/gte-Qwen2-1.5B-instruct",
		"input": []string{query},
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "http://localhost:8003/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := h.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed server returned %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return result.Data[0].Embedding, nil
}

// vectorSearchByEmbedding performs pure vector search with pre-calculated norms.
func (h *Handlers) vectorSearchByEmbedding(queryEmbedding []float32, topK int, minScore float64) ([]QueryResult, error) {
	queryNorm := storage.CalculateNorm(queryEmbedding)

	rows, err := h.DB.Query(`
		SELECT e.chunk_id, e.document_id, e.embedding, e.norm,
			c.chunk_text, c.chunk_index, d.title
		FROM embeddings e
		INNER JOIN chunks c ON c.chunk_id = e.chunk_id
		INNER JOIN documents d ON d.document_id = c.document_id
	`)
	if err != nil {
		return nil, err
	}
	defer data.SafeClose(rows, "vector search")

	type scored struct {
		result QueryResult
		score  float64
	}
	var candidates []scored

	for rows.Next() {
		var result QueryResult
		var embeddingBlob []byte
		var docNorm float64

		if err := rows.Scan(&result.ChunkID, &result.DocumentID, &embeddingBlob, &docNorm,
			&result.ChunkText, &result.ChunkIndex, &result.DocumentTitle); err != nil {
			continue
		}

		embedding := storage.DeserializeVector(embeddingBlob)
		score := storage.CosineSimilarityOptimized(queryEmbedding, embedding, queryNorm, docNorm)
		if score >= minScore {
			candidates = append(candidates, scored{result: result, score: score})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	var results []QueryResult
	for i := 0; i < topK && i < len(candidates); i++ {
		r := candidates[i].result
		r.Score = candidates[i].score
		results = append(results, r)
	}
	return results, nil
}

func normalizeRank(rank float64) float64 {
	score := 1.0 / (1.0 - rank)
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}
