# chrc (CLI)

Responsabilite: Binaire HTTP du service veille — chi router, JWT auth, usertenant pool, MCP/QUIC optionnel. Deploye sur veille.docbusinessia.fr.
Depend de: `github.com/hazyhaar/chrc/veille`, `github.com/hazyhaar/chrc/veille/catalog`, `github.com/hazyhaar/pkg` (auth, audit, connectivity, dbopen, horosafe, idgen, kit, mcpquic, shield, trace), `github.com/hazyhaar/usertenant`, `modernc.org/sqlite`
Dependants: aucun (entry point terminal)
Point d'entree: main.go
Types cles: aucun type exporte (`package main`)
Fonctionnalites:
- chi router avec groupes : `/api/auth`, `/api/dossiers/{dossierID}`, `/api/admin/users`, `/api/admin/engines`, `/api/admin/source-registry`, `/api/admin/overview`, `/api/source-registry`
- JWT auth via cookie httpOnly (login/logout, session middleware)
- usertenant pool : multi-tenant, un shard SQLite par dossierID
- Dossier CRUD : `GET/POST /api/dossiers`, `DELETE /api/dossiers/{dossierID}`
- MCP/QUIC optionnel via `MCP_TRANSPORT=quic` (port 9444)
- Static embed SPA (`//go:embed static`) — JS vanilla, routeur hash
- Graceful shutdown via `signal.NotifyContext`
- shield middleware stack (CSP, X-Frame-Options, rate limiting)
- audit logger (SQLite)
- trace driver (sqlite-trace → traces.db)
Env vars: `PORT` (8085), `AUTH_PASSWORD` (requis), `SESSION_SECRET`, `DATA_DIR`, `CATALOG_DB`, `BUFFER_DIR`, `TRACE_DB`, `MCP_TRANSPORT`, `MCP_QUIC_ADDR`, `TLS_CERT`, `TLS_KEY`, `LOG_LEVEL`
Build: `CGO_ENABLED=0 go build -o bin/chrc ./cmd/chrc/`
NE PAS:
- Deployer sans `AUTH_PASSWORD` (crash au demarrage)
- Oublier `_ "modernc.org/sqlite"` dans les imports (driver registration)
- Confondre avec `cmd/domkeeper` ou `cmd/domwatch` (binaires distincts)
- Utiliser `/api/spaces` — migre vers `/api/dossiers/{dossierID}` (2026-02-25)
