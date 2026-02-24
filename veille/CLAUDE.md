# CLAUDE.md — veille

## Ce que c'est

Service d'**acquisition et d'indexation de contenu** multi-tenant strict. Surveille des sources web, détecte les changements, extrait le contenu, le découpe en chunks RAG et l'indexe en FTS5. Ne fait PAS de RAG/recherche aval — il produit des chunks SQLite consommés via MCP/connectivity.

## Architecture

```
veille.Service
├── pool (PoolResolver)          ← usertenant shard routing
├── spaces (SpaceManager)        ← space lifecycle
├── fetcher (fetch.Fetcher)      ← HTTP GET conditionnel
├── pipeline (pipeline.Pipeline) ← fetch → extract → chunk → store
└── scheduler (scheduler.Scheduler) ← DueSources → pipeline
```

Multi-tenant via **usertenant** : chaque user×space = un SQLite shard isolé. Pas de `user_id` dans le schema veille — l'isolation est au niveau fichier.

## Packages internes

| Package | Rôle |
|---------|------|
| `internal/store/` | Data access layer — CRUD sources, extractions, chunks, FTS5 search, fetch log, stats |
| `internal/fetch/` | HTTP fetcher avec ETag, If-Modified-Since, hash-based dedup |
| `internal/pipeline/` | Orchestrateur fetch → extract → chunk → store |
| `internal/scheduler/` | Poll DueSources across shards, enqueue jobs |

## Packages partagés (top-level chrc/)

| Package | Rôle |
|---------|------|
| `extract/` | Extraction HTML (CSS, XPath, density, auto) — déplacé de `domkeeper/internal/extract` |
| `chunk/` | Découpage texte RAG avec overlap — déplacé de `domkeeper/internal/chunk` |

## MCP Tools (13)

| Outil | Description |
|-------|-------------|
| `veille_add_source` | Ajouter une source |
| `veille_list_sources` | Lister les sources |
| `veille_update_source` | Modifier une source |
| `veille_delete_source` | Supprimer une source |
| `veille_fetch_now` | Fetch immédiat |
| `veille_search` | Recherche FTS5 sur les chunks |
| `veille_list_chunks` | Lister les chunks (paginé) |
| `veille_list_extractions` | Lister les extractions d'une source |
| `veille_stats` | Compteurs agrégés |
| `veille_fetch_history` | Historique des fetch |
| `veille_create_space` | Créer un espace de veille |
| `veille_list_spaces` | Lister les espaces |
| `veille_delete_space` | Supprimer un espace |

## Pipeline

```
FetchJob{userID, spaceID, sourceID, url}
    ↓
pool.Resolve(userID, spaceID) → *sql.DB
    ↓
store.GetSource(sourceID)
    ↓
fetcher.Fetch(url, etag, lastMod, prevHash)
    ↓ [304 / hash match] → store.RecordUnchanged → done
    ↓ [changed]
extract.Extract(body, Options{Mode: "auto"})
    ↓
extract.CleanText(result.Text)
    ↓
chunk.Split(text, opts)
    ↓
store.InsertExtraction + store.InsertChunks (FTS5 auto-sync)
    ↓
store.RecordFetchSuccess
```

## Build & Test

```bash
CGO_ENABLED=0 go test -v -count=1 ./veille/...
CGO_ENABLED=0 go test -v -count=1 ./extract/ ./chunk/
CGO_ENABLED=0 go test -v -count=1 ./e2e/
```

## Store : pas d'Open()

Contrairement à domkeeper, veille reçoit un `*sql.DB` du pool usertenant :

```go
store := store.NewStore(db) // db vient de pool.Resolve()
```

Le schema est appliqué via `veille.ApplySchema(db)` lors du premier Resolve.

## TODO

- [ ] Phase 2 : Flux RSS/Atom parser (`veille/internal/feed/`)
- [ ] Phase 2 : Connecteurs API JSON (`veille/internal/connector/`)
- [ ] Phase 3 : Binary unifié `cmd/chrc/main.go` avec usertenant + MCP + HTTP
- [ ] Phase 4 : Interface utilisateur HTML/templ
- [ ] Phase 5 : domwatch dispatch, domregistry pull, horosembed + vecbridge
