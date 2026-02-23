-- Gateway Service Schema
-- Sas IN/OUT unifié pour flux HORUM ↔ Edge

-- Dynamic configuration (replaces all hardcoded paths)
CREATE TABLE IF NOT EXISTS config_params (
    param_key TEXT PRIMARY KEY,
    param_value TEXT NOT NULL,
    description TEXT,
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

INSERT OR IGNORE INTO config_params (param_key, param_value, description) VALUES
('data_dir',             '/inference/horos47/data', 'Root data directory'),
('staging_dir',          '/inference/staging',      'Temp staging for workflows'),
('horum_callback_url',   '',                        'HORUM result callback URL (set at runtime)'),
('gateway_listen_addr',  ':8443',                   'QUIC/HTTP3 listen address');

-- Task envelopes: every agent task flows through here
CREATE TABLE IF NOT EXISTS task_envelopes (
    envelope_id BLOB PRIMARY KEY,                    -- UUID v7
    origin_mention_id TEXT NOT NULL,                  -- post_mentions.mention_id from HORUM
    origin_node_id TEXT,                              -- node_id of source post
    origin_user_id TEXT NOT NULL,                     -- user who triggered
    agent_name TEXT NOT NULL,                         -- sources, syntheses, lexique...
    workflow_name TEXT,                               -- resolved by router from workflow_definitions
    payload_json TEXT NOT NULL DEFAULT '{}',          -- content: post text, attachments, context
    provenance_json TEXT NOT NULL DEFAULT '{}',       -- full chain: parents, user, timestamp
    status TEXT NOT NULL CHECK(status IN (
        'received','routing','awaiting_clarification','processing','completed','failed','dispatched'
    )) DEFAULT 'received',
    result_json TEXT,                                 -- workflow result
    error_message TEXT,
    priority INTEGER DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    started_at INTEGER,
    completed_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_envelopes_status ON task_envelopes(status, created_at)
    WHERE status IN ('received','routing','awaiting_clarification','processing');
CREATE INDEX IF NOT EXISTS idx_envelopes_agent ON task_envelopes(agent_name, status);

-- Clarification requests: intent clarification before workflow processing
CREATE TABLE IF NOT EXISTS clarification_requests (
    request_id BLOB PRIMARY KEY,             -- UUID v7
    envelope_id BLOB NOT NULL REFERENCES task_envelopes(envelope_id),
    detected_uncertainties TEXT NOT NULL,     -- JSON [{type, severity, evidence, suggestion}]
    questions TEXT NOT NULL,                  -- JSON [{question_id, header, text, options, allow_other}]
    status TEXT NOT NULL CHECK(status IN ('pending','answered','expired','cancelled'))
        DEFAULT 'pending',
    answers TEXT,                             -- JSON user answers
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    expires_at INTEGER NOT NULL,             -- 24h default
    answered_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_clarif_envelope ON clarification_requests(envelope_id);
CREATE INDEX IF NOT EXISTS idx_clarif_pending ON clarification_requests(status, expires_at)
    WHERE status = 'pending';

-- Chunked payloads: fragmented file reception for large attachments
CREATE TABLE IF NOT EXISTS chunked_payloads (
    payload_id BLOB PRIMARY KEY,          -- UUID v7
    envelope_id BLOB NOT NULL REFERENCES task_envelopes(envelope_id),
    total_chunks INTEGER NOT NULL,
    received_chunks INTEGER NOT NULL DEFAULT 0,
    file_size INTEGER NOT NULL,
    file_sha256 TEXT NOT NULL,
    storage_dir TEXT NOT NULL,             -- staging directory for reconstruction
    status TEXT NOT NULL CHECK(status IN ('receiving','complete','failed'))
        DEFAULT 'receiving',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    completed_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_chunked_envelope ON chunked_payloads(envelope_id);
