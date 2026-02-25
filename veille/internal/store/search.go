// CLAUDE:SUMMARY FTS5 full-text search on extractions with snippet generation.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/hazyhaar/pkg/idgen"
)

// Search performs a FTS5 full-text search on extractions.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]*SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT e.id, e.source_id, e.title, e.extracted_text, rank
		FROM extractions_fts f
		JOIN extractions e ON e.rowid = f.rowid
		WHERE extractions_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ExtractionID, &r.SourceID, &r.Title, &r.Text, &r.Rank); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		results = append(results, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Log the search (fire-and-forget).
	s.DB.ExecContext(ctx,
		`INSERT INTO search_log (id, query, result_count, searched_at) VALUES (?, ?, ?, ?)`,
		idgen.New(), query, len(results), time.Now().UnixMilli())

	return results, nil
}

// ListSearchLog returns recent search log entries.
func (s *Store) ListSearchLog(ctx context.Context, limit int) ([]SearchLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, query, result_count, searched_at FROM search_log ORDER BY searched_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []SearchLogEntry
	for rows.Next() {
		var e SearchLogEntry
		if err := rows.Scan(&e.ID, &e.Query, &e.ResultCount, &e.SearchedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
