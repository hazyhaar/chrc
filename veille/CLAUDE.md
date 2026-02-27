# CLAUDE.md — veille

## Ce que c'est

Service d'**acquisition et d'indexation de contenu** multi-tenant strict. Surveille des sources web, RSS, API et documents locaux, détecte les changements, extrait le contenu, le découpe en l'indexe en FTS5 (directement sur les extractions). Produit aussi des `.md` dans un buffer filesystem pour consommation asynchrone par un îlot RAG.

## Architecture

```
veille.Service
├── pool (PoolResolver)          ← usertenant shard routing (dossierID)
├── fetcher (fetch.Fetcher)      ← HTTP GET conditionnel
├── pipeline (pipeline.Pipeline) ← dispatch → handler → store + buffer
│   ├── WebHandler               ← source_type: "web" (default)
│   ├── RSSHandler               ← source_type: "rss"
│   ├── APIHandler               ← source_type: "api"
│   ├── DocumentHandler          ← source_type: "document"
│   ├── QuestionHandler          ← source_type: "question" (tracked questions)
│   └── ConnectivityBridge       ← source_type: auto-discovered via {type}_fetch
├── router (*connectivity.Router) ← optional, plug-and-play external services
└── scheduler (scheduler.Scheduler) ← DueSources → pipeline
```

Multi-tenant via **usertenant** : chaque dossierID = un SQLite shard isolé. Le dossierID (UUID v7) est la clé universelle cross-service.

## Packages internes

| Package | Rôle |
|---------|------|
| `internal/store/` | Data access layer — CRUD sources, extractions, FTS5 search (on extractions), fetch log, stats, dedup, search engines, tracked questions |
| `internal/fetch/` | HTTP fetcher avec ETag, If-Modified-Since, hash-based dedup |
| `internal/pipeline/` | Orchestrateur dispatch par source_type → handlers → store + buffer + ConnectivityBridge |
| `internal/scheduler/` | Poll DueSources across shards, enqueue jobs |
| `internal/buffer/` | Écrit des `.md` (frontmatter YAML + texte) dans buffer/pending/ (atomic write) |
| `internal/feed/` | Parser RSS 2.0 et Atom 1.0 (encoding/xml, auto-détection) |
| `internal/apifetch/` | Fetch JSON API, dot-notation walker, field mapping, ${ENV_VAR} expansion |
| `internal/search/` | Search engine abstraction — strategy dispatch (api, generic stub) |
| `internal/question/` | Question runner — execute tracked questions against search engines |
| `internal/repair/` | Auto-repair : classifie erreurs, applique actions (backoff, UA rotation, mark broken), sweep périodique |
| `catalog/` | Seed catalog — sources + search engines pré-définis |

## Packages partagés (top-level chrc/)

| Package | Rôle |
|---------|------|
| `extract/` | Extraction HTML (CSS, XPath, density, auto) |
| `chunk/` | Découpage texte RAG avec overlap |
| `docpipe/` | Extraction multi-format (PDF, DOCX, ODT, HTML, TXT, MD) |

## Source Types

| Type | Handler | Description |
|------|---------|-------------|
| `web` | WebHandler | HTTP GET → HTML extract → FTS5 + buffer (défaut) |
| `rss` | RSSHandler | Fetch XML → parse RSS/Atom → par entry: dedup, extract, FTS5, buffer |
| `api` | APIHandler | Fetch JSON → walk result_path → par result: dedup, extract, FTS5, buffer |
| `document` | DocumentHandler | Fichier local → docpipe extract → dedup par hash, FTS5, buffer |
| `question` | QuestionHandler | Tracked question → search engines → dedup, extract, FTS5, buffer |
| `{custom}` | ConnectivityBridge | Auto-discovered via `{type}_fetch` on connectivity.Router |

## Tracked Questions

Questions = sources de type `"question"`. Une question est rejouée périodiquement sur des search engines, produisant une série temporelle de résultats.

```
AddQuestion("LLM inference 2026", keywords, ["brave_api"], 24h)
  → crée tracked_question + auto-source (type="question", interval=24h)
  → scheduler poll DueSources → QuestionHandler → Runner
  → search engines query → dedup → extractions + chunks + .md
```

- `sourceID = questionID` — `ListExtractions(qID)` donne l'historique complet
- Dedup par `hash(result.URL)` entre runs
- `follow_links`: fetch page complète (true) ou snippet only (false)

## Search Engines

Registry per-shard. Deux stratégies :
- **`api`** : HTTP JSON (Brave Search). Réutilise `apifetch`.
- **`generic`** : Rod/CSS selectors (DDG, Scholar). Stub — nécessite domwatch.

Seed : `catalog.PopulateSearchEngines(ctx, insertFn)` — Brave (enabled), DDG (stub), Scholar (stub).

## ConnectivityBridge

Extension plug-and-play via `connectivity.Router`. Convention `{type}_fetch` :

```
Service externe enregistre "socmed_fetch" sur le router
  → veille.New() scanne ListServices(), découvre *_fetch
  → ConnectivityBridge enregistré comme handler pour type "socmed"
  → AddSource(type="socmed") → pipeline dispatch → bridge → router.Call()
```

Protocole : entrée `{source_id, url, config, source_type}`, sortie `{extractions: [{title, content, url, content_hash}]}`.

## Buffer (.md output)

```
buffer/
├── pending/          ← veille dépose ici (atomic write)
├── done/             ← RAG worker déplace après traitement
└── failed/           ← erreurs de traitement RAG
```

Format : frontmatter YAML (id, source_id, dossier_id, source_url, source_type, title, extracted_at, content_hash) + texte nettoyé.

Activé via `Config.BufferDir`. UUID v7 comme filename = trié par temps.

## Pipeline dispatch

```
HandleJob(ctx, store, job)
    ↓
store.GetSource(sourceID)
    ↓
dispatch sur src.SourceType → handler.Handle(ctx, store, src, pipeline)
    ↓ (dans chaque handler)
fetch/parse → dedup (ExtractionExists) → extract → InsertExtraction (FTS5 auto-sync)
    ↓
buffer.Write (si configuré)
```

## Seed Catalog

```go
catalog.Populate(ctx, addSource, "tech")     // HN, Lobsters, Ars, Verge, TechCrunch, Go Blog
catalog.Populate(ctx, addSource, "legal-fr") // Legifrance, CNIL, EUR-Lex
catalog.Populate(ctx, addSource, "opendata") // data.gouv.fr, OpenAlex
catalog.Populate(ctx, addSource, "academic") // arXiv CS.AI, CS.CL
catalog.Populate(ctx, addSource, "news-fr")  // Le Monde, Next INpact

catalog.PopulateSearchEngines(ctx, insertFn) // Brave API (enabled), DDG HTML (stub), Scholar (stub)
```

## MCP Tools (15)

| Outil | Description |
|-------|-------------|
| `veille_add_source` | Ajouter une source (web, rss, api, document) |
| `veille_list_sources` | Lister les sources |
| `veille_update_source` | Modifier une source |
| `veille_delete_source` | Supprimer une source |
| `veille_fetch_now` | Fetch immédiat |
| `veille_search` | Recherche FTS5 sur les extractions |
| `veille_list_extractions` | Lister les extractions d'une source |
| `veille_stats` | Compteurs agrégés |
| `veille_fetch_history` | Historique des fetch |
| `veille_add_question` | Ajouter une question trackée |
| `veille_list_questions` | Lister les questions |
| `veille_update_question` | Modifier une question |
| `veille_delete_question` | Supprimer une question |
| `veille_run_question` | Exécuter une question immédiatement |
| `veille_question_results` | Résultats d'une question |

## Build & Test

```bash
CGO_ENABLED=0 go test -v -count=1 ./veille/...
CGO_ENABLED=0 go test -v -count=1 ./veille/internal/buffer/
CGO_ENABLED=0 go test -v -count=1 ./veille/internal/feed/
CGO_ENABLED=0 go test -v -count=1 ./veille/internal/apifetch/
CGO_ENABLED=0 go test -v -count=1 ./veille/catalog/
CGO_ENABLED=0 go test -v -count=1 ./e2e/
CGO_ENABLED=0 go test -count=1 ./...  # full suite
```

## Store : pas d'Open()

Contrairement à domkeeper, veille reçoit un `*sql.DB` du pool usertenant :

```go
store := store.NewStore(db) // db vient de pool.Resolve()
```

Le schema est appliqué via `veille.ApplySchema(db)` lors du premier Resolve.

## Auto-repair (internal/repair/)

Niveau 1 — natif, sans LLM. Intégré dans `processJob` (après chaque erreur pipeline).

**Classify** : `(sourceType, statusCode, errMsg)` → `(ErrorClass, Action)`

| Erreur | Action |
|--------|--------|
| 301/302/307/308 | `follow_redirect` — update URL |
| 5xx, timeout, DNS | `backoff` — doubler fetch_interval (cap 24h) |
| 429 | `increase_rate` — doubler rate_limit_ms |
| 404/410 | `mark_broken` |
| 403 (web/rss) | `rotate_ua` — essayer un autre User-Agent |
| 403 (api) | `mark_broken` (clé API révoquée) |
| parse error | `mark_broken` (nécessite LLM) |

**Repairer** : applique l'action recommandée en DB (backoff, UA rotation, mark broken).
**Sweeper** : probe périodique (HEAD, 10s timeout) des sources broken/error → reset si 2xx.

Statut `broken` = distinct de `error` : auto-repair a échoué, nécessite intervention admin.
Champ `original_fetch_interval` : sauvegardé avant backoff, restauré après reset.

### REST API admin

| Endpoint | Méthode | Description |
|----------|---------|-------------|
| `/api/admin/source-health` | GET | Liste toutes les sources en erreur cross-dossier |
| `/api/admin/source-health/sweep` | POST | Déclencher un sweep manuel |
| `/api/admin/source-health/probe` | POST | Probe une URL `{"url":"..."}` |
| `/api/dossiers/{id}/sources/{id}/reset` | POST | Reset fail_count d'une source |

### SPA

- Bouton "Reset" visible si `fail_count > 0` ou `last_status in (broken, error)`
- Bouton "Probe" visible si `fail_count > 0`
- Badge `broken` (rouge foncé) distinct de `error` (rouge)

## Securite

- **SSRF** : `horosafe.ValidateURL` appele avant chaque fetch + sur chaque redirect via `CheckRedirect`
- `Config.URLValidator` injectable (defaut: `horosafe.ValidateURL`), max 5 redirects
- IPs privees/loopback/link-local/metadata (169.254.x.x) bloquees

## TODO

- [ ] Phase 3 : Binary unifié `cmd/chrc/main.go` avec usertenant + MCP + HTTP
- [ ] Phase 3 : domwatch search engine adapter — wire generic strategy (Rod/CSS) to domwatch
- [ ] Phase 4 : Interface utilisateur HTML/templ
- [ ] Phase 5 : domwatch dispatch, domregistry pull, horosembed + vecbridge
- [ ] Niveau 2 repair : LLM via connectivity (horostracker pool gratuit)
