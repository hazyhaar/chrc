// CLAUDE:SUMMARY CRUD operations for extraction_rules table — insert, match by URL/page, update, failure tracking.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Rule is an extraction rule defining how to extract content from pages.
type Rule struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URLPattern  string   `json:"url_pattern"`
	PageID      string   `json:"page_id,omitempty"`
	Selectors   []string `json:"selectors"`
	ExtractMode string   `json:"extract_mode"` // "css", "xpath", "density", "auto"
	TrustLevel  string   `json:"trust_level"`  // "official", "institutional", "community", "unverified"
	FolderID    string   `json:"folder_id,omitempty"`
	Enabled     bool     `json:"enabled"`
	Priority    int      `json:"priority"`
	Version     int      `json:"version"`
	LastSuccess *int64   `json:"last_success,omitempty"`
	FailCount   int      `json:"fail_count"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

// InsertRule inserts a new extraction rule.
func (s *Store) InsertRule(ctx context.Context, r *Rule) error {
	sels, _ := json.Marshal(r.Selectors)
	now := time.Now().UnixMilli()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	if r.Version == 0 {
		r.Version = 1
	}

	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO extraction_rules
			(id, name, url_pattern, page_id, selectors, extract_mode, trust_level,
			 folder_id, enabled, priority, version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.URLPattern, r.PageID, string(sels), r.ExtractMode, r.TrustLevel,
		nullStr(r.FolderID), boolInt(r.Enabled), r.Priority, r.Version, r.CreatedAt, r.UpdatedAt,
	)
	return err
}

// GetRule retrieves a rule by ID.
func (s *Store) GetRule(ctx context.Context, id string) (*Rule, error) {
	r := &Rule{}
	var sels string
	var folderID sql.NullString
	var enabled int
	var lastSuccess sql.NullInt64

	err := s.DB.QueryRowContext(ctx, `
		SELECT id, name, url_pattern, page_id, selectors, extract_mode, trust_level,
		       folder_id, enabled, priority, version, last_success, fail_count, created_at, updated_at
		FROM extraction_rules WHERE id = ?`, id).Scan(
		&r.ID, &r.Name, &r.URLPattern, &r.PageID, &sels, &r.ExtractMode, &r.TrustLevel,
		&folderID, &enabled, &r.Priority, &r.Version, &lastSuccess, &r.FailCount, &r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(sels), &r.Selectors)
	r.FolderID = folderID.String
	r.Enabled = enabled != 0
	if lastSuccess.Valid {
		r.LastSuccess = &lastSuccess.Int64
	}
	return r, nil
}

// ListRules returns all rules, optionally filtered by enabled status.
func (s *Store) ListRules(ctx context.Context, enabledOnly bool) ([]*Rule, error) {
	query := `SELECT id, name, url_pattern, page_id, selectors, extract_mode, trust_level,
	                 folder_id, enabled, priority, version, last_success, fail_count, created_at, updated_at
	          FROM extraction_rules`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY priority DESC, created_at ASC`

	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*Rule
	for rows.Next() {
		r := &Rule{}
		var sels string
		var folderID sql.NullString
		var enabled int
		var lastSuccess sql.NullInt64

		if err := rows.Scan(
			&r.ID, &r.Name, &r.URLPattern, &r.PageID, &sels, &r.ExtractMode, &r.TrustLevel,
			&folderID, &enabled, &r.Priority, &r.Version, &lastSuccess, &r.FailCount, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}

		json.Unmarshal([]byte(sels), &r.Selectors)
		r.FolderID = folderID.String
		r.Enabled = enabled != 0
		if lastSuccess.Valid {
			r.LastSuccess = &lastSuccess.Int64
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// MatchRules returns enabled rules whose url_pattern matches the given URL.
func (s *Store) MatchRules(ctx context.Context, pageURL, pageID string) ([]*Rule, error) {
	// When pageID is non-empty, match by exact page_id.
	// When pageID is empty, match by URL GLOB pattern.
	query := `
		SELECT id, name, url_pattern, page_id, selectors, extract_mode, trust_level,
		       folder_id, enabled, priority, version, last_success, fail_count, created_at, updated_at
		FROM extraction_rules
		WHERE enabled = 1
		  AND ((? != '' AND page_id = ?) OR (page_id = '' AND ? GLOB url_pattern))
		ORDER BY priority DESC`
	rows, err := s.DB.QueryContext(ctx, query, pageID, pageID, pageURL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*Rule
	for rows.Next() {
		r := &Rule{}
		var sels string
		var folderID sql.NullString
		var enabled int
		var lastSuccess sql.NullInt64

		if err := rows.Scan(
			&r.ID, &r.Name, &r.URLPattern, &r.PageID, &sels, &r.ExtractMode, &r.TrustLevel,
			&folderID, &enabled, &r.Priority, &r.Version, &lastSuccess, &r.FailCount, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}

		json.Unmarshal([]byte(sels), &r.Selectors)
		r.FolderID = folderID.String
		r.Enabled = enabled != 0
		if lastSuccess.Valid {
			r.LastSuccess = &lastSuccess.Int64
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// UpdateRule updates a rule. Bumps version and updated_at.
func (s *Store) UpdateRule(ctx context.Context, r *Rule) error {
	sels, _ := json.Marshal(r.Selectors)
	r.UpdatedAt = time.Now().UnixMilli()
	r.Version++

	_, err := s.DB.ExecContext(ctx, `
		UPDATE extraction_rules SET
			name=?, url_pattern=?, page_id=?, selectors=?, extract_mode=?,
			trust_level=?, folder_id=?, enabled=?, priority=?, version=?,
			updated_at=?
		WHERE id=?`,
		r.Name, r.URLPattern, r.PageID, string(sels), r.ExtractMode,
		r.TrustLevel, nullStr(r.FolderID), boolInt(r.Enabled), r.Priority, r.Version,
		r.UpdatedAt, r.ID,
	)
	return err
}

// DeleteRule removes a rule by ID. Cascades to content_cache and chunks.
func (s *Store) DeleteRule(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM extraction_rules WHERE id = ?`, id)
	return err
}

// RecordRuleSuccess updates last_success and resets fail_count.
func (s *Store) RecordRuleSuccess(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE extraction_rules SET last_success = ?, fail_count = 0, updated_at = ?
		WHERE id = ?`, now, now, id)
	return err
}

// RecordRuleFailure increments fail_count.
func (s *Store) RecordRuleFailure(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE extraction_rules SET fail_count = fail_count + 1, updated_at = ?
		WHERE id = ?`, now, id)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
