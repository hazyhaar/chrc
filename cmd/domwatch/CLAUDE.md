# domwatch (CLI)

Responsabilite: CLI du daemon d'observation DOM — modes config YAML, single-page, et profiling structurel.
Depend de: `github.com/hazyhaar/chrc/domwatch`, `github.com/hazyhaar/chrc/domwatch/mutation`, `github.com/hazyhaar/pkg/idgen`, `modernc.org/sqlite`
Dependants: aucun (entry point terminal)
Point d'entree: main.go
Types cles: aucun type exporte (`package main`)
Modes:
- Config : `domwatch -config domwatch.yaml` — observe les pages depuis le YAML, sinks configurables (stdout, webhook)
- Single-page : `domwatch -url https://example.com` — observe une seule URL (stdout sink)
- Profile : `domwatch -profile https://example.com` — analyse structurelle one-shot (JSON stdout, puis exit)
Flags: `-config`, `-url`, `-profile`, `-log-level`
Invariants:
- Config par defaut : headless stealth, 1 GiB memory limit, recycle browser toutes les 4h, debounce 250ms/1000 mutations max
- Resource blocking par defaut : images, fonts, media
- Sinks disponibles : `stdout` (defaut), `webhook` (POST JSON)
- Graceful shutdown SIGINT/SIGTERM (sauf mode profile qui est one-shot)
- `MarshalProfile` serialise le profil en JSON pour stdout
NE PAS:
- Lancer sans aucun flag (affiche usage et exit 1)
- Confondre avec `cmd/domkeeper` (domwatch observe le DOM, domkeeper extrait le contenu)
- Oublier `_ "modernc.org/sqlite"` dans les imports
