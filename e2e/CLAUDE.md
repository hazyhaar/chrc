# e2e

Responsabilite: Tests d'integration end-to-end validant le cablage inter-packages via connectivity.Router et le pipeline veille complet (add source -> fetch -> extract -> search).
Depend de: `github.com/hazyhaar/chrc/veille`, `github.com/hazyhaar/chrc/docpipe`, `github.com/hazyhaar/chrc/domkeeper`, `github.com/hazyhaar/chrc/domregistry`, `github.com/hazyhaar/chrc/horosembed`, `github.com/hazyhaar/chrc/vecbridge`, `github.com/hazyhaar/horosvec`, `github.com/hazyhaar/pkg/connectivity`, `github.com/hazyhaar/pkg/dbopen`
Dependants: aucun (package de test uniquement)
Point d'entree: `e2e_test.go` (connectivity router), `veille_test.go` (pipeline veille)
Types cles: `hashEmbedder` (deterministic non-zero vectors via SHA-256), `testPool` (in-memory tenant pool, Resolve(ctx, dossierID)), `sliceIter` (horosvec.VectorIterator mock)
Invariants:
- Chaque test cree ses propres fichiers temp / DB in-memory (isolation totale)
- `hashEmbedder` produit des vecteurs normalises unit-length deterministes (pas noopEmbedder qui donne des zeros)
- Les tests veille utilisent `httptest.NewServer` pour mocker les sources HTTP/RSS/API
- Dedup verifie : un second fetch ne cree jamais de nouvelles extractions
- Multi-tenant verifie : dossier A ne voit jamais les donnees de dossier B
- Tests couvrent : shared router stats, embed->insert->search, registry lifecycle, extract+embed, keeper rule CRUD, batch embed bulk insert, docpipe multi-format, RSS, API, question, connectivity bridge, GitHub
NE PAS:
- Utiliser `noopEmbedder` dans les tests ANN search (zero vectors = resultats degrades)
- Oublier `t.Cleanup` pour fermer les DB/services
- Ajouter des tests qui dependent d'un serveur externe (tout doit etre mock)
