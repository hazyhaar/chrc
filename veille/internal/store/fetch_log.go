package store

import (
	"context"
	"fmt"
)

// InsertFetchLog records a fetch attempt.
func (s *Store) InsertFetchLog(ctx context.Context, entry *FetchLogEntry) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO fetch_log (id, source_id, status, status_code, content_hash,
		error_message, duration_ms, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.SourceID, entry.Status, entry.StatusCode,
		entry.ContentHash, entry.ErrorMessage, entry.DurationMs, entry.FetchedAt,
	)
	return err
}

// FetchHistory returns fetch log entries for a source, newest first.
func (s *Store) FetchHistory(ctx context.Context, sourceID string, limit int) ([]*FetchLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, source_id, status, status_code, content_hash,
		error_message, duration_ms, fetched_at
		FROM fetch_log WHERE source_id = ?
		ORDER BY fetched_at DESC LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*FetchLogEntry
	for rows.Next() {
		var e FetchLogEntry
		if err := rows.Scan(&e.ID, &e.SourceID, &e.Status, &e.StatusCode,
			&e.ContentHash, &e.ErrorMessage, &e.DurationMs, &e.FetchedAt); err != nil {
			return nil, fmt.Errorf("scan fetch log: %w", err)
		}
		result = append(result, &e)
	}
	return result, rows.Err()
}
