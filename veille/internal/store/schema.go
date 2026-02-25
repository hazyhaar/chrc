// CLAUDE:SUMMARY Applies the complete veille SQL schema including FTS5 indexes and triggers.
package store

import "database/sql"

// Schema is the complete veille schema applied to each user×space shard.
const Schema = `
-- Sources to monitor
CREATE TABLE IF NOT EXISTS sources (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    url             TEXT NOT NULL,
    source_type     TEXT NOT NULL DEFAULT 'web',
    fetch_interval  INTEGER NOT NULL DEFAULT 3600000,
    enabled         INTEGER NOT NULL DEFAULT 1,
    config_json     TEXT NOT NULL DEFAULT '{}',
    last_fetched_at INTEGER,
    last_hash       TEXT NOT NULL DEFAULT '',
    last_status     TEXT NOT NULL DEFAULT 'pending',
    last_error      TEXT NOT NULL DEFAULT '',
    fail_count      INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sources_enabled ON sources(enabled, last_fetched_at);

-- Extractions: content extracted from a source at a point in time
CREATE TABLE IF NOT EXISTS extractions (
    id              TEXT PRIMARY KEY,
    source_id       TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    content_hash    TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    extracted_text  TEXT NOT NULL,
    extracted_html  TEXT NOT NULL DEFAULT '',
    url             TEXT NOT NULL,
    extracted_at    INTEGER NOT NULL,
    metadata_json   TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_extractions_source ON extractions(source_id);
CREATE INDEX IF NOT EXISTS idx_extractions_time ON extractions(extracted_at DESC);

-- FTS5 on extractions (title + text)
CREATE VIRTUAL TABLE IF NOT EXISTS extractions_fts USING fts5(
    title, extracted_text, content='extractions', content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);

-- Triggers to keep FTS5 in sync
CREATE TRIGGER IF NOT EXISTS extractions_ai AFTER INSERT ON extractions BEGIN
    INSERT INTO extractions_fts(rowid, title, extracted_text) VALUES (new.rowid, new.title, new.extracted_text);
END;
CREATE TRIGGER IF NOT EXISTS extractions_ad AFTER DELETE ON extractions BEGIN
    INSERT INTO extractions_fts(extractions_fts, rowid, title, extracted_text) VALUES('delete', old.rowid, old.title, old.extracted_text);
END;
CREATE TRIGGER IF NOT EXISTS extractions_au AFTER UPDATE ON extractions BEGIN
    INSERT INTO extractions_fts(extractions_fts, rowid, title, extracted_text) VALUES('delete', old.rowid, old.title, old.extracted_text);
    INSERT INTO extractions_fts(rowid, title, extracted_text) VALUES (new.rowid, new.title, new.extracted_text);
END;

-- Fetch log (observability)
CREATE TABLE IF NOT EXISTS fetch_log (
    id              TEXT PRIMARY KEY,
    source_id       TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    status          TEXT NOT NULL,
    status_code     INTEGER,
    content_hash    TEXT NOT NULL DEFAULT '',
    error_message   TEXT NOT NULL DEFAULT '',
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    fetched_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_fetch_log_source ON fetch_log(source_id, fetched_at DESC);

-- Search engines (per-shard)
CREATE TABLE IF NOT EXISTS search_engines (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    strategy      TEXT NOT NULL DEFAULT 'api',
    url_template  TEXT NOT NULL,
    api_config    TEXT NOT NULL DEFAULT '{}',
    selectors     TEXT NOT NULL DEFAULT '{}',
    stealth_level INTEGER NOT NULL DEFAULT 1,
    rate_limit_ms INTEGER NOT NULL DEFAULT 2000,
    max_pages     INTEGER NOT NULL DEFAULT 3,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

-- Tracked questions (per-shard)
CREATE TABLE IF NOT EXISTS tracked_questions (
    id                TEXT PRIMARY KEY,
    text              TEXT NOT NULL,
    keywords          TEXT NOT NULL DEFAULT '',
    channels          TEXT NOT NULL DEFAULT '[]',
    schedule_ms       INTEGER NOT NULL DEFAULT 86400000,
    max_results       INTEGER NOT NULL DEFAULT 20,
    follow_links      INTEGER NOT NULL DEFAULT 1,
    enabled           INTEGER NOT NULL DEFAULT 1,
    last_run_at       INTEGER,
    last_result_count INTEGER NOT NULL DEFAULT 0,
    total_results     INTEGER NOT NULL DEFAULT 0,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tracked_questions_enabled ON tracked_questions(enabled, last_run_at);

-- Search log (per-shard, user search history)
CREATE TABLE IF NOT EXISTS search_log (
    id           TEXT PRIMARY KEY,
    query        TEXT NOT NULL,
    result_count INTEGER NOT NULL DEFAULT 0,
    searched_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_search_log_time ON search_log(searched_at DESC);
`

// Migration adds the UNIQUE index on sources(url) for dedup.
// Safe to run on existing databases (IF NOT EXISTS).
const Migration001UniqueURL = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_sources_url_unique ON sources(url);
`

// Migration002OriginalFetchInterval adds original_fetch_interval for backoff tracking.
// NULL = no backoff active, non-NULL = original interval to restore after recovery.
const Migration002OriginalFetchInterval = `
ALTER TABLE sources ADD COLUMN original_fetch_interval INTEGER;
`

// ApplySchema creates all tables and indexes on the given database.
func ApplySchema(db *sql.DB) error {
	if _, err := db.Exec(Schema); err != nil {
		return err
	}
	// Apply migrations.
	if _, err := db.Exec(Migration001UniqueURL); err != nil {
		return err
	}
	applyColumnMigration(db, "sources", "original_fetch_interval", Migration002OriginalFetchInterval)
	return nil
}

// applyColumnMigration adds a column if it doesn't exist (idempotent).
func applyColumnMigration(db *sql.DB, table, column, ddl string) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count)
	if err != nil || count > 0 {
		return
	}
	db.Exec(ddl)
}
