// CLAUDE:SUMMARY CRUD operations for the content_cache table — extracted content storage with hash-based dedup.
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Content is an extracted content entry from a page.
type Content struct {
	ID            string `json:"id"`
	RuleID        string `json:"rule_id"`
	PageURL       string `json:"page_url"`
	PageID        string `json:"page_id,omitempty"`
	SnapshotRef   string `json:"snapshot_ref,omitempty"`
	ContentHash   string `json:"content_hash"`
	ExtractedText string `json:"extracted_text"`
	ExtractedHTML string `json:"extracted_html,omitempty"`
	Title         string `json:"title,omitempty"`
	Metadata      string `json:"metadata,omitempty"`
	TrustLevel    string `json:"trust_level"`
	ExtractedAt   int64  `json:"extracted_at"`
	ExpiresAt     *int64 `json:"expires_at,omitempty"`
}

// InsertContent stores extracted content. Returns false if a content with
// the same hash already exists for this rule (dedup).
func (s *Store) InsertContent(ctx context.Context, c *Content) (bool, error) {
	if c.ExtractedAt == 0 {
		c.ExtractedAt = time.Now().UnixMilli()
	}

	// Check for duplicate content hash on same rule.
	var existing int
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM content_cache
		WHERE rule_id = ? AND content_hash = ?`, c.RuleID, c.ContentHash).Scan(&existing)
	if err != nil {
		return false, err
	}
	if existing > 0 {
		return false, nil // content unchanged
	}

	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO content_cache
			(id, rule_id, page_url, page_id, snapshot_ref, content_hash,
			 extracted_text, extracted_html, title, metadata, trust_level,
			 extracted_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.RuleID, c.PageURL, c.PageID, c.SnapshotRef, c.ContentHash,
		c.ExtractedText, c.ExtractedHTML, c.Title, c.Metadata, c.TrustLevel,
		c.ExtractedAt, c.ExpiresAt,
	)
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetContent retrieves content by ID.
func (s *Store) GetContent(ctx context.Context, id string) (*Content, error) {
	c := &Content{}
	var expiresAt sql.NullInt64

	err := s.DB.QueryRowContext(ctx, `
		SELECT id, rule_id, page_url, page_id, snapshot_ref, content_hash,
		       extracted_text, extracted_html, title, metadata, trust_level,
		       extracted_at, expires_at
		FROM content_cache WHERE id = ?`, id).Scan(
		&c.ID, &c.RuleID, &c.PageURL, &c.PageID, &c.SnapshotRef, &c.ContentHash,
		&c.ExtractedText, &c.ExtractedHTML, &c.Title, &c.Metadata, &c.TrustLevel,
		&c.ExtractedAt, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		c.ExpiresAt = &expiresAt.Int64
	}
	return c, nil
}

// ListContentByRule returns all content for a given rule.
func (s *Store) ListContentByRule(ctx context.Context, ruleID string) ([]*Content, error) {
	return s.queryContent(ctx, `
		SELECT id, rule_id, page_url, page_id, snapshot_ref, content_hash,
		       extracted_text, extracted_html, title, metadata, trust_level,
		       extracted_at, expires_at
		FROM content_cache WHERE rule_id = ?
		ORDER BY extracted_at DESC`, ruleID)
}

// LatestContentByRule returns the most recent content for a rule.
func (s *Store) LatestContentByRule(ctx context.Context, ruleID string) (*Content, error) {
	c := &Content{}
	var expiresAt sql.NullInt64

	err := s.DB.QueryRowContext(ctx, `
		SELECT id, rule_id, page_url, page_id, snapshot_ref, content_hash,
		       extracted_text, extracted_html, title, metadata, trust_level,
		       extracted_at, expires_at
		FROM content_cache WHERE rule_id = ?
		ORDER BY extracted_at DESC LIMIT 1`, ruleID).Scan(
		&c.ID, &c.RuleID, &c.PageURL, &c.PageID, &c.SnapshotRef, &c.ContentHash,
		&c.ExtractedText, &c.ExtractedHTML, &c.Title, &c.Metadata, &c.TrustLevel,
		&c.ExtractedAt, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		c.ExpiresAt = &expiresAt.Int64
	}
	return c, nil
}

// DeleteExpiredContent removes content past its expires_at timestamp.
func (s *Store) DeleteExpiredContent(ctx context.Context) (int64, error) {
	now := time.Now().UnixMilli()
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM content_cache WHERE expires_at IS NOT NULL AND expires_at < ?`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteContentByRule removes all content for a given rule.
func (s *Store) DeleteContentByRule(ctx context.Context, ruleID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM content_cache WHERE rule_id = ?`, ruleID)
	return err
}

func (s *Store) queryContent(ctx context.Context, query string, args ...any) ([]*Content, error) {
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*Content
	for rows.Next() {
		c := &Content{}
		var expiresAt sql.NullInt64
		if err := rows.Scan(
			&c.ID, &c.RuleID, &c.PageURL, &c.PageID, &c.SnapshotRef, &c.ContentHash,
			&c.ExtractedText, &c.ExtractedHTML, &c.Title, &c.Metadata, &c.TrustLevel,
			&c.ExtractedAt, &expiresAt,
		); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			c.ExpiresAt = &expiresAt.Int64
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
