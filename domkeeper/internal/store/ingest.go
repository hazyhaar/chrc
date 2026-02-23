package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// IngestEntry tracks the processing of a batch or snapshot.
type IngestEntry struct {
	ID             string `json:"id"`
	BatchID        string `json:"batch_id,omitempty"`
	SnapshotID     string `json:"snapshot_id,omitempty"`
	PageURL        string `json:"page_url"`
	PageID         string `json:"page_id,omitempty"`
	Status         string `json:"status"` // "pending", "processing", "done", "error"
	ErrorMessage   string `json:"error_message,omitempty"`
	RecordsCount   int    `json:"records_count"`
	ExtractedCount int    `json:"extracted_count"`
	CreatedAt      int64  `json:"created_at"`
	CompletedAt    *int64 `json:"completed_at,omitempty"`
}

// InsertIngestEntry creates a new ingest log entry.
func (s *Store) InsertIngestEntry(ctx context.Context, e *IngestEntry) error {
	if e.CreatedAt == 0 {
		e.CreatedAt = time.Now().UnixMilli()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO ingest_log (id, batch_id, snapshot_id, page_url, page_id, status,
		                        error_message, records_count, extracted_count, created_at, completed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.BatchID, e.SnapshotID, e.PageURL, e.PageID, e.Status,
		e.ErrorMessage, e.RecordsCount, e.ExtractedCount, e.CreatedAt, e.CompletedAt,
	)
	return err
}

// CompleteIngestEntry marks an entry as done or error.
func (s *Store) CompleteIngestEntry(ctx context.Context, id, status, errMsg string, extractedCount int) error {
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE ingest_log SET status=?, error_message=?, extracted_count=?, completed_at=?
		WHERE id=?`, status, errMsg, extractedCount, now, id)
	return err
}

// RecentIngestEntries returns the latest N ingest log entries.
func (s *Store) RecentIngestEntries(ctx context.Context, limit int) ([]*IngestEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, batch_id, snapshot_id, page_url, page_id, status,
		       error_message, records_count, extracted_count, created_at, completed_at
		FROM ingest_log ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*IngestEntry
	for rows.Next() {
		e := &IngestEntry{}
		var completedAt sql.NullInt64
		if err := rows.Scan(&e.ID, &e.BatchID, &e.SnapshotID, &e.PageURL, &e.PageID,
			&e.Status, &e.ErrorMessage, &e.RecordsCount, &e.ExtractedCount,
			&e.CreatedAt, &completedAt); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			e.CompletedAt = &completedAt.Int64
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// SourcePage is a tracked page from domwatch.
type SourcePage struct {
	PageID      string `json:"page_id"`
	PageURL     string `json:"page_url"`
	TrustLevel  string `json:"trust_level"`
	ProfileJSON string `json:"profile_json"`
	LastSeen    int64  `json:"last_seen"`
	CreatedAt   int64  `json:"created_at"`
}

// UpsertSourcePage creates or updates a source page record.
func (s *Store) UpsertSourcePage(ctx context.Context, p *SourcePage) error {
	now := time.Now().UnixMilli()
	if p.LastSeen == 0 {
		p.LastSeen = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO source_pages (page_id, page_url, trust_level, profile_json, last_seen, created_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(page_id) DO UPDATE SET
			page_url=excluded.page_url, trust_level=excluded.trust_level,
			profile_json=excluded.profile_json, last_seen=excluded.last_seen`,
		p.PageID, p.PageURL, p.TrustLevel, p.ProfileJSON, p.LastSeen, now,
	)
	return err
}

// GetSourcePage retrieves a source page by ID.
func (s *Store) GetSourcePage(ctx context.Context, pageID string) (*SourcePage, error) {
	p := &SourcePage{}
	err := s.DB.QueryRowContext(ctx, `
		SELECT page_id, page_url, trust_level, profile_json, last_seen, created_at
		FROM source_pages WHERE page_id = ?`, pageID).Scan(
		&p.PageID, &p.PageURL, &p.TrustLevel, &p.ProfileJSON, &p.LastSeen, &p.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ListSourcePages returns all tracked source pages.
func (s *Store) ListSourcePages(ctx context.Context) ([]*SourcePage, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT page_id, page_url, trust_level, profile_json, last_seen, created_at
		FROM source_pages ORDER BY last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []*SourcePage
	for rows.Next() {
		p := &SourcePage{}
		if err := rows.Scan(&p.PageID, &p.PageURL, &p.TrustLevel, &p.ProfileJSON,
			&p.LastSeen, &p.CreatedAt); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}
