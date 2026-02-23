# CLAUDE.md — horos47

## Ce que c'est

Implémentation de référence des patterns HOROS. Ce n'est **pas un service déployable** — c'est un template/guide à copier et adapter.

**Module** : `horos47`
**État** : Référence stable, pas de développement actif

## Patterns documentés

| Dossier | Pattern |
|---------|---------|
| `core/chassis/` | Unified chassis (HTTP + MCP server) |
| `core/jobs/` | Job worker async avec retry |
| `core/data/` | UUID v7, helpers data |
| `core/trace/` | Tracing SQL |
| `services/gateway/` | Service gateway exemple |
| `services/gpufeeder/` | Service GPU feeder exemple |

## Documents de référence

- `UUID_V7_MIGRATION_GUIDE.md` — stratégie migration UUID v4 → v7
- `VECTOR_SEARCH.md` — embeddings, similarité cosinus, RAG
- `BUILD.md` — notes compilation (référence legacy CUDA, ignorer pour pure Go)

## Usage

**Ne pas importer directement `horos47`**. Copier les patterns dans le nouveau service, adapter au contexte. Les patterns concrets ont été extraits dans `github.com/hazyhaar/pkg` pour réutilisation directe.
