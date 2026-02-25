# cmd

Responsabilite: Points d'entree CLI pour les trois binaires du module chrc.
Depend de: packages chrc correspondants + `modernc.org/sqlite`
Dependants: aucun (entry points terminaux)
Point d'entree: `chrc/main.go` (HTTP service), `domkeeper/main.go` (extraction engine), `domwatch/main.go` (DOM observer)

## cmd/chrc/
Binaire HTTP deploye sur veille.docbusinessia.fr (port 8085). chi router, Basic Auth, usertenant pool, MCP/QUIC optionnel. Build: `make build` -> `bin/chrc`.
Depend de: `veille`, `veille/catalog`, `hazyhaar/pkg` (auth, connectivity, dbopen, kit, mcpquic), `hazyhaar/usertenant`

## cmd/domkeeper/
CLI extraction engine. Modes: daemon (`-config`), search (`-search "query"`), stats (`-stats`).
Depend de: `domkeeper`

## cmd/domwatch/
CLI DOM observation daemon. Modes: config (`-config`), single URL (`-url`), profile (`-profile`).
Depend de: `domwatch`, `domwatch/mutation`, `hazyhaar/pkg/idgen`

Invariants:
- Tous les binaires utilisent `signal.NotifyContext` pour graceful shutdown
- Tous les binaires utilisent `slog.NewJSONHandler` (structured logging JSON)
- `CGO_ENABLED=0` obligatoire pour le build
NE PAS:
- Deployer cmd/domkeeper ou cmd/domwatch sans configuration (pas de valeurs par defaut suffisantes)
- Oublier `_ "modernc.org/sqlite"` dans les imports (driver registration)
