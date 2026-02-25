// CLAUDE:SUMMARY Loads watch_pages configuration from SQLite and provides a change watcher.
package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hazyhaar/pkg/watch"
)

// Schema for the watch_pages table.
const Schema = `
CREATE TABLE IF NOT EXISTS watch_pages (
	id                  TEXT PRIMARY KEY,
	url                 TEXT NOT NULL,
	stealth_level       INTEGER DEFAULT -1,
	selectors           TEXT DEFAULT '[]',
	filters             TEXT DEFAULT '[]',
	snapshot_interval_ms INTEGER DEFAULT 14400000,
	profile             INTEGER DEFAULT 1,
	status              TEXT DEFAULT 'active',
	updated_at          INTEGER NOT NULL
);
`

// DBPage is a row from the watch_pages table.
type DBPage struct {
	ID               string
	URL              string
	StealthLevel     int
	Selectors        []string
	Filters          []string
	SnapshotInterval time.Duration
	Profile          bool
	Status           string
}

// LoadPages reads all active pages from the database.
func LoadPages(ctx context.Context, db *sql.DB) ([]DBPage, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, url, stealth_level, selectors, filters,
		       snapshot_interval_ms, profile, status
		FROM watch_pages
		WHERE status = 'active'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []DBPage
	for rows.Next() {
		var p DBPage
		var selsJSON, filtersJSON string
		var snapMs int64
		var profileInt int

		if err := rows.Scan(&p.ID, &p.URL, &p.StealthLevel,
			&selsJSON, &filtersJSON, &snapMs, &profileInt, &p.Status); err != nil {
			return nil, err
		}

		json.Unmarshal([]byte(selsJSON), &p.Selectors)
		json.Unmarshal([]byte(filtersJSON), &p.Filters)
		p.SnapshotInterval = time.Duration(snapMs) * time.Millisecond
		p.Profile = profileInt != 0
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

// WatchPages creates a watch.Watcher that detects changes to watch_pages.
func WatchPages(db *sql.DB, logger *slog.Logger) *watch.Watcher {
	return watch.New(db, watch.Options{
		Interval: 200 * time.Millisecond,
		Debounce: 500 * time.Millisecond,
		Detector: watch.PragmaDataVersion,
		Logger:   logger,
	})
}
