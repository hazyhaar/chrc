# CLAUDE.md — hazyhaar/chrc

## Ce que c'est

Prototypes RAG et web scraping créés par Claude GitHub. Contient les composants uniques qui ne vivent pas encore dans `hazyhaar/pkg`.

**Module** : `github.com/hazyhaar/chrc`
**Dépend de** : `github.com/hazyhaar/pkg` (dbopen, idgen, vtq, kit, watch, connectivity), `github.com/hazyhaar/horosvec`

## Packages

| Package | Rôle |
|---------|------|
| `domwatch/` | DOM observation daemon — mutation tracking via CDP (go-rod) |
| `domkeeper/` | Content extraction engine auto-réparant, scheduling, chunking |
| `docpipe/` | Pipeline d'extraction de documents (PDF, DOCX, ODT, HTML, texte) |
| `horosembed/` | Client embeddings, vector operations |
| `vecbridge/` | Bridge vectoriel MCP entre horosvec et les services |

## Build

```bash
CGO_ENABLED=0 go build ./...
```

## Origine

Repo initialement créé comme contexte pour Claude GitHub (snapshot hazyhaar/pkg + horos47). Le code dupliqué a été nettoyé — seuls les packages uniques restent.
