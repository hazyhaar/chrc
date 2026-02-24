package store

// Schema contains the complete DDL for the domregistry tables.
const Schema = `
-- Community DOM profiles: URL patterns + extraction strategies shared across instances
CREATE TABLE IF NOT EXISTS profiles (
    id              TEXT PRIMARY KEY,
    url_pattern     TEXT NOT NULL UNIQUE,
    domain          TEXT NOT NULL,
    schema_id       TEXT NOT NULL DEFAULT '',
    extractors      TEXT NOT NULL,
    dom_profile     TEXT NOT NULL,
    trust_level     TEXT NOT NULL DEFAULT 'community',
    success_rate    REAL NOT NULL DEFAULT 0.0,
    total_uses      INTEGER NOT NULL DEFAULT 0,
    total_repairs   INTEGER NOT NULL DEFAULT 0,
    contributors    TEXT NOT NULL DEFAULT '[]',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_profiles_domain ON profiles(domain);
CREATE INDEX IF NOT EXISTS idx_profiles_trust ON profiles(trust_level);
CREATE INDEX IF NOT EXISTS idx_profiles_success ON profiles(success_rate DESC);

-- Corrections: proposed changes to profile extractors (audit trail)
CREATE TABLE IF NOT EXISTS corrections (
    id              TEXT PRIMARY KEY,
    profile_id      TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    instance_id     TEXT NOT NULL,
    old_extractors  TEXT NOT NULL,
    new_extractors  TEXT NOT NULL,
    reason          TEXT NOT NULL,
    validated       INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_corrections_profile ON corrections(profile_id);
CREATE INDEX IF NOT EXISTS idx_corrections_status ON corrections(validated);
CREATE INDEX IF NOT EXISTS idx_corrections_instance ON corrections(instance_id);

-- Reports: failure signals from instances
CREATE TABLE IF NOT EXISTS reports (
    id              TEXT PRIMARY KEY,
    profile_id      TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    instance_id     TEXT NOT NULL,
    error_type      TEXT NOT NULL DEFAULT '',
    message         TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_reports_profile ON reports(profile_id);
CREATE INDEX IF NOT EXISTS idx_reports_time ON reports(created_at DESC);

-- Instance reputation: tracks contribution quality per instance
CREATE TABLE IF NOT EXISTS instance_reputation (
    instance_id         TEXT PRIMARY KEY,
    corrections_accepted INTEGER NOT NULL DEFAULT 0,
    corrections_rejected INTEGER NOT NULL DEFAULT 0,
    corrections_pending  INTEGER NOT NULL DEFAULT 0,
    domains_covered     INTEGER NOT NULL DEFAULT 0,
    last_active         INTEGER NOT NULL,
    created_at          INTEGER NOT NULL
);
`
