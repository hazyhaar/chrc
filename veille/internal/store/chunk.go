package store

import (
	"context"
	"fmt"
)

// InsertChunks stores a batch of chunks in a single transaction.
func (s *Store) InsertChunks(ctx context.Context, chunks []*Chunk) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO chunks (id, extraction_id, source_id, chunk_index, text,
		token_count, overlap_prev, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, c := range chunks {
		_, err := stmt.ExecContext(ctx,
			c.ID, c.ExtractionID, c.SourceID, c.ChunkIndex, c.Text,
			c.TokenCount, c.OverlapPrev, c.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert chunk %d: %w", c.ChunkIndex, err)
		}
	}

	return tx.Commit()
}

// ListChunks returns chunks ordered by creation, newest first.
func (s *Store) ListChunks(ctx context.Context, limit, offset int) ([]*Chunk, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, extraction_id, source_id, chunk_index, text,
		token_count, overlap_prev, created_at
		FROM chunks ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.ExtractionID, &c.SourceID, &c.ChunkIndex,
			&c.Text, &c.TokenCount, &c.OverlapPrev, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		result = append(result, &c)
	}
	return result, rows.Err()
}
