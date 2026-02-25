// CLAUDE:SUMMARY Search engine CRUD: insert, list, get, update, delete per shard.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// InsertSearchEngine adds a new search engine to the shard.
func (s *Store) InsertSearchEngine(ctx context.Context, e *SearchEngine) error {
	now := time.Now().UnixMilli()
	if e.CreatedAt == 0 {
		e.CreatedAt = now
	}
	if e.UpdatedAt == 0 {
		e.UpdatedAt = now
	}
	if e.Strategy == "" {
		e.Strategy = "api"
	}
	if e.APIConfigJSON == "" {
		e.APIConfigJSON = "{}"
	}
	if e.SelectorsJSON == "" {
		e.SelectorsJSON = "{}"
	}
	if e.RateLimitMs == 0 {
		e.RateLimitMs = 2000
	}
	if e.MaxPages == 0 {
		e.MaxPages = 3
	}

	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO search_engines (id, name, strategy, url_template, api_config,
		selectors, stealth_level, rate_limit_ms, max_pages, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Name, e.Strategy, e.URLTemplate, e.APIConfigJSON,
		e.SelectorsJSON, e.StealthLevel, e.RateLimitMs, e.MaxPages, e.Enabled,
		e.CreatedAt, e.UpdatedAt,
	)
	return err
}

// GetSearchEngine retrieves a search engine by ID.
func (s *Store) GetSearchEngine(ctx context.Context, id string) (*SearchEngine, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, name, strategy, url_template, api_config, selectors,
		stealth_level, rate_limit_ms, max_pages, enabled, created_at, updated_at
		FROM search_engines WHERE id = ?`, id)
	return scanSearchEngine(row)
}

// ListSearchEngines returns all search engines in the shard.
func (s *Store) ListSearchEngines(ctx context.Context) ([]*SearchEngine, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name, strategy, url_template, api_config, selectors,
		stealth_level, rate_limit_ms, max_pages, enabled, created_at, updated_at
		FROM search_engines ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var engines []*SearchEngine
	for rows.Next() {
		e, err := scanSearchEngineRows(rows)
		if err != nil {
			return nil, err
		}
		engines = append(engines, e)
	}
	return engines, rows.Err()
}

// DeleteSearchEngine removes a search engine by ID.
func (s *Store) DeleteSearchEngine(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM search_engines WHERE id = ?`, id)
	return err
}

// SearchEngineToSearch converts a stored SearchEngine to the search package Engine type.
// The caller must unmarshal APIConfigJSON and SelectorsJSON into the appropriate types.
func (e *SearchEngine) APIConfigRaw() json.RawMessage {
	return json.RawMessage(e.APIConfigJSON)
}

func scanSearchEngine(row *sql.Row) (*SearchEngine, error) {
	var e SearchEngine
	var enabled int
	err := row.Scan(
		&e.ID, &e.Name, &e.Strategy, &e.URLTemplate, &e.APIConfigJSON,
		&e.SelectorsJSON, &e.StealthLevel, &e.RateLimitMs, &e.MaxPages,
		&enabled, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan search engine: %w", err)
	}
	e.Enabled = enabled != 0
	return &e, nil
}

func scanSearchEngineRows(rows *sql.Rows) (*SearchEngine, error) {
	var e SearchEngine
	var enabled int
	err := rows.Scan(
		&e.ID, &e.Name, &e.Strategy, &e.URLTemplate, &e.APIConfigJSON,
		&e.SelectorsJSON, &e.StealthLevel, &e.RateLimitMs, &e.MaxPages,
		&enabled, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan search engine: %w", err)
	}
	e.Enabled = enabled != 0
	return &e, nil
}
