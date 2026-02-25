# chrc ("cherche")

Responsabilité: Acquisition de contenu multi-source — veille informationnelle, web scraping, extraction documents, DOM observation. Output = extractions FTS5 + `.md` buffer pour RAG.
Module: `github.com/hazyhaar/chrc`
Dépend de: `github.com/hazyhaar/pkg` (dbopen, idgen, vtq, kit, watch, connectivity, mcpquic), `github.com/hazyhaar/horosvec`, `github.com/hazyhaar/usertenant`
Déployé: https://veille.docbusinessia.fr (VPS BO, Basic Auth)

## Index des packages

| Package | Rôle |
|---------|------|
| [`veille/`](veille/CLAUDE.md) | Veille informationnelle — acquisition multi-tenant (web, RSS, API, documents), FTS5, buffer .md |
| [`domwatch/`](domwatch/CLAUDE.md) | DOM observation daemon — mutation tracking via CDP (go-rod) |
| [`domkeeper/`](domkeeper/CLAUDE.md) | Content extraction engine auto-réparant, scheduling, chunking |
| [`domregistry/`](domregistry/CLAUDE.md) | Registre de domaines surveillés |
| [`docpipe/`](docpipe/CLAUDE.md) | Pipeline extraction documents (PDF, DOCX, ODT, HTML, texte) |
| [`horosembed/`](horosembed/CLAUDE.md) | Client embeddings, vector operations |
| [`vecbridge/`](vecbridge/CLAUDE.md) | Bridge vectoriel MCP entre horosvec et les services |
| [`extract/`](extract/CLAUDE.md) | Extraction HTML (CSS, XPath, density, auto) — partagé domkeeper + veille |
| [`chunk/`](chunk/CLAUDE.md) | Découpage texte RAG avec overlap |
| [`cmd/`](cmd/CLAUDE.md) | Entry points CLI (chrc HTTP, domkeeper, domwatch) |
| [`e2e/`](e2e/CLAUDE.md) | Tests d'intégration end-to-end cross-packages |
| [`bin/`](bin/CLAUDE.md) | Binaires compilés (artefacts de build) |

### veille — packages internes

| Package | Rôle |
|---------|------|
| `veille/internal/pipeline/` | Dispatch par source_type, handlers, buffer writer |
| `veille/internal/buffer/` | Atomic .md writer (frontmatter YAML + texte) |
| `veille/internal/feed/` | Parser RSS 2.0 + Atom 1.0 |
| `veille/internal/apifetch/` | Fetch JSON API, dot-notation, ${ENV_VAR} expansion |
| `veille/internal/search/` | Search engine abstraction, strategy dispatch |
| `veille/internal/question/` | Question runner — tracked questions against search engines |
| `veille/internal/store/` | Data access layer, FTS5, dedup, search engines |
| `veille/catalog/` | Seed catalog — sources + search engines |

## Documentation

| Document | Contenu |
|----------|---------|
| [`docs/veille_api_admin.md`](docs/veille_api_admin.md) | Notice d'administration de la veille par API REST (auth, CRUD, import OPML, codes d'erreur) |

## Build / Test

```bash
make build          # → bin/chrc (21MB, CGO_ENABLED=0)
make test           # → full suite
```

## Deploy

```bash
./scripts/deploy/deploy.sh veille
```

Auth: Basic Auth, user `veille`, password dans `.env`.
Port: 8085. Env vars: `PORT`, `AUTH_PASSWORD` (required), `DATA_DIR`, `CATALOG_DB`, `BUFFER_DIR`, `MCP_TRANSPORT`, `LOG_LEVEL`.

## NE PAS

- Oublier que `listActiveShards` retourne nil (no-op) — FetchNow via REST fonctionne
- Importer du code de horos47/horum_47 (archives mortes)
