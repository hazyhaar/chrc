// CLAUDE:SUMMARY One-shot migration: normalizes source URLs and removes duplicates so the UNIQUE index can be applied.
// CLAUDE:DEPENDS veille/normalize.go
// CLAUDE:EXPORTS MigrateNormalizeURLs
package veille

import (
	"database/sql"
	"log/slog"
)

// MigrateNormalizeURLs normalizes all source URLs and removes duplicates.
// For each group of sources sharing the same normalized URL, the oldest
// (by created_at) is kept and the others are deleted (CASCADE removes
// their extractions and fetch_log entries).
//
// This migration is idempotent and safe to call at every startup.
// It must run BEFORE the UNIQUE index on sources(url) is applied.
func MigrateNormalizeURLs(db *sql.DB) error {
	// Check if sources table exists (new DBs may not have it yet).
	var tableCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sources'`,
	).Scan(&tableCount); err != nil || tableCount == 0 {
		return nil
	}

	// Read all sources ordered by created_at ASC (oldest first).
	rows, err := db.Query(`SELECT id, url, created_at FROM sources ORDER BY created_at ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type entry struct {
		id        string
		url       string
		createdAt int64
	}
	var all []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.url, &e.createdAt); err != nil {
			return err
		}
		all = append(all, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(all) == 0 {
		return nil
	}

	// Group by normalized URL, preserving insertion order (oldest first).
	type group struct {
		normalizedURL string
		entries       []entry
	}
	groups := make(map[string]*group)
	var order []string // track insertion order
	for _, e := range all {
		norm, normErr := NormalizeSourceURL(e.url)
		if normErr != nil {
			// Malformed URL — skip normalization, keep as-is.
			norm = e.url
		}
		g, exists := groups[norm]
		if !exists {
			g = &group{normalizedURL: norm}
			groups[norm] = g
			order = append(order, norm)
		}
		g.entries = append(g.entries, e)
	}

	// Apply changes in a single transaction.
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var deleted, updated int

	for _, normURL := range order {
		g := groups[normURL]
		keep := g.entries[0] // oldest by created_at (query was ORDER BY created_at ASC)

		// Delete duplicates (all except the oldest).
		for _, dup := range g.entries[1:] {
			if _, err := tx.Exec(`DELETE FROM sources WHERE id = ?`, dup.id); err != nil {
				return err
			}
			deleted++
		}

		// Normalize the surviving source's URL if it changed.
		if keep.url != g.normalizedURL {
			if _, err := tx.Exec(
				`UPDATE sources SET url = ? WHERE id = ?`, g.normalizedURL, keep.id,
			); err != nil {
				return err
			}
			updated++
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if deleted > 0 || updated > 0 {
		slog.Info("veille: URL migration complete", "deleted", deleted, "normalized", updated)
	}
	return nil
}
