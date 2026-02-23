PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS gpu_jobs (
    -- Identité (UUID v7 = triable temporellement)
    id BLOB PRIMARY KEY,

    -- Idempotence
    payload_sha256 TEXT UNIQUE NOT NULL,

    -- Fractionnement (fan-in)
    parent_id BLOB,                         -- UUID v7 du job parent
    fragment_index INTEGER,                 -- 1-indexed
    total_fragments INTEGER DEFAULT 1,

    -- Routing GPU
    model_type TEXT NOT NULL,               -- 'vision' | 'think'

    -- Payload/Result paths
    payload_path TEXT NOT NULL,             -- /data/stage_vision/pending/{id}.json
    result_path TEXT,                       -- /data/stage_vision/done/{id}.json

    -- État
    status TEXT NOT NULL DEFAULT 'pending',
    -- 'pending'    : Attend dispatch
    -- 'processing' : Dans un batch en cours
    -- 'done'       : Résultat écrit
    -- 'failed'     : Échec (retry possible)
    -- 'poison'     : Échec définitif (3x)

    -- Batch tracking
    batch_id BLOB,                          -- UUID v7 du batch actuel

    -- Timestamps
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    completed_at INTEGER,

    -- Error handling
    attempts INTEGER DEFAULT 0,
    max_attempts INTEGER DEFAULT 3,
    last_error TEXT,

    -- Déduplication sémantique Think (hash du prompt, pas du payload complet)
    prompt_hash TEXT,

    CHECK (status IN ('pending', 'processing', 'done', 'failed', 'poison')),
    CHECK (model_type IN ('vision', 'think', 'embed')),
    FOREIGN KEY (parent_id) REFERENCES gpu_jobs(id)
);

-- Index pour dispatch rapide
CREATE INDEX IF NOT EXISTS idx_pending_model ON gpu_jobs(model_type, status, created_at)
    WHERE status = 'pending';

-- Index pour fan-in
CREATE INDEX IF NOT EXISTS idx_fanin ON gpu_jobs(parent_id, status)
    WHERE parent_id IS NOT NULL;

-- Index pour batch tracking
CREATE INDEX IF NOT EXISTS idx_batch ON gpu_jobs(batch_id)
    WHERE batch_id IS NOT NULL;

-- Index unique partiel pour déduplication sémantique Think
CREATE UNIQUE INDEX IF NOT EXISTS idx_gpu_jobs_prompt_hash
    ON gpu_jobs(prompt_hash)
    WHERE prompt_hash IS NOT NULL AND status IN ('processing', 'done');

-- Vue poison pills
CREATE VIEW IF NOT EXISTS v_poison_pills AS
SELECT id, model_type, payload_path, last_error, attempts
FROM gpu_jobs WHERE status = 'poison';

-- Vue stats
CREATE VIEW IF NOT EXISTS v_stats AS
SELECT
    model_type,
    status,
    COUNT(*) as count,
    AVG(CASE WHEN completed_at IS NOT NULL THEN completed_at - started_at END) as avg_duration_sec
FROM gpu_jobs
GROUP BY model_type, status;
