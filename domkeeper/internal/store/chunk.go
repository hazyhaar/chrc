// CLAUDE:SUMMARY CRUD operations for the chunks table — batch insert, get-by-content, delete.
package store

import (
	"context"
	"time"
)

// Chunk is a text fragment for RAG / full-text search.
type Chunk struct {
	ID          string `json:"id"`
	ContentID   string `json:"content_id"`
	ChunkIndex  int    `json:"chunk_index"`
	Text        string `json:"text"`
	TokenCount  int    `json:"token_count"`
	OverlapPrev int    `json:"overlap_prev"`
	Metadata    string `json:"metadata,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

// InsertChunks stores multiple chunks in a single transaction.
func (s *Store) InsertChunks(ctx context.Context, chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chunks (id, content_id, chunk_index, text, token_count, overlap_prev, metadata, created_at)
		VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UnixMilli()
	for _, c := range chunks {
		if c.CreatedAt == 0 {
			c.CreatedAt = now
		}
		if _, err := stmt.ExecContext(ctx,
			c.ID, c.ContentID, c.ChunkIndex, c.Text, c.TokenCount,
			c.OverlapPrev, c.Metadata, c.CreatedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetChunksByContent returns all chunks for a content ID, ordered by index.
func (s *Store) GetChunksByContent(ctx context.Context, contentID string) ([]*Chunk, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, content_id, chunk_index, text, token_count, overlap_prev, metadata, created_at
		FROM chunks WHERE content_id = ?
		ORDER BY chunk_index`, contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []*Chunk
	for rows.Next() {
		c := &Chunk{}
		if err := rows.Scan(&c.ID, &c.ContentID, &c.ChunkIndex, &c.Text,
			&c.TokenCount, &c.OverlapPrev, &c.Metadata, &c.CreatedAt); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// DeleteChunksByContent removes all chunks for a content ID.
func (s *Store) DeleteChunksByContent(ctx context.Context, contentID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM chunks WHERE content_id = ?`, contentID)
	return err
}
