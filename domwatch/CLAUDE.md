# domwatch

Responsabilite: Daemon d'observation DOM via Chrome DevTools Protocol (go-rod) — capture mutations, snapshots periodiques, profiling structurel, auto-detection stealth level.
Depend de: `github.com/go-rod/rod`, `github.com/go-rod/stealth`, `github.com/hazyhaar/pkg/connectivity`, `github.com/hazyhaar/pkg/idgen`, `gopkg.in/yaml.v3`, `modernc.org/sqlite`
Dependants: `domkeeper/` (via Sink callback), `cmd/domwatch/`, `e2e/` (indirect via domkeeper)
Point d'entree: `watcher.go`
Types cles: `Watcher` (orchestrateur top-level), `Config/BrowserConfig/PageConfig/DebounceConfig/SinkConfig` (re-exports de internal/config), `Sink` (interface de sortie)
Invariants:
- domwatch observe, il n'interprete pas — HTML brut et deltas structures emis vers sinks
- 3 niveaux stealth : LevelHTTP (0), LevelHeadless (1), LevelHeadful (2), "auto" = essaie HTTP puis escalade
- Browser recycle : callback BeforeRecycle flush les observers, AfterRecycle reconnecte
- Debounce window configurable (defaut 250ms, max 1000 mutations par batch)
- RegisterConnectivity expose 2 handlers : `domwatch_observe`, `domwatch_profile`

## Sous-package mutation/

Types publics du contrat API consommateur :
- `Batch` (ID, PageURL, PageID, Seq, Records, Timestamp, SnapshotRef)
- `Record` (Op, XPath, NodeType, Tag, Name, Value, OldValue, HTML)
- `Snapshot` (ID, PageURL, PageID, HTML, HTMLHash, Timestamp)
- `Profile` (PageURL, Landmarks, DynamicZones, StaticZones, ContentSelectors, Fingerprint, TextDensityMap)
- `Op` constantes : insert, remove, text, attr, attr_del, doc_reset
- Marshal/Unmarshal helpers + `HashHTML` (SHA-256)

NE PAS:
- Importer domkeeper depuis domwatch (sens unique : domkeeper importe domwatch, pas l'inverse)
- Oublier que `mutation/` est le contrat public — toute modification casse les consommateurs
- Modifier les StealthLevel sans adapter `resolveStealthLevel` dans watcher.go
- Ignorer le RecycleCallback lors du recycle browser (perte de mutations)
