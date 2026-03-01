# domkeeper (cmd/domkeeper) -- Binary Technical Schema

> CLI content extraction engine with FTS5 search, VTQ scheduling, and daemon mode

## Usage Modes

```
╔══════════════════════════════════════════════════════════════════════╗
║  domkeeper -config domkeeper.yaml       # YAML config, daemon mode ║
║  domkeeper -db domkeeper.db             # defaults, daemon mode    ║
║  domkeeper -db domkeeper.db -search "q" # one-shot search + exit   ║
║  domkeeper -db domkeeper.db -stats      # show stats + exit        ║
╚══════════════════════════════════════════════════════════════════════╝
```

## CLI Flags

```
╔═══════════════╦══════════╦═══════════════════════════════════════╗
║ Flag          ║ Default  ║ Purpose                               ║
╠═══════════════╬══════════╬═══════════════════════════════════════╣
║ -config       ║ ""       ║ Path to YAML config file              ║
║ -db           ║ ""       ║ Path to SQLite database               ║
║ -search       ║ ""       ║ Search query (one-shot mode)          ║
║ -stats        ║ false    ║ Show stats (one-shot mode)            ║
║ -log-level    ║ info     ║ debug/info/warn/error                 ║
║ -limit        ║ 20       ║ Max search results                    ║
╚═══════════════╩══════════╩═══════════════════════════════════════╝
```

## Startup Sequence (Daemon Mode)

```
╔══════════════════════════════════════════════════════════════════╗
║  1. Parse CLI flags                                             ║
║  2. Configure slog JSON handler                                 ║
║  3. signal.NotifyContext(SIGINT, SIGTERM)                       ║
║  4. resolveConfig(configPath, dbPath) -- YAML or defaults       ║
║  5. domkeeper.New(cfg, logger)                                  ║
║     ├── store.Open(dbPath) -- SQLite + full DDL schema          ║
║     ├── vtq.New(db, "domkeeper_refresh") -- VTQ queue           ║
║     ├── ingest.New(store, chunkOpts) -- consumer pipeline       ║
║     └── schedule.New(store, vtq, cfg) -- freshness scheduler    ║
║  6. k.Start(ctx) -- scheduler goroutine                         ║
║  7. <-ctx.Done() -- block until signal                          ║
║  8. k.Close() -- close DB                                       ║
╚══════════════════════════════════════════════════════════════════╝
```

## Architecture

```
       domwatch mutations (via Sink callback)
                    │
                    ▼
    ╔═══════════════════════════════════════╗
    ║           domkeeper.Keeper            ║
    ║  ┌────────────────────────────────┐   ║
    ║  │  ingest.Consumer               │   ║
    ║  │  ├── HandleBatch(mutation.Batch)│   ║
    ║  │  ├── HandleSnapshot(Snapshot)  │   ║
    ║  │  └── HandleProfile(Profile)    │   ║
    ║  │      ↓                         │   ║
    ║  │  extract (css/xpath/density/   │   ║
    ║  │          auto mode)            │   ║
    ║  │      ↓                         │   ║
    ║  │  chunk (512 tok, 64 overlap)   │   ║
    ║  │      ↓                         │   ║
    ║  │  store (FTS5, content_cache,   │   ║
    ║  │         chunks)                │   ║
    ║  └────────────────────────────────┘   ║
    ║  ┌────────────────────────────────┐   ║
    ║  │  schedule.Scheduler            │   ║
    ║  │  (polls every 5 min, checks    │   ║
    ║  │   freshness via VTQ queue)     │   ║
    ║  └────────────────────────────────┘   ║
    ║  ┌────────────────────────────────┐   ║
    ║  │  API surface:                  │   ║
    ║  │  ├── Search(query, opts)       │   ║
    ║  │  ├── PremiumSearch(multi-pass) │   ║
    ║  │  ├── AddRule / ListRules / Del │   ║
    ║  │  ├── AddFolder / ListFolders   │   ║
    ║  │  ├── Stats()                   │   ║
    ║  │  ├── GPUStats / GPUThreshold   │   ║
    ║  │  └── Sink() → domwatch.Sink    │   ║
    ║  └────────────────────────────────┘   ║
    ╚════════════════════╤══════════════════╝
                         │
                ╔════════╧════════╗
                ║ domkeeper.db    ║
                ║ (SQLite)        ║
                ╚═════════════════╝
```

## YAML Config Format

```yaml
db_path: domkeeper.db
chunk:
  max_tokens: 512
  overlap_tokens: 64
  min_chunk_tokens: 32
scheduler:
  check_interval: 5m
  default_freshness: 1h
  max_fail_count: 10
  visibility: 60s
  poll_interval: 5s
```

## MCP Tools (11 tools)

```
domkeeper_search          -- FTS5 search with folder/trust filters
domkeeper_premium_search  -- Multi-pass tiered search (free/premium)
domkeeper_add_rule        -- Create extraction rule
domkeeper_list_rules      -- List rules (optional enabled_only filter)
domkeeper_delete_rule     -- Delete rule + cascade content
domkeeper_add_folder      -- Create content folder
domkeeper_list_folders    -- List all folders
domkeeper_stats           -- Counts: rules, folders, content, chunks, pages
domkeeper_get_content     -- Full content + chunks by content_id
domkeeper_gpu_stats       -- GPU pricing + threshold data
domkeeper_gpu_threshold   -- Recompute serverless vs dedicated decision
```

## Connectivity Services (8 services)

```
domkeeper_search, domkeeper_premium_search, domkeeper_add_rule,
domkeeper_list_rules, domkeeper_delete_rule, domkeeper_stats,
domkeeper_gpu_stats, domkeeper_gpu_threshold
```

## Dependencies

```
domkeeper       → chunk, domwatch/mutation, extract
domkeeper       → pkg/vtq, pkg/idgen, pkg/kit, pkg/connectivity, pkg/dbopen
```
