// CLAUDE:SUMMARY Source CRUD, DueSources scheduling query, and fetch status recording.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// InsertSource adds a new source to the shard.
func (s *Store) InsertSource(ctx context.Context, src *Source) error {
	now := time.Now().UnixMilli()
	if src.CreatedAt == 0 {
		src.CreatedAt = now
	}
	if src.UpdatedAt == 0 {
		src.UpdatedAt = now
	}
	if src.SourceType == "" {
		src.SourceType = "web"
	}
	if src.FetchInterval == 0 {
		src.FetchInterval = 3600000
	}
	if src.ConfigJSON == "" {
		src.ConfigJSON = "{}"
	}
	if src.LastStatus == "" {
		src.LastStatus = "pending"
	}

	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sources (id, name, url, source_type, fetch_interval, enabled,
		config_json, last_fetched_at, last_hash, last_status, last_error, fail_count,
		created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		src.ID, src.Name, src.URL, src.SourceType, src.FetchInterval, src.Enabled,
		src.ConfigJSON, src.LastFetchedAt, src.LastHash, src.LastStatus, src.LastError,
		src.FailCount, src.CreatedAt, src.UpdatedAt,
	)
	return err
}

// GetSource retrieves a source by ID.
func (s *Store) GetSource(ctx context.Context, id string) (*Source, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, name, url, source_type, fetch_interval, enabled,
		config_json, last_fetched_at, last_hash, last_status, last_error, fail_count,
		created_at, updated_at
		FROM sources WHERE id = ?`, id)
	return scanSource(row)
}

// ListSources returns all sources in the shard.
func (s *Store) ListSources(ctx context.Context) ([]*Source, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name, url, source_type, fetch_interval, enabled,
		config_json, last_fetched_at, last_hash, last_status, last_error, fail_count,
		created_at, updated_at
		FROM sources ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []*Source
	for rows.Next() {
		src, err := scanSourceRows(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

// UpdateSource updates a source's mutable fields.
func (s *Store) UpdateSource(ctx context.Context, src *Source) error {
	src.UpdatedAt = time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sources SET name=?, url=?, source_type=?, fetch_interval=?,
		enabled=?, config_json=?, updated_at=?
		WHERE id=?`,
		src.Name, src.URL, src.SourceType, src.FetchInterval,
		src.Enabled, src.ConfigJSON, src.UpdatedAt, src.ID,
	)
	return err
}

// DeleteSource removes a source (cascades to extractions, chunks, fetch_log).
func (s *Store) DeleteSource(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
	return err
}

// GetSourceByURL returns an enabled source matching the given URL, or nil.
func (s *Store) GetSourceByURL(ctx context.Context, url string) (*Source, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, name, url, source_type, fetch_interval, enabled,
		config_json, last_fetched_at, last_hash, last_status, last_error, fail_count,
		created_at, updated_at
		FROM sources WHERE url = ? LIMIT 1`, url)
	return scanSource(row)
}

// CountSources returns the total number of sources in the shard.
func (s *Store) CountSources(ctx context.Context) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sources`).Scan(&count)
	return count, err
}

// DueSources returns enabled sources whose next fetch time has passed.
// next fetch = last_fetched_at + fetch_interval
// Sources with nil last_fetched_at are always due.
func (s *Store) DueSources(ctx context.Context, maxFailCount int) ([]*Source, error) {
	now := time.Now().UnixMilli()
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name, url, source_type, fetch_interval, enabled,
		config_json, last_fetched_at, last_hash, last_status, last_error, fail_count,
		created_at, updated_at
		FROM sources
		WHERE enabled = 1
		  AND fail_count < ?
		  AND (last_fetched_at IS NULL OR last_fetched_at + fetch_interval <= ?)
		ORDER BY last_fetched_at ASC NULLS FIRST`, maxFailCount, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []*Source
	for rows.Next() {
		src, err := scanSourceRows(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

// RecordFetchSuccess updates a source after a successful fetch.
func (s *Store) RecordFetchSuccess(ctx context.Context, id, hash string) error {
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sources SET last_fetched_at=?, last_hash=?, last_status='ok',
		last_error='', fail_count=0, updated_at=?
		WHERE id=?`, now, hash, now, id)
	return err
}

// RecordFetchUnchanged updates the last_fetched_at without changing content hash.
func (s *Store) RecordFetchUnchanged(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sources SET last_fetched_at=?, last_status='unchanged',
		last_error='', fail_count=0, updated_at=?
		WHERE id=?`, now, now, id)
	return err
}

// RecordFetchError updates a source after a failed fetch.
func (s *Store) RecordFetchError(ctx context.Context, id, errMsg string) error {
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sources SET last_fetched_at=?, last_status='error',
		last_error=?, fail_count=fail_count+1, updated_at=?
		WHERE id=?`, now, errMsg, now, id)
	return err
}

func scanSource(row *sql.Row) (*Source, error) {
	var src Source
	var enabled int
	err := row.Scan(
		&src.ID, &src.Name, &src.URL, &src.SourceType, &src.FetchInterval, &enabled,
		&src.ConfigJSON, &src.LastFetchedAt, &src.LastHash, &src.LastStatus, &src.LastError,
		&src.FailCount, &src.CreatedAt, &src.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan source: %w", err)
	}
	src.Enabled = enabled != 0
	return &src, nil
}

func scanSourceRows(rows *sql.Rows) (*Source, error) {
	var src Source
	var enabled int
	err := rows.Scan(
		&src.ID, &src.Name, &src.URL, &src.SourceType, &src.FetchInterval, &enabled,
		&src.ConfigJSON, &src.LastFetchedAt, &src.LastHash, &src.LastStatus, &src.LastError,
		&src.FailCount, &src.CreatedAt, &src.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan source: %w", err)
	}
	src.Enabled = enabled != 0
	return &src, nil
}
