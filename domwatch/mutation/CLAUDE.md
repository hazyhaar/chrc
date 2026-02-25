# mutation

Responsabilite: Types structures emis par domwatch — contrat API public consomme par domkeeper et tout pipeline custom.
Depend de: standard library uniquement (crypto/sha256, encoding/json)
Dependants: `domwatch/` (producteur), `domkeeper/` (consommateur), `cmd/domwatch/` (serialisation profil)
Point d'entree: batch.go (types principaux)
Types cles:
- `Op` — type de mutation DOM (`insert`, `remove`, `text`, `attr`, `attr_del`, `doc_reset`)
- `Record` — une mutation DOM unique (Op, XPath, NodeType, Tag, Name, Value, OldValue, HTML)
- `Batch` — unite atomique emise par le watcher (ID UUIDv7, PageURL, PageID, Seq, Records, Timestamp, SnapshotRef)
- `Snapshot` — photo DOM complete (ID, PageURL, PageID, HTML, HTMLHash SHA-256, Timestamp)
- `Profile` — analyse structurelle d'une page (Landmarks, DynamicZones, StaticZones, ContentSelectors, Fingerprint, TextDensityMap)
- `Landmark` — element HTML5 landmark (tag, xpath, role ARIA)
- `Zone` — region DOM classifiee dynamique ou statique (xpath, selector CSS, mutation_rate)
Fonctions:
- `MarshalBatch/UnmarshalBatch` — serialisation JSON Batch
- `MarshalSnapshot/UnmarshalSnapshot` — serialisation JSON Snapshot
- `MarshalProfile/UnmarshalProfile` — serialisation JSON Profile
- `HashHTML` — digest SHA-256 hex d'un HTML brut
Invariants:
- Ce package est le contrat public — toute modification de structure casse les consommateurs
- Batch.Seq est monotone croissant par page (detection de gaps)
- Snapshot emis au startup, periodiquement (4h defaut), et apres chaque doc_reset
- HTML brut dans Snapshot est l'asset immutable fondateur
Tests: `mutation_test.go` — roundtrip marshal/unmarshal pour Batch, Snapshot, Profile + HashHTML determinisme + Op JSON values
NE PAS:
- Modifier les types sans coordonner avec domkeeper (contrat API)
- Ajouter de dependances externes (ce package doit rester stdlib-only)
- Changer les valeurs des constantes Op (serialisees en JSON dans les consumers)
