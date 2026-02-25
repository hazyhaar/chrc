# domkeeper

Responsabilite: Moteur d'extraction de contenu auto-reparant — ingestion depuis domwatch, extraction par regles, chunking, FTS5 search, scheduling VTQ, MCP tools.
Depend de: `github.com/hazyhaar/chrc/chunk`, `github.com/hazyhaar/chrc/extract`, `github.com/hazyhaar/chrc/domwatch/mutation`, `github.com/hazyhaar/pkg/vtq`, `github.com/hazyhaar/pkg/kit`, `github.com/hazyhaar/pkg/idgen`, `github.com/hazyhaar/pkg/connectivity`, `github.com/modelcontextprotocol/go-sdk/mcp`, `modernc.org/sqlite`, `gopkg.in/yaml.v3`
Dependants: `cmd/domkeeper/`, `e2e/` (tests integration)
Point d'entree: `keeper.go`
Types cles: `Keeper` (orchestrateur principal), `Config` (DBPath, ChunkConfig, SchedulerConfig), `Stats`, `PremiumSearchOptions`, `PremiumSearchResult`, `SearchTier`
Invariants:
- Pipeline : domwatch -> ingest -> extract -> chunk -> store -> search/MCP
- Deduplication par SHA-256 hash du contenu
- VTQ queue nommee `domkeeper_refresh` pour le scheduling
- Le Sink() cree un domwatch.CallbackSink zero-serialisation (in-process)
- ExtractMode par defaut = "auto", TrustLevel par defaut = "unverified"
- RegisterMCP expose 11 tools (search, premium_search, rules CRUD, folders, stats, content, GPU)
- RegisterConnectivity expose 8 handlers
- Premium search multi-pass : query expansion + trust-level boosting + dedup
- GPU threshold : serverless vs dedicated decision based on backlog
NE PAS:
- Oublier d'appeler `k.Close()` (ferme la DB SQLite)
- Appeler `k.Start()` avant d'avoir wire le Sink dans domwatch
- Modifier les types re-exportes dans `types.go` sans mettre a jour `internal/store`
- Confondre `Search` (single FTS pass) avec `PremiumSearch` (multi-pass tiered)
