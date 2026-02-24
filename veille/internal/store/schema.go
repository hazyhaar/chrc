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

-- Chunks: text fragments for RAG consumption
CREATE TABLE IF NOT EXISTS chunks (
    id              TEXT PRIMARY KEY,
    extraction_id   TEXT NOT NULL REFERENCES extractions(id) ON DELETE CASCADE,
    source_id       TEXT NOT NULL,
    chunk_index     INTEGER NOT NULL,
    text            TEXT NOT NULL,
    token_count     INTEGER NOT NULL,
    overlap_prev    INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chunks_extraction ON chunks(extraction_id, chunk_index);
CREATE INDEX IF NOT EXISTS idx_chunks_source ON chunks(source_id);

-- FTS5 on chunks
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    text, content='chunks', content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);

-- Triggers to keep FTS5 in sync
CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, text) VALUES (new.rowid, new.text);
END;
CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, text) VALUES('delete', old.rowid, old.text);
END;
CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, text) VALUES('delete', old.rowid, old.text);
    INSERT INTO chunks_fts(rowid, text) VALUES (new.rowid, new.text);
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
`

// ApplySchema creates all tables and indexes on the given database.
func ApplySchema(db *sql.DB) error {
	_, err := db.Exec(Schema)
	return err
}
