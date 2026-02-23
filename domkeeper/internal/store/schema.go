package store

// Schema contains the complete DDL for the domkeeper tables.
const Schema = `
-- Folders: logical groupings of extracted content (knowledge base)
CREATE TABLE IF NOT EXISTS folders (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    parent_id   TEXT,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    FOREIGN KEY (parent_id) REFERENCES folders(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_folders_parent ON folders(parent_id);

-- Extraction rules: how to extract content from a page
CREATE TABLE IF NOT EXISTS extraction_rules (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    url_pattern  TEXT NOT NULL,
    page_id      TEXT NOT NULL DEFAULT '',
    selectors    TEXT NOT NULL DEFAULT '[]',
    extract_mode TEXT NOT NULL DEFAULT 'auto',
    trust_level  TEXT NOT NULL DEFAULT 'unverified',
    folder_id    TEXT,
    enabled      INTEGER NOT NULL DEFAULT 1,
    priority     INTEGER NOT NULL DEFAULT 0,
    version      INTEGER NOT NULL DEFAULT 1,
    last_success INTEGER,
    fail_count   INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_rules_url ON extraction_rules(url_pattern);
CREATE INDEX IF NOT EXISTS idx_rules_page ON extraction_rules(page_id) WHERE page_id != '';
CREATE INDEX IF NOT EXISTS idx_rules_folder ON extraction_rules(folder_id);
CREATE INDEX IF NOT EXISTS idx_rules_enabled ON extraction_rules(enabled, priority DESC);

-- Content cache: extracted content from pages
CREATE TABLE IF NOT EXISTS content_cache (
    id             TEXT PRIMARY KEY,
    rule_id        TEXT NOT NULL,
    page_url       TEXT NOT NULL,
    page_id        TEXT NOT NULL DEFAULT '',
    snapshot_ref   TEXT NOT NULL DEFAULT '',
    content_hash   TEXT NOT NULL,
    extracted_text TEXT NOT NULL,
    extracted_html TEXT NOT NULL DEFAULT '',
    title          TEXT NOT NULL DEFAULT '',
    metadata       TEXT NOT NULL DEFAULT '{}',
    trust_level    TEXT NOT NULL DEFAULT 'unverified',
    extracted_at   INTEGER NOT NULL,
    expires_at     INTEGER,
    FOREIGN KEY (rule_id) REFERENCES extraction_rules(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_cache_rule ON content_cache(rule_id);
CREATE INDEX IF NOT EXISTS idx_cache_url ON content_cache(page_url);
CREATE INDEX IF NOT EXISTS idx_cache_hash ON content_cache(content_hash);
CREATE INDEX IF NOT EXISTS idx_cache_expires ON content_cache(expires_at) WHERE expires_at IS NOT NULL;

-- Text chunks for RAG / search
CREATE TABLE IF NOT EXISTS chunks (
    id            TEXT PRIMARY KEY,
    content_id    TEXT NOT NULL,
    chunk_index   INTEGER NOT NULL,
    text          TEXT NOT NULL,
    token_count   INTEGER NOT NULL,
    overlap_prev  INTEGER NOT NULL DEFAULT 0,
    metadata      TEXT NOT NULL DEFAULT '{}',
    created_at    INTEGER NOT NULL,
    FOREIGN KEY (content_id) REFERENCES content_cache(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_chunks_content ON chunks(content_id, chunk_index);

-- FTS5 full-text search index
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    text,
    content='chunks',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);

-- Triggers to keep FTS5 in sync with chunks table
CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, text) VALUES (new.rowid, new.text);
END;
CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
END;
CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
    INSERT INTO chunks_fts(rowid, text) VALUES (new.rowid, new.text);
END;

-- Ingestion log: tracks each batch/snapshot processing
CREATE TABLE IF NOT EXISTS ingest_log (
    id              TEXT PRIMARY KEY,
    batch_id        TEXT NOT NULL DEFAULT '',
    snapshot_id     TEXT NOT NULL DEFAULT '',
    page_url        TEXT NOT NULL,
    page_id         TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',
    error_message   TEXT NOT NULL DEFAULT '',
    records_count   INTEGER NOT NULL DEFAULT 0,
    extracted_count INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    completed_at    INTEGER
);
CREATE INDEX IF NOT EXISTS idx_ingest_status ON ingest_log(status);
CREATE INDEX IF NOT EXISTS idx_ingest_page ON ingest_log(page_url);
CREATE INDEX IF NOT EXISTS idx_ingest_time ON ingest_log(created_at DESC);

-- Source pages: known pages tracked by domkeeper
CREATE TABLE IF NOT EXISTS source_pages (
    page_id      TEXT PRIMARY KEY,
    page_url     TEXT NOT NULL UNIQUE,
    trust_level  TEXT NOT NULL DEFAULT 'unverified',
    profile_json TEXT NOT NULL DEFAULT '{}',
    last_seen    INTEGER NOT NULL,
    created_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_source_url ON source_pages(page_url);
`
