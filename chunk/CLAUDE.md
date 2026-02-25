# chunk

Responsabilite: Decoupe du texte en fragments avec overlap pour RAG et indexation FTS5, avec respect des frontieres de paragraphes.
Depend de: rien (stdlib uniquement)
Dependants: `domkeeper/internal/ingest` (chunking du contenu extrait)
Point d'entree: `chunk.go`
Types cles: `Options` (MaxTokens, OverlapTokens, MinChunkTokens), `Chunk` (Index, Text, TokenCount, OverlapPrev)
Invariants:
- Chaque chunk ne depasse jamais `MaxTokens`
- Le premier chunk a toujours `OverlapPrev = 0`
- Les chunks trop courts (< MinChunkTokens) sont fusionnes avec le precedent
- `Split("")` retourne nil, jamais un slice vide
- Tokenization = whitespace split (approximation mot = token)
NE PAS:
- Confondre `CountTokens` (whitespace split) avec `EstimateTokens` (heuristique BPE) — le premier est utilise pour le chunking, le second pour les estimations externes
- Modifier la strategie de split sans verifier les tests de paragraph-aware splitting
- Utiliser ce package depuis veille (veille n'en a plus besoin, seul domkeeper l'utilise)
