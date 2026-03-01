# chrc (cmd/chrc) -- Binary Technical Schema

> Veille HTTP service -- multi-tenant content monitoring SPA with JWT auth, MCP/QUIC, usertenant pool

## Startup Sequence

```
╔══════════════════════════════════════════════════════════════════════════════╗
║                          cmd/chrc/main.go                                  ║
║                                                                            ║
║  1. Parse env vars (PORT, SESSION_SECRET/AUTH_PASSWORD, DATA_DIR, ...)     ║
║  2. Derive 32-byte JWT secret via SHA-256(AUTH_PASSWORD)                   ║
║  3. Configure slog JSON handler                                            ║
║  4. signal.NotifyContext(SIGINT, SIGTERM)                                  ║
║  5. Open trace DB (db/traces.db) via dbopen.Open + trace.NewStore          ║
║  6. Open catalog DB (db/catalog.db) via dbopen.Open + WithTrace()          ║
║  7. tenant.InitCatalog(catalogDB) -- usertenant catalog tables             ║
║  8. migrateAuthColumns(catalogDB) -- add email/password_hash/role to users ║
║  9. migrateGlobalTables(catalogDB) -- global_search_engines, source_reg.   ║
║ 10. audit.NewSQLiteLogger(catalogDB).Init()                               ║
║ 11. ratelimit.New(catalogDB).Init() -- 5 req/60s on ip:login              ║
║ 12. seedAdmin(catalogDB) -- "admin/admin123!!!" if no admin exists         ║
║ 13. seedGlobalEngines(catalogDB) -- from catalog.PopulateSearchEngines     ║
║ 14. tenant.New(dataDir, catalogDB) -- usertenant pool                      ║
║ 15. migrateExistingShards() -- apply veille schema to all active shards    ║
║ 16. connectivity.New() + RegisterLocal("github_fetch", "api_fetch")        ║
║ 17. veille.New(pool, cfg, opts...) -- main service                         ║
║ 18. svc.RegisterConnectivity(router) -- 15 handlers                        ║
║ 19. Optional MCP/QUIC listener (if MCP_TRANSPORT=quic)                     ║
║ 20. svc.Start(ctx) -- scheduler + sweeper goroutines                       ║
║ 21. chi.NewRouter() + shield.DefaultBOStack() + auth.Middleware            ║
║ 22. Mount all HTTP routes (see below)                                      ║
║ 23. http.Server ListenAndServe on :PORT                                    ║
║ 24. <-ctx.Done() -> graceful shutdown (10s timeout)                        ║
╚══════════════════════════════════════════════════════════════════════════════╝
```

## Environment Variables

```
╔═══════════════════════╦══════════════╦══════════════════════════════════════╗
║ Variable              ║ Default      ║ Purpose                              ║
╠═══════════════════════╬══════════════╬══════════════════════════════════════╣
║ PORT                  ║ 8085         ║ HTTP listen port                     ║
║ SESSION_SECRET        ║ (required*)  ║ JWT signing key (SHA-256 derived)    ║
║ AUTH_PASSWORD          ║ (required*)  ║ Fallback for SESSION_SECRET          ║
║ DATA_DIR              ║ data         ║ Root dir for shard SQLite DBs        ║
║ CATALOG_DB            ║ db/catalog.db║ Usertenant catalog database           ║
║ BUFFER_DIR            ║ buffer/pending║ .md buffer output for RAG            ║
║ MCP_TRANSPORT         ║ ""           ║ "quic" to enable MCP/QUIC            ║
║ MCP_QUIC_ADDR         ║ :9444        ║ QUIC listener address                ║
║ TLS_CERT              ║ ""           ║ TLS cert file (or self-signed)       ║
║ TLS_KEY               ║ ""           ║ TLS key file                         ║
║ TRACE_DB              ║ db/traces.db ║ SQL trace database                   ║
║ LOG_LEVEL             ║ info         ║ debug/info/warn/error                ║
╚═══════════════════════╩══════════════╩══════════════════════════════════════╝
* One of SESSION_SECRET or AUTH_PASSWORD must be set.
```

## Network Diagram

```
                                    Internet / LAN
                                         │
                          ┌──────────────┼──────────────┐
                          │              │              │
                    ╔═════╧═════╗  ╔═════╧═════╗  ╔════╧════╗
                    ║ Browser   ║  ║ MCP Client║  ║External ║
                    ║ (SPA)     ║  ║ (QUIC)    ║  ║Services ║
                    ╚═════╤═════╝  ╚═════╤═════╝  ╚════╤════╝
                          │              │              │
         ┌────────────────┼──────────────┼──────────────┘
         │                │              │
    ╔════╧════════════════╧══════════════╧═══════════════════════╗
    ║  HTTP :8085                    QUIC :9444 (optional)       ║
    ║  ┌────────────────────────┐   ┌──────────────────────┐     ║
    ║  │ chi Router             │   │ MCP Server (go-sdk)  │     ║
    ║  │ + shield.DefaultBO     │   │ 15 veille tools      │     ║
    ║  │ + auth.Middleware(JWT) │   │ via mcpquic.Listener │     ║
    ║  │ + ratelimit (login)    │   └──────────────────────┘     ║
    ║  └───────────┬────────────┘                                ║
    ║              │                                              ║
    ║  ┌───────────┼──────────────────────────────────────────┐  ║
    ║  │ /connectivity ──→ connectivity.Router.Gateway()      │  ║
    ║  │   ├── github_fetch   (local handler)                 │  ║
    ║  │   ├── api_fetch      (local handler)                 │  ║
    ║  │   └── veille_*       (15 local handlers)             │  ║
    ║  └──────────────────────────────────────────────────────┘  ║
    ║              │                                              ║
    ║  ╔═══════════╧═══════════════════════════════════════════╗  ║
    ║  ║ veille.Service                                        ║  ║
    ║  ║  ├── scheduler (polls due sources every 1 min)        ║  ║
    ║  ║  ├── sweeper   (probes broken sources every 6h)       ║  ║
    ║  ║  ├── pipeline  (web/rss/api/document/question/conn.)  ║  ║
    ║  ║  ├── repairer  (auto-repair on fetch failure)         ║  ║
    ║  ║  └── buffer    (atomic .md writer to BUFFER_DIR)      ║  ║
    ║  ╚═══════════════════════════════════════════════════════╝  ║
    ║              │                        │                     ║
    ║  ╔═══════════╧══════╗    ╔════════════╧═══════════╗        ║
    ║  ║ catalog.db       ║    ║ data/{dossierID}.db    ║        ║
    ║  ║ (usertenant)     ║    ║ (per-shard veille DB)  ║        ║
    ║  ╚══════════════════╝    ╚════════════════════════╝        ║
    ╚════════════════════════════════════════════════════════════╝
```

## HTTP Routes

```
╔═══════════════════════════════════════════════════════════════════════════════╗
║ PUBLIC (no auth)                                                            ║
╠═══════════════════════════════════════════════════════════════════════════════╣
║ GET  /health                                   → {"status":"ok"}            ║
║ GET  /                                         → SPA index.html (embedded)  ║
║ GET  /static/*                                 → Embedded static assets     ║
║ POST /api/auth/login      [rate:5/60s]         → JWT token + cookie         ║
║ POST /api/auth/logout                          → Clear cookie               ║
║ ANY  /connectivity/*                           → Connectivity gateway       ║
╠═══════════════════════════════════════════════════════════════════════════════╣
║ AUTHENTICATED (requireSession)                                              ║
╠═══════════════════════════════════════════════════════════════════════════════╣
║ GET  /api/auth/me                              → Current user claims        ║
║ GET  /api/source-registry                      → Browse source registry     ║
║                                                                             ║
║ DOSSIERS                                                                    ║
║ GET    /api/dossiers                           → List active dossiers       ║
║ POST   /api/dossiers                           → Create dossier             ║
║ DELETE /api/dossiers/{dossierID}               → Delete dossier             ║
║                                                                             ║
║ SOURCES                                                                     ║
║ POST   /api/dossiers/{d}/sources                    → Add source            ║
║ POST   /api/dossiers/{d}/sources/from-registry/{id} → Add from registry     ║
║ GET    /api/dossiers/{d}/sources                    → List sources           ║
║ PUT    /api/dossiers/{d}/sources/{id}               → Update source          ║
║ DELETE /api/dossiers/{d}/sources/{id}               → Delete source          ║
║ POST   /api/dossiers/{d}/sources/{id}/fetch         → Fetch now              ║
║ POST   /api/dossiers/{d}/sources/{id}/reset         → Reset error state      ║
║ GET    /api/dossiers/{d}/sources/{id}/extractions   → List extractions       ║
║ GET    /api/dossiers/{d}/sources/{id}/history        → Fetch history          ║
║                                                                             ║
║ SEARCH & STATS                                                              ║
║ GET    /api/dossiers/{d}/search?q=&limit=      → FTS5 search                ║
║ GET    /api/dossiers/{d}/stats                  → {sources, extractions, ...}║
║                                                                             ║
║ QUESTIONS                                                                   ║
║ POST   /api/dossiers/{d}/questions              → Add question               ║
║ GET    /api/dossiers/{d}/questions              → List questions              ║
║ PUT    /api/dossiers/{d}/questions/{id}         → Update question             ║
║ DELETE /api/dossiers/{d}/questions/{id}         → Delete question             ║
║ POST   /api/dossiers/{d}/questions/{id}/run     → Run now                    ║
║ GET    /api/dossiers/{d}/questions/{id}/results → Question results           ║
╠═══════════════════════════════════════════════════════════════════════════════╣
║ ADMIN (requireAdmin)                                                        ║
╠═══════════════════════════════════════════════════════════════════════════════╣
║ GET/POST /api/admin/users                      → List / create users         ║
║ DELETE   /api/admin/users/{userID}             → Delete user                 ║
║                                                                             ║
║ ENGINES                                                                     ║
║ GET/POST   /api/admin/engines                  → List / create engines       ║
║ PUT/DELETE /api/admin/engines/{id}             → Update / delete engine       ║
║                                                                             ║
║ SOURCE REGISTRY                                                             ║
║ GET/POST   /api/admin/source-registry          → List / create entries       ║
║ PUT/DELETE /api/admin/source-registry/{id}     → Update / delete entry        ║
║                                                                             ║
║ OVERVIEW                                                                    ║
║ GET  /api/admin/overview                       → Cross-tenant user+shard map ║
║ GET  /api/admin/overview/{d}/searches          → Dossier search log          ║
║ POST /api/admin/overview/{d}/promote           → Promote search→question     ║
║                                                                             ║
║ SOURCE HEALTH                                                               ║
║ GET  /api/admin/source-health                  → Broken sources list         ║
║ POST /api/admin/source-health/sweep            → Manual sweep                ║
║ POST /api/admin/source-health/probe            → Probe single URL            ║
╚═══════════════════════════════════════════════════════════════════════════════╝
```

## Catalog DB Schema (catalog.db -- global tables added by cmd/chrc)

```
users (from usertenant.InitCatalog + migrateAuthColumns)
├── id              TEXT PK
├── name            TEXT
├── email           TEXT (unique where != '')
├── password_hash   TEXT (bcrypt)
├── role            TEXT DEFAULT 'user'  -- admin|user
├── status          TEXT                 -- active|deleted
└── created_at      INTEGER

shards (from usertenant.InitCatalog)
├── id              TEXT PK (dossierID)
├── name            TEXT
├── owner_id        TEXT
├── status          TEXT -- active|deleted
└── created_at      INTEGER

global_search_engines (from migrateGlobalTables)
├── id              TEXT PK
├── name            TEXT NOT NULL UNIQUE
├── strategy        TEXT DEFAULT 'api'
├── url_template    TEXT NOT NULL
├── api_config      TEXT DEFAULT '{}'
├── selectors       TEXT DEFAULT '{}'
├── rate_limit_ms   INTEGER DEFAULT 2000
├── max_pages       INTEGER DEFAULT 3
├── enabled         INTEGER DEFAULT 1
├── created_at      INTEGER
└── updated_at      INTEGER

source_registry (from migrateGlobalTables)
├── id              TEXT PK
├── name            TEXT NOT NULL
├── url             TEXT NOT NULL UNIQUE
├── source_type     TEXT DEFAULT 'rss'
├── category        TEXT DEFAULT ''
├── config_json     TEXT DEFAULT '{}'
├── description     TEXT DEFAULT ''
├── fetch_interval  INTEGER DEFAULT 3600000
├── enabled         INTEGER DEFAULT 1
├── created_at      INTEGER
└── updated_at      INTEGER
```

## Dependencies on hazyhaar/pkg

```
pkg/audit       -- SQLite audit logger
pkg/auth        -- JWT claims, cookie, middleware
pkg/connectivity -- Router, Gateway, inter-service calls
pkg/dbopen      -- SQLite open with pragmas + retry
pkg/horosafe    -- URL SSRF validation
pkg/idgen       -- UUID v7 generation
pkg/mcpquic     -- QUIC transport for MCP
pkg/ratelimit   -- SQLite-backed rate limiter
pkg/redact      -- Env var redaction in API output
pkg/shield      -- Security middleware stack (BO)
pkg/trace       -- SQL tracing (driver wrapper)
```

## Embedded Assets

```
//go:embed static
var staticFS embed.FS   -- SPA (index.html, CSS, JS)
```
