# vecbridge

Responsabilite: Bridge entre horosvec (Vamana+RaBitQ ANN index) et l'ecosysteme HOROS — expose l'index vectoriel via MCP tools et connectivity handlers, gere la DB SQLite.
Depend de: `github.com/hazyhaar/horosvec`, `github.com/hazyhaar/pkg/dbopen`, `github.com/hazyhaar/pkg/kit`, `github.com/hazyhaar/pkg/connectivity`, `github.com/modelcontextprotocol/go-sdk/mcp`
Dependants: `e2e/` (tests integration)
Point d'entree: `vecbridge.go`
Types cles: `Service` (wraps horosvec.Index + sql.DB), `Config` (DBPath, Horosvec config, CacheSize, Logger)
Invariants:
- vecbridge est un thin wrapper — ne modifie jamais les internals de horosvec
- `New()` ouvre la DB via dbopen, `NewFromDB()` reutilise une DB existante
- CacheSize par defaut = -512000 (512 MB de cache SQLite)
- IDs sont hex-encoded dans les MCP tools (conversion bytes <-> hex dans les handlers)
- `loadVector` lit directement la table `vec_nodes` par `ext_id`
- RegisterMCP expose 4 tools : `horosvec_search`, `horosvec_insert`, `horosvec_stats`, `horosvec_similar`
- RegisterConnectivity expose 3 handlers : `horosvec_search`, `horosvec_insert`, `horosvec_stats`
NE PAS:
- Appeler `Index.Search` avant `Index.Build` (l'index doit etre construit avec des seed vectors d'abord)
- Oublier de fermer le Service (fuite de descripteur SQLite)
- Modifier `deserializeFloat32s` sans verifier la coherence avec `horosembed.SerializeVector` (meme format little-endian)
