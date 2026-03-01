# chrc ("cherche") -- Global Project Technical Schema

> Multi-source content acquisition platform -- web scraping, RSS, API, documents, DOM observation, search engine queries. Output = FTS5 extractions + .md buffer for RAG.

**Module**: `github.com/hazyhaar/chrc`
**Deployed**: https://veille.docbusinessia.fr (VPS BO, Basic Auth)
**Build**: `CGO_ENABLED=0 go build -v -o bin/chrc ./cmd/chrc/`

## High-Level Architecture

```
╔═══════════════════════════════════════════════════════════════════════════════════════╗
║                              chrc ECOSYSTEM                                          ║
║                                                                                      ║
║  ┌─────────────────────────────────────────────────────────────────────────────────┐  ║
║  │                         cmd/chrc (HTTP :8085)                                   │  ║
║  │  Veille HTTP SPA — JWT auth, chi router, usertenant multi-tenant pool           │  ║
║  │  Optional: MCP/QUIC :9444                                                       │  ║
║  │                                                                                  │  ║
║  │  ┌───────────────────────────────────────────────────────────────────────────┐   │  ║
║  │  │                       veille.Service                                      │   │  ║
║  │  │                                                                           │   │  ║
║  │  │  scheduler ──→ pipeline ──→ store ──→ FTS5 index                          │   │  ║
║  │  │  (1min poll)    │  │  │                  ↓                                │   │  ║
║  │  │                 │  │  └─→ buffer/pending/*.md (→ HORAG RAG pipeline)      │   │  ║
║  │  │                 │  │                                                       │   │  ║
║  │  │  sweeper ──→ repair (auto-backoff, auto-recovery)                         │   │  ║
║  │  │  (6h probe)                                                               │   │  ║
║  │  │                                                                           │   │  ║
║  │  │  Pipeline handlers:                                                       │   │  ║
║  │  │  ├── web       (HTTP GET → extract → dedup → store)                       │   │  ║
║  │  │  ├── rss       (RSS/Atom → per-entry dedup → optional follow_links)       │   │  ║
║  │  │  ├── api       (JSON API → apifetch → dedup → store)                      │   │  ║
║  │  │  ├── document  (local file → docpipe extract → dedup → store)             │   │  ║
║  │  │  ├── question  (tracked question → search engines → follow → store)       │   │  ║
║  │  │  ├── github    (GitHub API → connectivity bridge)                          │   │  ║
║  │  │  └── *_fetch   (any connectivity bridge, auto-discovered)                 │   │  ║
║  │  └───────────────────────────────────────────────────────────────────────────┘   │  ║
║  └─────────────────────────────────────────────────────────────────────────────────┘  ║
║                                                                                      ║
║  ┌───────────────────────┐  ┌──────────────────────┐  ┌─────────────────────────┐    ║
║  │  cmd/domwatch          │  │  cmd/domkeeper        │  │  (library-only)         │    ║
║  │  DOM observation daemon│  │  Content extraction    │  │                         │    ║
║  │  Chrome CDP, mutations │  │  engine, FTS5, VTQ     │  │  domregistry            │    ║
║  │  Stealth 0/1/2/auto   │──│  Rules, folders, chunks│  │  Community DOM profiles │    ║
║  │  Profiler, debouncer   │→ │  Premium search tiers  │  │  Corrections, reports   │    ║
║  │  Sinks: stdout/webhook │  │  GPU cost monitoring   │  │  Instance reputation    │    ║
║  └───────────────────────┘  └──────────────────────┘  └─────────────────────────┘    ║
║                                                                                      ║
║  ┌──────────────────┐  ┌──────────────────┐  ┌────────────────────────────────────┐  ║
║  │  extract/          │  │  vecbridge/       │  │  (migrated to pkg)                │  ║
║  │  HTML extraction   │  │  horosvec wrapper │  │  chunk/     → pkg/chunk            │  ║
║  │  css/xpath/density │  │  MCP + connectivity│  │  docpipe/  → pkg/docpipe           │  ║
║  │  auto mode         │  │  ANN vector index │  │  horosembed → pkg/horosembed       │  ║
║  └──────────────────┘  └──────────────────┘  └────────────────────────────────────┘  ║
╚═══════════════════════════════════════════════════════════════════════════════════════╝
```

## Data Flow: Source → Extraction → Buffer → RAG

```
    ╔══════════╗     ╔══════════╗     ╔══════════╗     ╔══════════╗
    ║  Source   ║     ║  Source   ║     ║  Source   ║     ║  Source   ║
    ║  (web)    ║     ║  (rss)    ║     ║  (api)    ║     ║(question)║
    ╚════╤═════╝     ╚════╤═════╝     ╚════╤═════╝     ╚════╤═════╝
         │                │                │                │
         ▼                ▼                ▼                ▼
    ╔════════════════════════════════════════════════════════════════╗
    ║                    pipeline.Pipeline                          ║
    ║  ┌────────┐ ┌────────┐ ┌────────┐ ┌──────────┐ ┌──────────┐║
    ║  │WebHdlr │ │RSSHdlr │ │APIHdlr │ │DocHdlr   │ │QuestHdlr│║
    ║  │fetch   │ │parse   │ │apifetch│ │docpipe   │ │search   ││║
    ║  │extract │ │entries │ │walk    │ │extract   │ │engines  ││║
    ║  │dedup   │ │dedup   │ │dedup   │ │dedup     │ │follow   ││║
    ║  │store   │ │follow? │ │store   │ │store     │ │dedup    ││║
    ║  │buffer  │ │store   │ │buffer  │ │buffer    │ │store    ││║
    ║  └────────┘ │buffer  │ └────────┘ └──────────┘ │buffer   ││║
    ║             └────────┘                          └──────────┘║
    ╚══════════╤════════════════════════════════════╤══════════════╝
               │                                    │
    ╔══════════╧══════════╗           ╔═════════════╧════════════╗
    ║  shard.db (per-      ║           ║  buffer/pending/          ║
    ║  dossier SQLite)     ║           ║  {uuid}.md               ║
    ║  ├── sources         ║           ║  ---                     ║
    ║  ├── extractions     ║           ║  id: ...                 ║
    ║  ├── extractions_fts ║           ║  source_id: ...          ║
    ║  ├── fetch_log       ║           ║  dossier_id: ...         ║
    ║  ├── search_engines  ║           ║  source_url: ...         ║
    ║  ├── tracked_questions║          ║  source_type: web        ║
    ║  └── search_log      ║           ║  title: ...              ║
    ╚══════════════════════╝           ║  extracted_at: RFC3339   ║
               │                       ║  content_hash: ...       ║
               │ FTS5 search           ║  ---                     ║
               ▼                       ║  [extracted markdown]    ║
       User queries via API            ╚════════════╤════════════╝
       or MCP tools                                 │
                                                    ▼
                                          HORAG RAG pipeline
                                          (chunk → embed → shard)
```

## Data Flow: domwatch → domkeeper

```
     ╔═══════════════════════╗
     ║  Web Page (live DOM)  ║
     ╚═══════════╤═══════════╝
                 │ Chrome CDP (DOM.enable)
     ╔═══════════╧═══════════════════════════╗
     ║         domwatch.Watcher              ║
     ║  ├── observer (CDP mutations)         ║
     ║  ├── debouncer (250ms window)         ║
     ║  ├── profiler (landmarks, zones)      ║
     ║  └── fetcher (HTTP-only fallback)     ║
     ╚═══════════╤═══════════════════════════╝
                 │ Batch / Snapshot / Profile
                 │ via CallbackSink (in-process)
     ╔═══════════╧═══════════════════════════╗
     ║         domkeeper.Keeper              ║
     ║  ├── ingest.Consumer                  ║
     ║  │   ├── match extraction rules       ║
     ║  │   ├── extract (css/xpath/density)  ║
     ║  │   ├── dedup (SHA-256 hash)         ║
     ║  │   ├── chunk (512 tok, 64 overlap)  ║
     ║  │   └── store (content_cache+chunks) ║
     ║  ├── schedule.Scheduler               ║
     ║  │   └── VTQ-based refresh queue      ║
     ║  └── premium.PremiumSearch            ║
     ║      └── multi-pass, trust-boost      ║
     ╚═══════════╤═══════════════════════════╝
                 │
     ╔═══════════╧══════╗
     ║  domkeeper.db    ║
     ╚══════════════════╝
```

## Data Flow: domkeeper ↔ domregistry

```
     ╔════════════════════╗         ╔═════════════════════════╗
     ║  domkeeper         ║         ║  domregistry             ║
     ║  (per-instance)    ║         ║  (shared registry)       ║
     ╠════════════════════╣         ╠═════════════════════════╣
     ║                    ║  Pull   ║                          ║
     ║  New domain found ─╫────────▶║  SearchProfiles(domain) ║
     ║  Import profile    ║◀────────╫─ Returns Profile         ║
     ║                    ║         ║                          ║
     ║  Auto-repair fixes ─╫────────▶║  SubmitCorrection()     ║
     ║  an extractor      ║         ║  auto-accept if         ║
     ║                    ║         ║  reputation is high     ║
     ║                    ║         ║                          ║
     ║  Extractor fails  ─╫────────▶║  ReportFailure()        ║
     ║  locally           ║         ║  → adjust success_rate  ║
     ╚════════════════════╝         ╚═════════════════════════╝
```

## ALL Database Schemas

### 1. Veille Shard DB (per-dossier, managed by usertenant)

```
╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: sources                                                           ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id                    TEXT PK                                            ║
║ name                  TEXT NOT NULL                                      ║
║ url                   TEXT NOT NULL  (UNIQUE INDEX idx_sources_url_unique)║
║ source_type           TEXT NOT NULL DEFAULT 'web'  -- web|rss|api|       ║
║                                                    -- document|question  ║
║ fetch_interval        INTEGER NOT NULL DEFAULT 3600000  (ms)             ║
║ enabled               INTEGER NOT NULL DEFAULT 1                        ║
║ config_json           TEXT NOT NULL DEFAULT '{}'                         ║
║ last_fetched_at       INTEGER (nullable)                                ║
║ last_hash             TEXT NOT NULL DEFAULT ''                           ║
║ last_status           TEXT NOT NULL DEFAULT 'pending'                    ║
║ last_error            TEXT NOT NULL DEFAULT ''                           ║
║ fail_count            INTEGER NOT NULL DEFAULT 0                        ║
║ original_fetch_interval INTEGER (nullable, migration 002)               ║
║ created_at            INTEGER NOT NULL                                  ║
║ updated_at            INTEGER NOT NULL                                  ║
║ INDEX idx_sources_enabled ON (enabled, last_fetched_at)                 ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: extractions                                                       ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id                    TEXT PK                                            ║
║ source_id             TEXT NOT NULL FK→sources(id) ON DELETE CASCADE     ║
║ content_hash          TEXT NOT NULL                                      ║
║ title                 TEXT NOT NULL DEFAULT ''                           ║
║ extracted_text        TEXT NOT NULL                                      ║
║ extracted_html        TEXT NOT NULL DEFAULT ''                           ║
║ url                   TEXT NOT NULL                                      ║
║ extracted_at          INTEGER NOT NULL                                  ║
║ metadata_json         TEXT NOT NULL DEFAULT '{}'                         ║
║ INDEX idx_extractions_source ON (source_id)                             ║
║ INDEX idx_extractions_time ON (extracted_at DESC)                       ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ VIRTUAL TABLE: extractions_fts USING fts5                                ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ title, extracted_text                                                    ║
║ content='extractions', content_rowid='rowid'                             ║
║ tokenize='unicode61 remove_diacritics 2'                                ║
║ Sync triggers: extractions_ai, extractions_ad, extractions_au           ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: fetch_log                                                         ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id                    TEXT PK                                            ║
║ source_id             TEXT NOT NULL FK→sources(id) ON DELETE CASCADE     ║
║ status                TEXT NOT NULL  -- ok|error|extract_error|unchanged ║
║ status_code           INTEGER (nullable)                                ║
║ content_hash          TEXT NOT NULL DEFAULT ''                           ║
║ error_message         TEXT NOT NULL DEFAULT ''                           ║
║ duration_ms           INTEGER NOT NULL DEFAULT 0                        ║
║ fetched_at            INTEGER NOT NULL                                  ║
║ INDEX idx_fetch_log_source ON (source_id, fetched_at DESC)              ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: search_engines (per-shard)                                        ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id                    TEXT PK                                            ║
║ name                  TEXT NOT NULL UNIQUE                               ║
║ strategy              TEXT NOT NULL DEFAULT 'api'  -- api|generic        ║
║ url_template          TEXT NOT NULL  -- e.g. "https://...?q={query}"     ║
║ api_config            TEXT NOT NULL DEFAULT '{}'                         ║
║ selectors             TEXT NOT NULL DEFAULT '{}'                         ║
║ stealth_level         INTEGER NOT NULL DEFAULT 1                        ║
║ rate_limit_ms         INTEGER NOT NULL DEFAULT 2000                     ║
║ max_pages             INTEGER NOT NULL DEFAULT 3                        ║
║ enabled               INTEGER NOT NULL DEFAULT 1                        ║
║ created_at            INTEGER NOT NULL                                  ║
║ updated_at            INTEGER NOT NULL                                  ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: tracked_questions (per-shard)                                     ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id                    TEXT PK                                            ║
║ text                  TEXT NOT NULL                                      ║
║ keywords              TEXT NOT NULL DEFAULT ''                           ║
║ channels              TEXT NOT NULL DEFAULT '[]'  -- JSON arr engine IDs ║
║ schedule_ms           INTEGER NOT NULL DEFAULT 86400000  (24h)          ║
║ max_results           INTEGER NOT NULL DEFAULT 20                       ║
║ follow_links          INTEGER NOT NULL DEFAULT 1                        ║
║ enabled               INTEGER NOT NULL DEFAULT 1                        ║
║ last_run_at           INTEGER (nullable)                                ║
║ last_result_count     INTEGER NOT NULL DEFAULT 0                        ║
║ total_results         INTEGER NOT NULL DEFAULT 0                        ║
║ created_at            INTEGER NOT NULL                                  ║
║ updated_at            INTEGER NOT NULL                                  ║
║ INDEX idx_tracked_questions_enabled ON (enabled, last_run_at)           ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: search_log (per-shard)                                            ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id                    TEXT PK                                            ║
║ query                 TEXT NOT NULL                                      ║
║ result_count          INTEGER NOT NULL DEFAULT 0                        ║
║ searched_at           INTEGER NOT NULL                                  ║
║ INDEX idx_search_log_time ON (searched_at DESC)                         ║
╚═══════════════════════════════════════════════════════════════════════════╝
```

### 2. Domkeeper DB (single SQLite file)

```
╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: folders                                                           ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id              TEXT PK                                                  ║
║ name            TEXT NOT NULL                                            ║
║ description     TEXT NOT NULL DEFAULT ''                                 ║
║ parent_id       TEXT FK→folders(id) ON DELETE SET NULL                   ║
║ created_at      INTEGER NOT NULL                                        ║
║ updated_at      INTEGER NOT NULL                                        ║
║ INDEX idx_folders_parent ON (parent_id)                                 ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: extraction_rules                                                  ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id              TEXT PK                                                  ║
║ name            TEXT NOT NULL                                            ║
║ url_pattern     TEXT NOT NULL  -- glob pattern                           ║
║ page_id         TEXT NOT NULL DEFAULT ''                                 ║
║ selectors       TEXT NOT NULL DEFAULT '[]'  -- JSON array                ║
║ extract_mode    TEXT NOT NULL DEFAULT 'auto' -- css|xpath|density|auto   ║
║ trust_level     TEXT NOT NULL DEFAULT 'unverified'                       ║
║ folder_id       TEXT FK→folders(id) ON DELETE SET NULL                   ║
║ enabled         INTEGER NOT NULL DEFAULT 1                              ║
║ priority        INTEGER NOT NULL DEFAULT 0                              ║
║ version         INTEGER NOT NULL DEFAULT 1                              ║
║ last_success    INTEGER (nullable)                                      ║
║ fail_count      INTEGER NOT NULL DEFAULT 0                              ║
║ created_at      INTEGER NOT NULL                                        ║
║ updated_at      INTEGER NOT NULL                                        ║
║ INDEX idx_rules_url ON (url_pattern)                                    ║
║ INDEX idx_rules_page ON (page_id) WHERE page_id != ''                   ║
║ INDEX idx_rules_folder ON (folder_id)                                   ║
║ INDEX idx_rules_enabled ON (enabled, priority DESC)                     ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: content_cache                                                     ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id              TEXT PK                                                  ║
║ rule_id         TEXT NOT NULL FK→extraction_rules(id) ON DELETE CASCADE  ║
║ page_url        TEXT NOT NULL                                            ║
║ page_id         TEXT NOT NULL DEFAULT ''                                 ║
║ snapshot_ref    TEXT NOT NULL DEFAULT ''                                 ║
║ content_hash    TEXT NOT NULL                                            ║
║ extracted_text  TEXT NOT NULL                                            ║
║ extracted_html  TEXT NOT NULL DEFAULT ''                                 ║
║ title           TEXT NOT NULL DEFAULT ''                                 ║
║ metadata        TEXT NOT NULL DEFAULT '{}'                               ║
║ trust_level     TEXT NOT NULL DEFAULT 'unverified'                       ║
║ extracted_at    INTEGER NOT NULL                                        ║
║ expires_at      INTEGER (nullable)                                      ║
║ INDEX idx_cache_rule ON (rule_id)                                       ║
║ INDEX idx_cache_url ON (page_url)                                       ║
║ INDEX idx_cache_hash ON (content_hash)                                  ║
║ INDEX idx_cache_expires ON (expires_at) WHERE expires_at IS NOT NULL    ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: chunks (RAG-ready text fragments)                                 ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id              TEXT PK                                                  ║
║ content_id      TEXT NOT NULL FK→content_cache(id) ON DELETE CASCADE     ║
║ chunk_index     INTEGER NOT NULL                                        ║
║ text            TEXT NOT NULL                                            ║
║ token_count     INTEGER NOT NULL                                        ║
║ overlap_prev    INTEGER NOT NULL DEFAULT 0                              ║
║ metadata        TEXT NOT NULL DEFAULT '{}'                               ║
║ created_at      INTEGER NOT NULL                                        ║
║ INDEX idx_chunks_content ON (content_id, chunk_index)                   ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ VIRTUAL TABLE: chunks_fts USING fts5                                     ║
║ text, content='chunks', content_rowid='rowid'                            ║
║ tokenize='unicode61 remove_diacritics 2'                                ║
║ Sync triggers: chunks_ai, chunks_ad, chunks_au                          ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: ingest_log                                                        ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id, batch_id, snapshot_id, page_url, page_id                            ║
║ status (pending|ok|error), error_message                                ║
║ records_count, extracted_count, created_at, completed_at                ║
║ INDEX idx_ingest_status, idx_ingest_page, idx_ingest_time               ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: source_pages                                                      ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ page_id TEXT PK, page_url TEXT NOT NULL UNIQUE                           ║
║ trust_level, profile_json, last_seen, created_at                        ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: gpu_pricing (serverless GPU burst cost tracking)                   ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id, provider, gpu_model, cost_per_sec, min_commit_hrs                   ║
║ throughput, cost_per_unit, is_dedicated, status, measured_at             ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: gpu_threshold (serverless vs dedicated decision)                   ║
║ id DEFAULT 'default', backlog_units, serverless_cost, dedicated_cost    ║
║ decision ('serverless'|'dedicated'), computed_at                         ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: search_tiers (premium vs free search analytics)                   ║
║ id, user_id, tier (free|premium), query, passes                         ║
║ results_count, latency_ms, created_at                                   ║
╚═══════════════════════════════════════════════════════════════════════════╝

+ VTQ table: vtq_domkeeper_refresh (auto-created by pkg/vtq)
```

### 3. Domregistry DB (single SQLite file)

```
╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: profiles (community DOM profiles)                                 ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id              TEXT PK                                                  ║
║ url_pattern     TEXT NOT NULL UNIQUE                                     ║
║ domain          TEXT NOT NULL                                            ║
║ schema_id       TEXT NOT NULL DEFAULT ''                                 ║
║ extractors      TEXT NOT NULL  -- JSON: extraction strategies            ║
║ dom_profile     TEXT NOT NULL  -- JSON: landmarks, zones, fingerprint    ║
║ trust_level     TEXT NOT NULL DEFAULT 'community'                        ║
║ success_rate    REAL NOT NULL DEFAULT 0.0                                ║
║ total_uses      INTEGER NOT NULL DEFAULT 0                              ║
║ total_repairs   INTEGER NOT NULL DEFAULT 0                              ║
║ contributors    TEXT NOT NULL DEFAULT '[]'  -- JSON array of instance IDs║
║ created_at      INTEGER NOT NULL                                        ║
║ updated_at      INTEGER NOT NULL                                        ║
║ INDEX idx_profiles_domain, idx_profiles_trust, idx_profiles_success     ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: corrections (audit trail of profile changes)                      ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id, profile_id FK→profiles, instance_id                                 ║
║ old_extractors, new_extractors, reason, validated (0|1), created_at     ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: reports (failure signals from instances)                           ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ id, profile_id FK→profiles, instance_id                                 ║
║ error_type, message, created_at                                         ║
╚═══════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════╗
║ TABLE: instance_reputation                                               ║
╠═══════════════════════════════════════════════════════════════════════════╣
║ instance_id TEXT PK                                                      ║
║ corrections_accepted, corrections_rejected, corrections_pending          ║
║ domains_covered, last_active, created_at                                ║
╚═══════════════════════════════════════════════════════════════════════════╝
```

### 4. Catalog DB (catalog.db -- global, managed by cmd/chrc)

```
See cmd/chrc/chrc_schem.md for: users, shards, global_search_engines, source_registry
Also: audit_log (from pkg/audit), rate_limits (from pkg/ratelimit)
```

## ALL MCP Tools (32 total)

```
╔════════════════════════════════════════════════════════════════════════════╗
║ VEILLE (15 tools) -- registered via veille.RegisterMCP()                 ║
╠════════════════════════════════════════════════════════════════════════════╣
║ veille_add_source         veille_list_sources       veille_update_source ║
║ veille_delete_source      veille_fetch_now          veille_search        ║
║ veille_list_extractions   veille_stats              veille_fetch_history ║
║ veille_add_question       veille_list_questions     veille_update_question║
║ veille_delete_question    veille_run_question       veille_question_results║
╠════════════════════════════════════════════════════════════════════════════╣
║ DOMKEEPER (11 tools) -- registered via domkeeper.RegisterMCP()           ║
╠════════════════════════════════════════════════════════════════════════════╣
║ domkeeper_search          domkeeper_premium_search  domkeeper_add_rule   ║
║ domkeeper_list_rules      domkeeper_delete_rule     domkeeper_add_folder ║
║ domkeeper_list_folders    domkeeper_stats           domkeeper_get_content║
║ domkeeper_gpu_stats       domkeeper_gpu_threshold                       ║
╠════════════════════════════════════════════════════════════════════════════╣
║ DOMREGISTRY (6 tools) -- registered via domregistry.RegisterMCP()        ║
╠════════════════════════════════════════════════════════════════════════════╣
║ domregistry_search_profiles    domregistry_submit_correction            ║
║ domregistry_report_failure     domregistry_leaderboard                  ║
║ domregistry_stats              domregistry_publish_profile              ║
╚════════════════════════════════════════════════════════════════════════════╝
```

## ALL Connectivity Services (34 total)

```
╔════════════════════════════════════════════════════════════════════════════╗
║ VEILLE (17 services) -- veille.RegisterConnectivity() + 2 external      ║
╠════════════════════════════════════════════════════════════════════════════╣
║ github_fetch, api_fetch  (external, registered in cmd/chrc/main.go)     ║
║ veille_add_source, veille_list_sources, veille_update_source,           ║
║ veille_delete_source, veille_fetch_now, veille_search,                  ║
║ veille_list_extractions, veille_stats, veille_fetch_history,            ║
║ veille_add_question, veille_list_questions, veille_update_question,     ║
║ veille_delete_question, veille_run_question, veille_question_results    ║
╠════════════════════════════════════════════════════════════════════════════╣
║ DOMKEEPER (8 services) -- domkeeper.RegisterConnectivity()              ║
╠════════════════════════════════════════════════════════════════════════════╣
║ domkeeper_search, domkeeper_premium_search, domkeeper_add_rule,         ║
║ domkeeper_list_rules, domkeeper_delete_rule, domkeeper_stats,           ║
║ domkeeper_gpu_stats, domkeeper_gpu_threshold                            ║
╠════════════════════════════════════════════════════════════════════════════╣
║ DOMREGISTRY (7 services) -- domregistry.RegisterConnectivity()          ║
╠════════════════════════════════════════════════════════════════════════════╣
║ domregistry_search_profiles, domregistry_get_profile,                   ║
║ domregistry_publish_profile, domregistry_submit_correction,             ║
║ domregistry_report_failure, domregistry_leaderboard, domregistry_stats  ║
╠════════════════════════════════════════════════════════════════════════════╣
║ DOMWATCH (2 services) -- domwatch.RegisterConnectivity()                ║
╠════════════════════════════════════════════════════════════════════════════╣
║ domwatch_observe, domwatch_profile                                      ║
╚════════════════════════════════════════════════════════════════════════════╝
```

## Dependencies on hazyhaar/pkg Sub-packages

```
╔══════════════════════╦════════════════════════════════════════════════════╗
║ pkg sub-package      ║ Used by                                           ║
╠══════════════════════╬════════════════════════════════════════════════════╣
║ pkg/audit            ║ cmd/chrc (SQLite audit logger)                    ║
║ pkg/auth             ║ cmd/chrc (JWT, cookies, middleware)               ║
║ pkg/connectivity     ║ cmd/chrc, veille, domkeeper, domregistry, domwatch║
║ pkg/dbopen           ║ cmd/chrc, vecbridge (SQLite open w/ pragmas)      ║
║ pkg/docpipe          ║ veille/pipeline (document handler)                ║
║ pkg/horosafe         ║ cmd/chrc, veille (SSRF/path validation)           ║
║ pkg/idgen            ║ everywhere (UUID v7)                              ║
║ pkg/kit              ║ veille, domkeeper, domregistry (MCP tool reg.)    ║
║ pkg/mcpquic          ║ cmd/chrc (optional QUIC listener)                 ║
║ pkg/ratelimit        ║ cmd/chrc (login rate limiting)                    ║
║ pkg/redact           ║ cmd/chrc (env var redaction)                      ║
║ pkg/shield           ║ cmd/chrc (security middleware stack)              ║
║ pkg/trace            ║ cmd/chrc (SQL tracing)                            ║
║ pkg/vtq              ║ domkeeper (VTQ-based refresh queue)               ║
╚══════════════════════╩════════════════════════════════════════════════════╝
```

## External Dependencies

```
╔══════════════════════════════════════╦════════════════════════════════════╗
║ Module                               ║ Purpose                           ║
╠══════════════════════════════════════╬════════════════════════════════════╣
║ go-chi/chi/v5                        ║ HTTP router (cmd/chrc)            ║
║ go-rod/rod + go-rod/stealth          ║ Chrome automation (domwatch)      ║
║ hazyhaar/horosvec                    ║ ANN vector index (vecbridge)      ║
║ hazyhaar/usertenant                  ║ Multi-tenant SQLite pool          ║
║ modelcontextprotocol/go-sdk          ║ MCP server SDK                    ║
║ pdfcpu/pdfcpu                        ║ PDF processing (docpipe)          ║
║ JohannesKaufmann/html-to-markdown/v2 ║ HTML→Markdown (buffer writer)     ║
║ golang.org/x/crypto                  ║ bcrypt (auth)                     ║
║ golang.org/x/net                     ║ HTML parser (extract)             ║
║ gopkg.in/yaml.v3                     ║ YAML config (domkeeper, domwatch) ║
║ modernc.org/sqlite                   ║ Pure-Go SQLite driver             ║
╚══════════════════════════════════════╩════════════════════════════════════╝
```

## File Layout on Disk

```
chrc/
├── CLAUDE.md                          # Project manifest
├── chrc_schem.md                      # THIS FILE
├── Makefile                           # build / test / clean
├── go.mod / go.sum
│
├── cmd/
│   ├── chrc/
│   │   ├── main.go                    # Veille HTTP SPA (1502 lines)
│   │   ├── chrc_schem.md             # Binary schema
│   │   └── static/                    # Embedded SPA assets
│   ├── domkeeper/
│   │   ├── main.go                    # CLI extraction engine
│   │   └── domkeeper_schem.md        # Binary schema
│   └── domwatch/
│       ├── main.go                    # DOM observation daemon
│       └── domwatch_schem.md         # Binary schema
│
├── veille/                            # Core veille service
│   ├── veille.go                      # Service orchestrator
│   ├── config.go                      # Config struct
│   ├── types.go                       # Re-exported store types
│   ├── errors.go                      # Sentinel errors
│   ├── validate.go                    # Input validation
│   ├── normalize.go                   # URL normalization for dedup
│   ├── mcp.go                         # 15 MCP tools
│   ├── connectivity.go                # 15 connectivity handlers
│   ├── api.go                         # NewAPIService (connectivity)
│   ├── github.go                      # NewGitHubService (connectivity)
│   ├── migrate_dedup.go               # URL normalization migration
│   ├── catalog/
│   │   └── catalog.go                 # Seed catalog (sources + engines)
│   └── internal/
│       ├── pipeline/
│       │   ├── pipeline.go            # Handler dispatch orchestrator
│       │   ├── handler.go             # SourceHandler interface
│       │   ├── handler_web.go         # HTTP fetch → extract → store
│       │   ├── handler_rss.go         # RSS/Atom → per-entry dedup
│       │   ├── handler_api.go         # JSON API fetch
│       │   ├── handler_document.go    # Local file via docpipe
│       │   ├── handler_question.go    # Tracked question → search engines
│       │   ├── handler_connectivity.go# ConnectivityBridge + DiscoverHandlers
│       │   ├── api_service.go         # api_fetch connectivity handler
│       │   └── github_service.go      # github_fetch connectivity handler
│       ├── store/
│       │   ├── schema.go              # Full DDL + migrations
│       │   ├── types.go               # Source, Extraction, etc.
│       │   ├── store.go               # Store wrapper
│       │   ├── source.go              # Source CRUD
│       │   ├── extraction.go          # Extraction insert/query
│       │   ├── fetch_log.go           # Fetch log insert/query
│       │   ├── search.go              # FTS5 search
│       │   ├── search_engine.go       # Search engine CRUD
│       │   ├── question.go            # Tracked question CRUD
│       │   └── stats.go               # Aggregate counters
│       ├── scheduler/
│       │   └── scheduler.go           # Poll-based source scheduler
│       ├── buffer/
│       │   └── writer.go              # Atomic .md file writer
│       ├── feed/
│       │   └── feed.go                # RSS 2.0 + Atom parser
│       ├── fetch/
│       │   └── fetcher.go             # HTTP fetcher with conditional GET
│       ├── apifetch/
│       │   └── apifetch.go            # JSON API fetch + dot-notation walk
│       ├── search/
│       │   └── search.go              # Search engine abstraction
│       ├── question/
│       │   └── runner.go              # Question runner (search→follow→store)
│       └── repair/
│           ├── repair.go              # Auto-repair (classify+backoff)
│           ├── classify.go            # Error classification
│           └── sweep.go               # Periodic broken source probe
│
├── domkeeper/                         # Content extraction engine
│   ├── keeper.go                      # Orchestrator (store, consumer, scheduler)
│   ├── config.go                      # YAML config
│   ├── types.go                       # Re-exported store types
│   ├── mcp.go                         # 11 MCP tools
│   ├── connectivity.go                # 8 connectivity handlers
│   ├── sink.go                        # domwatch.Sink callback factory
│   ├── premium.go                     # Multi-pass tiered search + GPU monitoring
│   └── internal/
│       ├── store/
│       │   ├── schema.go              # Full DDL (11 tables)
│       │   ├── store.go, cache.go, chunk.go, folder.go
│       │   ├── gpu.go, ingest.go, premium.go, rule.go, search.go
│       ├── extract/
│       │   ├── extract.go, css.go, xpath.go, density.go, clean.go
│       ├── chunk/
│       │   └── chunk.go               # Overlapping text chunker
│       ├── ingest/
│       │   └── consumer.go            # Batch/snapshot/profile consumer
│       └── schedule/
│           └── scheduler.go           # VTQ-based freshness scheduler
│
├── domwatch/                          # DOM observation daemon
│   ├── watcher.go                     # Top-level orchestrator
│   ├── config.go                      # Re-exported config types
│   ├── sinks.go                       # Sink factory functions
│   ├── mutation/                      # Public types (contract)
│   │   ├── batch.go                   # Batch{Records[Op,XPath,...]}
│   │   ├── snapshot.go                # Snapshot{HTML, HTMLHash}
│   │   ├── profile.go                 # Profile{Landmarks,Zones,...}
│   │   └── serialize.go               # JSON marshal helpers
│   └── internal/
│       ├── browser/                   # Chrome lifecycle
│       │   ├── manager.go, tab.go, resources.go, xvfb.go
│       ├── config/
│       │   ├── file.go (YAML), db.go (SQLite config)
│       ├── fetcher/
│       │   ├── fetcher.go, detect.go (content sufficiency)
│       ├── observer/
│       │   ├── observer.go, cdpdom.go, debounce.go
│       │   ├── dedup.go, shadow.go, spa.go, xpath.go
│       ├── profiler/
│       │   ├── profiler.go, density.go, fingerprint.go, landmarks.go
│       └── sink/
│           ├── sink.go (interface), router.go (fan-out)
│           ├── stdout.go, webhook.go, callback.go
│
├── domregistry/                       # Community DOM profile registry
│   ├── registry.go                    # Orchestrator
│   ├── config.go                      # Config + defaults
│   ├── types.go                       # Re-exported store types
│   ├── mcp.go                         # 6 MCP tools
│   ├── connectivity.go                # 7 connectivity handlers
│   ├── leaderboard.go                 # Domain/instance leaderboards
│   └── internal/store/
│       ├── schema.go                  # Full DDL (4 tables)
│       ├── store.go, profile.go, correction.go
│
├── extract/                           # Shared HTML extraction
│   ├── extract.go                     # Entry point + auto mode
│   ├── css.go                         # CSS selector extraction
│   ├── xpath.go                       # XPath extraction
│   ├── density.go                     # Text-density-based extraction
│   └── clean.go                       # Text cleaning + normalization
│
├── vecbridge/                         # horosvec ANN wrapper
│   ├── vecbridge.go                   # Service + SQLite + dbopen
│   ├── mcp.go                         # MCP tools
│   └── connectivity.go               # Connectivity handlers
│
├── chunk/    → MIGRATED to pkg/chunk
├── docpipe/  → MIGRATED to pkg/docpipe
├── horosembed → MIGRATED to pkg/horosembed
│
├── e2e/                               # Cross-package integration tests
└── bin/                               # Build artifacts
```

## Key Types and Their Relationships

```
╔════════════════════════════════════════════════════════════════════════╗
║                         TYPE HIERARCHY                                ║
║                                                                       ║
║  veille.Service                                                       ║
║  ├── pool: PoolResolver (usertenant.Pool.Resolve)                    ║
║  ├── pipeline: pipeline.Pipeline                                     ║
║  │   ├── handlers: map[string]SourceHandler                          ║
║  │   │   ├── "web"      → WebHandler                                 ║
║  │   │   ├── "rss"      → RSSHandler                                ║
║  │   │   ├── "api"      → APIHandler (or ConnectivityBridge)         ║
║  │   │   ├── "document" → DocumentHandler (uses pkg/docpipe)         ║
║  │   │   ├── "question" → QuestionHandler (uses question.Runner)     ║
║  │   │   └── "*_fetch"  → ConnectivityBridge (auto-discovered)       ║
║  │   ├── buffer: buffer.Writer → .md files                           ║
║  │   └── mdConverter: html-to-markdown                               ║
║  ├── scheduler: scheduler.Scheduler                                  ║
║  ├── repairer: repair.Repairer                                       ║
║  ├── sweeper: repair.Sweeper                                         ║
║  └── router: connectivity.Router                                     ║
║                                                                       ║
║  domkeeper.Keeper                                                    ║
║  ├── store: store.Store (SQLite wrapper)                             ║
║  ├── consumer: ingest.Consumer                                       ║
║  ├── scheduler: schedule.Scheduler                                   ║
║  └── queue: vtq.Q ("domkeeper_refresh")                              ║
║                                                                       ║
║  domwatch.Watcher                                                    ║
║  ├── mgr: browser.Manager (Chrome lifecycle)                         ║
║  ├── observers: map[pageID]*observer.Observer                        ║
║  ├── sinkR: sink.Router                                              ║
║  │   ├── StdoutSink / WebhookSink / CallbackSink                    ║
║  └── fetch: fetcher.Fetcher                                          ║
║                                                                       ║
║  domregistry.Registry                                                ║
║  └── store: store.Store (profiles, corrections, reports, reputation) ║
║                                                                       ║
║  vecbridge.Service                                                   ║
║  ├── Index: horosvec.Index                                           ║
║  └── db: *sql.DB                                                     ║
╚════════════════════════════════════════════════════════════════════════╝
```

## Sub-Schema Files Index

```
cmd/chrc/chrc_schem.md            -- Veille HTTP SPA binary
cmd/domkeeper/domkeeper_schem.md  -- Domkeeper CLI binary
cmd/domwatch/domwatch_schem.md    -- Domwatch daemon binary
chrc_schem.md                     -- THIS FILE (global project)
```
