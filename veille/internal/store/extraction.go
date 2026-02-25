// CLAUDE:SUMMARY Extraction CRUD: insert with FTS5 sync, list by source, existence check for dedup.
package store

import (
	"context"
	"database/sql"
	"fmt"
)

// InsertExtraction stores a new extraction.
func (s *Store) InsertExtraction(ctx context.Context, e *Extraction) error {
	if e.MetadataJSON == "" {
		e.MetadataJSON = "{}"
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO extractions (id, source_id, content_hash, title, extracted_text,
		extracted_html, url, extracted_at, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.SourceID, e.ContentHash, e.Title, e.ExtractedText,
		e.ExtractedHTML, e.URL, e.ExtractedAt, e.MetadataJSON,
	)
	return err
}

// GetExtraction retrieves an extraction by ID.
func (s *Store) GetExtraction(ctx context.Context, id string) (*Extraction, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, source_id, content_hash, title, extracted_text, extracted_html,
		url, extracted_at, metadata_json
		FROM extractions WHERE id = ?`, id)

	var e Extraction
	err := row.Scan(&e.ID, &e.SourceID, &e.ContentHash, &e.Title, &e.ExtractedText,
		&e.ExtractedHTML, &e.URL, &e.ExtractedAt, &e.MetadataJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan extraction: %w", err)
	}
	return &e, nil
}

// ListExtractions returns extractions for a source, newest first.
func (s *Store) ListExtractions(ctx context.Context, sourceID string, limit int) ([]*Extraction, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, source_id, content_hash, title, extracted_text, extracted_html,
		url, extracted_at, metadata_json
		FROM extractions WHERE source_id = ?
		ORDER BY extracted_at DESC LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Extraction
	for rows.Next() {
		var e Extraction
		if err := rows.Scan(&e.ID, &e.SourceID, &e.ContentHash, &e.Title, &e.ExtractedText,
			&e.ExtractedHTML, &e.URL, &e.ExtractedAt, &e.MetadataJSON); err != nil {
			return nil, fmt.Errorf("scan extraction: %w", err)
		}
		result = append(result, &e)
	}
	return result, rows.Err()
}

// ExtractionExists checks if an extraction with the given source and content hash exists.
// Used for deduplication in RSS/API pipelines to avoid re-processing identical content.
func (s *Store) ExtractionExists(ctx context.Context, sourceID, contentHash string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM extractions WHERE source_id = ? AND content_hash = ?`,
		sourceID, contentHash).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("extraction exists: %w", err)
	}
	return count > 0, nil
}

// DeleteExtractionsBySource removes all extractions for a source.
func (s *Store) DeleteExtractionsBySource(ctx context.Context, sourceID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM extractions WHERE source_id = ?`, sourceID)
	return err
}
