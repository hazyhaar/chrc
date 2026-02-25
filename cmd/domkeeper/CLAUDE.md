# domkeeper (CLI)

Responsabilite: CLI du moteur d'extraction de contenu — daemon mode, recherche FTS5, et affichage de statistiques.
Depend de: `github.com/hazyhaar/chrc/domkeeper`, `modernc.org/sqlite`
Dependants: aucun (entry point terminal)
Point d'entree: main.go
Types cles: aucun type exporte (`package main`)
Modes:
- Daemon : `domkeeper -config domkeeper.yaml` — extraction continue
- Search : `domkeeper -db domkeeper.db -search "query"` — recherche one-shot (JSON stdout)
- Stats : `domkeeper -db domkeeper.db -stats` — compteurs one-shot (JSON stdout)
Flags: `-config`, `-db`, `-search`, `-stats`, `-log-level`, `-limit`
Invariants:
- Config resolue par priorite : `-config` YAML > `-db` path > erreur usage
- Modes search et stats sont one-shot (sortie JSON sur stdout, puis exit)
- Daemon mode bloque sur `<-ctx.Done()` (graceful shutdown SIGINT/SIGTERM)
- Structured logging JSON sur stderr (`slog.NewJSONHandler`)
NE PAS:
- Lancer sans `-config` ni `-db` (affiche usage et exit 1)
- Oublier `_ "modernc.org/sqlite"` dans les imports
- Confondre avec `cmd/domwatch` (domkeeper extrait, domwatch observe)
