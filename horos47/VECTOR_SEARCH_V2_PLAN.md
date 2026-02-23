# Plan d'implémentation — Recherche vectorielle V2 : Vamana + RaBitQ

**Projet** : horos47 (`/data/HOROS SYSTEM DEV AREA/horos47/`)
**Date** : 2026-02-23
**Objectif** : Remplacer la recherche vectorielle brute-force O(N) par un index ANN (Approximate Nearest Neighbor) disk-native en pure Go, sans CGO.

---

## 0. Contexte : ce qui existe déjà

### Stack technique
- **Go 1.25**, module `horos47`
- **SQLite** via `modernc.org/sqlite` (pure Go, `CGO_ENABLED=0`)
- **UUIDv7** via `github.com/google/uuid` (type wrapper `data.UUID` dans `core/data/uuid.go`)
- **Helpers DB** dans `core/data/db.go` : `OpenDB()`, `RunTransaction()`, `SafeClose()`, `SafeTxRollback()`
- **Routeur** : `go-chi/chi/v5`

### Code existant de recherche vectorielle (V1 — brute-force)

| Fichier | Rôle |
|---------|------|
| `storage/embeddings.go` | Schema `embeddings` table, `SerializeVector()`, `DeserializeVector()`, `CosineSimilarity()`, `CosineSimilarityOptimized()`, `CalculateNorm()` |
| `storage/documents.go` | Schema `documents`, `chunks`, `pdf_pages`, `chunks_fts` (FTS5). Types `Document`, `Chunk`. `SaveDocument()`, `GetChunks()` |
| `storage/chunker.go` | `ChunkText()` (overlap par mots), `ChunkBySentences()` |
| `handlers/search.go` | `HandleRAGRetrieve()` — recherche hybride FTS5 + vecteurs. `hybridSearch()`, `ftsSearch()`, `vectorSearchByEmbedding()`, `getQueryEmbedding()` |
| `handlers/embed.go` | `HandleEmbedChunks()` — génère embeddings via GPU Feeder, stocke dans table `embeddings` |
| `handlers/background_embed.go` | `RunBackgroundEmbedder()` — ticker 30s, traite chunks non-embeddés par batches de 32 |

### Le problème actuel
`vectorSearchByEmbedding()` dans `handlers/search.go:232` fait un **full table scan** :
```sql
SELECT e.chunk_id, e.document_id, e.embedding, e.norm, ...
FROM embeddings e
INNER JOIN chunks c ON c.chunk_id = e.chunk_id
INNER JOIN documents d ON d.document_id = c.document_id
```
Puis itère sur TOUS les vecteurs en Go pour calculer la similarité cosinus. C'est O(N) — inutilisable aux volumes réels.

### Volumes réels par user

Les corpus sont de l'ordre du **Go voire To** par utilisateur :

| Corpus user | Chunks (~200 mots) | Vecteurs | Embeddings bruts (1536d) | Index Vamana+RaBitQ |
|-------------|--------------------|-----------|--------------------------|--------------------|
| 1 GB texte  | ~1M                | ~1M       | 6.1 GB                   | ~450 MB            |
| 10 GB texte | ~10M               | ~10M      | 61 GB                    | ~4.5 GB            |
| 100 GB+     | ~100M              | ~100M     | 614 GB                   | ~45 GB             |

À ces volumes, le brute-force est impossible (scan 1M vecteurs = ~500ms minimum, 10M = ~5s). Le graphe Vamana est **obligatoire**, pas optionnel. Les vecteurs complets pour le reranking **doivent** rester sur disque — seul l'index compressé (codes RaBitQ + adjacency lists) peut tenir en mémoire ou en cache SQLite.

### Serveur d'embeddings
Endpoint `POST http://localhost:8003/v1/embeddings` (format OpenAI-compatible). Modèle : `gte-Qwen2-1.5B-instruct`, **1536 dimensions** (float32). Le code de query embedding est dans `handlers/search.go:192-229`.

---

## 1. Pourquoi Vamana + RaBitQ (et pas HNSW)

### HNSW : pas adapté au disque
HNSW (Hierarchical Navigable Small World) utilise un **graphe multi-couches** qui DOIT tenir en RAM pour être performant. Chaque requête traverse plusieurs couches, ce qui génère des accès mémoire aléatoires catastrophiques sur disque. Pour 100K vecteurs de 1536 dimensions : ~600 MB de RAM juste pour les vecteurs + le graphe.

### Vamana : conçu pour le disque
Vamana (l'algorithme derrière DiskANN de Microsoft, intégré dans SQL Server 2025) utilise un **graphe plat mono-couche** avec des arêtes longue-portée. Avantages :
- Chaque nœud = une ligne SQLite (adjacency list + vecteur compressé)
- Une requête = 2-3 lectures SQLite (hops dans le graphe)
- RAM minimale : seuls les codes RaBitQ du graphe actif sont nécessaires
- Le graphe plat se stocke naturellement dans une table relationnelle

### RaBitQ : compression 1-bit avec garanties théoriques
RaBitQ (SIGMOD 2024) compresse chaque vecteur float32 en **1 bit par dimension** :
- Un vecteur 1536-dim passe de 6144 bytes (float32) à **192 bytes** (1-bit) = compression 32x
- Le calcul de distance approximative utilise des opérations **bitwise** (AND, POPCOUNT) — extrêmement rapide en pure Go
- Contrairement au Product Quantization (PQ), pas de codebook à entraîner et des bornes d'erreur théoriques
- Déjà prouvé en production par VectorChord (PostgreSQL) et LanceDB

### Ce que fait VectorChord pour PostgreSQL, on le fait pour SQLite
L'architecture est identique : graphe Vamana stocké dans une table, codes RaBitQ pour filtrage rapide, reranking sur vecteurs complets.

---

## 2. Architecture cible

### Nouveau package : `storage/vecindex/`

```
storage/vecindex/
├── rabitq.go        # Encodage/décodage RaBitQ, distance bitwise
├── vamana.go        # Construction du graphe Vamana, robust pruning
├── search.go        # Beam search greedy avec reranking
├── schema.go        # Tables SQLite pour l'index
├── index.go         # API publique : VectorIndex (Build, Search, Insert)
└── vecindex_test.go # Tests unitaires
```

### Schema SQLite

```sql
-- Table principale de l'index vectoriel Vamana + RaBitQ
-- À 1M rows : ~450 MB. À 10M rows : ~4.5 GB.
-- node_id INTEGER séquentiel = clé primaire B-Tree optimale pour SQLite.
-- Le page cache SQLite gardera les nœuds chauds (medoid + voisinage fréquent).
CREATE TABLE IF NOT EXISTS vec_index (
    node_id    INTEGER PRIMARY KEY,   -- ID interne séquentiel (0..N-1)
    chunk_id   BLOB NOT NULL UNIQUE,  -- FK vers chunks.chunk_id (UUID 16 bytes)
    neighbors  BLOB NOT NULL,         -- Adjacency list : []int32 little-endian (R×4 bytes, typ. 256 bytes)
    rabitq     BLOB NOT NULL,         -- Code RaBitQ 1-bit (dim/8 bytes, typ. 192 bytes)
    x_sqnorm   REAL NOT NULL          -- ||x_centré||² pour calcul distance RaBitQ
);

CREATE INDEX IF NOT EXISTS idx_vec_index_chunk ON vec_index(chunk_id);

-- Métadonnées de l'index
CREATE TABLE IF NOT EXISTS vec_index_meta (
    key   TEXT PRIMARY KEY,
    value BLOB NOT NULL
);
-- Clés stockées :
--   "medoid"         : int32 little-endian (node_id du point d'entrée)
--   "dimension"      : int32
--   "max_degree"     : int32 (R, typiquement 64)
--   "node_count"     : int32
--   "centroid"       : []float32 sérialisé (dim × 4 bytes)
--   "centroid_norm"  : float64 (norme L2 du centroïde au moment du build)
--   "built_at"       : int64 (unix timestamp)
--   "vectors_at_build" : int32 (nombre de vecteurs au moment du build)
--   "inserts_since_build" : int32 (compteur d'insertions incrémentales)
```

**Design SQLite pour gros volumes** :
- `node_id INTEGER PRIMARY KEY` = rowid alias → accès O(1) par B-Tree, pas de surcoût d'index secondaire
- `PRAGMA page_size=8192` recommandé : un nœud (~450 bytes) tient dans une page, 2 nœuds par page. SQLite cache les pages chaudes automatiquement
- `PRAGMA cache_size=-512000` (512 MB de cache) pour garder ~1M nœuds chauds en mémoire
- Les vecteurs complets restent dans `embeddings` (sur disque, accédés seulement au reranking final)

### Relation avec les tables existantes

```
documents (existant)
    └── chunks (existant) ──── chunk_id ───→ embeddings (existant, vecteurs complets sur disque)
                           └── chunk_id ───→ vec_index  (NOUVEAU, graph + RaBitQ, en cache)
```

La table `embeddings` existante garde les **vecteurs complets** (6+ GB pour 1M chunks). Accédée uniquement au reranking final (top 20 candidats → 20 lectures ponctuelles). `vec_index` contient le **graphe** et les **codes compressés** — c'est le hot path, optimisé pour le cache SQLite.

---

## 3. Algorithme RaBitQ — Détail d'implémentation

### 3.1 Principe mathématique

RaBitQ encode un vecteur `x ∈ ℝ^D` en un code binaire `b ∈ {0,1}^D` tel que la distance entre `x` et un vecteur query `q` peut être approximée par des opérations bitwise.

### 3.2 Encodage (offline, à la construction de l'index)

```
Entrée : vecteur x ∈ ℝ^D, matrice de rotation P ∈ ℝ^{D×D}

1. Centrer : x_c = x - centroid   (centroid = moyenne de tous les vecteurs)
2. Rotation : x_r = P · x_c       (P est une matrice orthogonale aléatoire fixe)
3. Quantifier : b[i] = 1 si x_r[i] ≥ 0, sinon 0
4. Stocker : b (D/8 bytes), ||x||² (float64), Σx_r[i] (float64)
```

### 3.3 Calcul de distance approximative (online, à la recherche)

```
Entrée : query q ∈ ℝ^D (déjà centrée et rotée), code binaire b, normes pré-calculées

1. Rotation query : q_r = P · (q - centroid)
2. Dot product approx :
   ip ≈ (2 * POPCOUNT(AND(b, q_bits)) - D) * factor
   où q_bits[i] = 1 si q_r[i] ≥ 0, factor = ||x|| * ||q|| / D
3. Distance approx :
   dist² ≈ ||x||² + ||q||² - 2 * ip
```

La beauté : `POPCOUNT(AND(b, q_bits))` est une seule instruction CPU sur la plupart des architectures, et Go expose `math/bits.OnesCount64()`.

### 3.4 Code Go pour RaBitQ

```go
// rabitq.go

package vecindex

import (
    "math"
    "math/bits"
    "math/rand/v2"
)

// RaBitQEncoder gère l'encodage/décodage RaBitQ.
type RaBitQEncoder struct {
    Dim      int       // Dimension des vecteurs (ex: 1536)
    Centroid []float32 // Centroïde (moyenne de tous les vecteurs)
    Rotation []float32 // Matrice de rotation aplatie D×D (stockée row-major)
}

// NewRaBitQEncoder crée un encodeur avec une matrice de rotation aléatoire.
// La matrice est générée par décomposition QR d'une matrice gaussienne.
func NewRaBitQEncoder(dim int, centroid []float32) *RaBitQEncoder {
    rotation := generateOrthogonalMatrix(dim)
    return &RaBitQEncoder{Dim: dim, Centroid: centroid, Rotation: rotation}
}

// Encode compresse un vecteur float32 en code binaire.
// Retourne : code []byte (dim/8 bytes), norm², sum des composantes rotées.
func (e *RaBitQEncoder) Encode(vec []float32) (code []byte, sqNorm float64, xSum float64) {
    // 1. Centrer
    centered := make([]float32, e.Dim)
    for i := range centered {
        centered[i] = vec[i] - e.Centroid[i]
    }

    // 2. Rotation : rotated = Rotation × centered
    rotated := make([]float32, e.Dim)
    for i := 0; i < e.Dim; i++ {
        var sum float64
        row := i * e.Dim
        for j := 0; j < e.Dim; j++ {
            sum += float64(e.Rotation[row+j]) * float64(centered[j])
        }
        rotated[i] = float32(sum)
    }

    // 3. Quantifier : 1-bit par composante
    codeLen := (e.Dim + 7) / 8
    code = make([]byte, codeLen)
    for i := 0; i < e.Dim; i++ {
        if rotated[i] >= 0 {
            code[i/8] |= 1 << (i % 8)
        }
        xSum += float64(rotated[i])
    }

    // 4. Norme L2 carrée du vecteur centré
    for _, v := range centered {
        sqNorm += float64(v) * float64(v)
    }

    return code, sqNorm, xSum
}

// ApproxDistance calcule la distance approximative entre un query
// (pré-traité) et un code RaBitQ stocké.
// queryBits : code binaire du query (après centrage + rotation + signe)
// querySqNorm : ||q_centré||²
func ApproxDistance(queryBits, docBits []byte, querySqNorm, docSqNorm, docXSum float64, dim int) float64 {
    // POPCOUNT(AND(queryBits, docBits))
    popcount := 0
    for i := range queryBits {
        popcount += bits.OnesCount8(queryBits[i] & docBits[i])
    }
    // Dot product approximatif
    // ip ≈ (2*popcount - D) * scale_factor
    // Simplification : on utilise les normes pour la correction
    ip := float64(2*popcount-dim) * math.Sqrt(docSqNorm) * math.Sqrt(querySqNorm) / float64(dim)
    dist := querySqNorm + docSqNorm - 2*ip
    return dist
}

// generateOrthogonalMatrix génère une matrice orthogonale D×D
// par décomposition QR d'une matrice gaussienne aléatoire.
func generateOrthogonalMatrix(dim int) []float32 {
    // Matrice gaussienne aléatoire
    n := dim * dim
    mat := make([]float32, n)
    for i := range mat {
        mat[i] = float32(rand.NormFloat64())
    }
    // Gram-Schmidt pour orthogonaliser
    // (en production, utiliser Householder QR pour stabilité numérique)
    q := gramSchmidt(mat, dim)
    return q
}

// gramSchmidt orthogonalise les colonnes d'une matrice dim×dim (row-major).
func gramSchmidt(mat []float32, dim int) []float32 {
    q := make([]float32, dim*dim)
    copy(q, mat)

    for i := 0; i < dim; i++ {
        // Soustraire projections sur vecteurs précédents
        for j := 0; j < i; j++ {
            dot := float64(0)
            norm := float64(0)
            for k := 0; k < dim; k++ {
                dot += float64(q[i*dim+k]) * float64(q[j*dim+k])
                norm += float64(q[j*dim+k]) * float64(q[j*dim+k])
            }
            if norm > 0 {
                proj := dot / norm
                for k := 0; k < dim; k++ {
                    q[i*dim+k] -= float32(proj) * q[j*dim+k]
                }
            }
        }
        // Normaliser
        var norm float64
        for k := 0; k < dim; k++ {
            norm += float64(q[i*dim+k]) * float64(q[i*dim+k])
        }
        norm = math.Sqrt(norm)
        if norm > 0 {
            for k := 0; k < dim; k++ {
                q[i*dim+k] /= float32(norm)
            }
        }
    }
    return q
}
```

### 3.5 Problème de la matrice de rotation D×D

Pour D=1536, la matrice de rotation fait 1536×1536×4 = **9.4 MB**. C'est :
- Trop gros pour stocker naïvement
- La multiplication matrice-vecteur coûte O(D²) par encodage

**Solutions** :
1. **Matrice de rotation structurée** (recommandé) : utiliser le produit de D/log(D) matrices de Hadamard-Walsh signées. Coût O(D log D) au lieu de O(D²). Chaque "rotation" se décompose en :
   - Appliquer un vecteur de signes aléatoires (flip ±1) : O(D)
   - Appliquer une transformée de Walsh-Hadamard rapide (Fast Walsh-Hadamard Transform) : O(D log D)
   - Répéter 3 fois

   La FWHT est ~20 lignes de Go et tourne en ~100µs pour D=1536.

2. **Alternative simple** : ne pas utiliser de rotation du tout. RaBitQ fonctionne sans rotation (perte de ~5% recall). Pour un MVP, c'est parfaitement acceptable.

**Recommandation** : commencer SANS rotation (MVP), ajouter FWHT ensuite si le recall est insuffisant.

---

## 4. Algorithme Vamana — Détail d'implémentation

### 4.1 Principe

Vamana construit un graphe de k-plus-proches-voisins approximatifs avec des arêtes longue-portée pour permettre une traversée rapide. Le graphe est plat (une seule couche, contrairement à HNSW).

### 4.2 Paramètres

| Paramètre | Valeur recommandée | Description |
|-----------|-------------------|-------------|
| `R` (max degree) | 64 | Nombre max de voisins par nœud. Plus = meilleur recall, plus de RAM/disque |
| `L` (search list size) | 128 | Beam width pendant construction. L ≥ R obligatoire |
| `alpha` | 1.2 | Facteur pruning. >1 = arêtes longue-portée. 1.0 = densité locale max |
| `NumBuildPasses` | 2 | La 2ème passe améliore le recall de ~5-8% |

### 4.3 Construction du graphe (offline) — Coût à grande échelle

**Temps de build estimés (pure Go, single-threaded)** :

| N vecteurs | Temps | RAM peak | Notes |
|-----------|-------|----------|-------|
| 100K | ~2-5 min | ~1 GB | Rapide, dev/test |
| 1M | ~1-3h | ~10 GB | Production typique |
| 10M | ~12-30h | ~100 GB | Nécessite parallélisation ou build distribué |

Le bottleneck est la greedy search à chaque insertion : O(N × L × log N). Pour 1M vecteurs avec L=128, c'est ~128M calculs de distance L2. Chaque distance L2 sur 1536 dims = ~3000 FLOPs.

**Optimisations essentielles pour 1M+** :
1. **Paralléliser le build** : les passes Vamana sont parallélisables par batch (chaque nœud est indépendant sauf les mises à jour de voisins inverses). Utiliser un pool de goroutines avec lock par nœud.
2. **Build batché** : construire sur les premiers 100K vecteurs, puis insérer les suivants incrémentalement par batch de 10K (plus rapide que rebuild complet).
3. **Distance L2 optimisée** : dérouler la boucle en blocs de 8 float32 pour que le compilateur Go vectorise.

### 4.4 Construction du graphe (offline)

```
Entrée : N vecteurs, paramètres R, L, alpha

1. Initialiser graphe aléatoire (chaque nœud connecté à R voisins aléatoires)
2. Trouver le medoid (point le plus proche du centroïde) → point d'entrée σ
3. Pour chaque nœud p (dans un ordre aléatoire) :
   a. GreedySearch(σ, p, L) → obtenir les L plus proches candidats de p
   b. RobustPrune(p, candidats, alpha, R) → sélectionner ≤ R voisins pour p
   c. Pour chaque nouveau voisin v de p :
      - Ajouter p à la liste de voisins de v
      - Si degree(v) > R : RobustPrune(v, voisins(v), alpha, R)
4. Faire 2 passes complètes (la deuxième améliore significativement le recall)
```

### 4.4 RobustPrune — Le cœur de Vamana

```
RobustPrune(p, candidates, alpha, R):
  Entrée : point p, set de candidats, alpha, degré max R
  Sortie : ≤ R voisins optimaux pour p

  1. Trier candidats par distance à p (plus proche en premier)
  2. new_neighbors = []
  3. Pour chaque candidat c (en ordre de distance croissante) :
     a. Si len(new_neighbors) ≥ R : stop
     b. keep = true
     c. Pour chaque voisin déjà sélectionné n ∈ new_neighbors :
        Si dist(c, n) * alpha < dist(c, p) :
           keep = false  (c est "couvert" par n, pas besoin de l'ajouter)
           break
     d. Si keep : ajouter c à new_neighbors
  4. Retourner new_neighbors
```

L'idée du pruning : si un candidat `c` est très proche d'un voisin déjà sélectionné `n`, alors `n` "couvre" déjà cette direction de l'espace. Le facteur `alpha > 1` relâche cette condition pour favoriser les arêtes longue-portée (diversité géographique).

### 4.5 GreedySearch (beam search)

```
GreedySearch(entry_point, query, beam_width):
  1. candidates = min-heap initialisé avec {entry_point}
  2. visited = set {entry_point}
  3. results = bounded max-heap de taille beam_width

  4. Tant que candidates n'est pas vide :
     a. Extraire le candidat le plus proche c de candidates
     b. Si dist(c, query) > dist(results.worst(), query) : stop (convergé)
     c. Pour chaque voisin v de c :
        Si v ∉ visited :
           visited.add(v)
           Si dist(v, query) < dist(results.worst(), query) OU len(results) < beam_width :
              candidates.push(v)
              results.push(v)
              Si len(results) > beam_width : results.pop_worst()

  5. Retourner results
```

### 4.6 Code Go pour Vamana

```go
// vamana.go

package vecindex

import (
    "container/heap"
    "math"
    "math/rand/v2"
)

// VamanaConfig contient les paramètres de construction du graphe.
type VamanaConfig struct {
    MaxDegree     int     // R : nombre max de voisins par nœud (défaut: 64)
    SearchList    int     // L : beam width pendant construction (défaut: 128)
    Alpha         float64 // Facteur pruning, >1 = plus de long-range edges (défaut: 1.2)
    NumBuildPasses int    // Nombre de passes complètes (défaut: 2)
}

// Node représente un nœud dans le graphe Vamana.
type Node struct {
    ID        int32     // Index séquentiel 0..N-1
    Neighbors []int32   // Liste d'adjacence
}

// VamanaGraph est le graphe complet en mémoire (pour la construction).
type VamanaGraph struct {
    Nodes    []Node
    Vectors  [][]float32 // Vecteurs complets pour le calcul de distance exact
    Medoid   int32       // Point d'entrée (le plus proche du centroïde)
    Config   VamanaConfig
}

// BuildVamana construit le graphe Vamana à partir d'un ensemble de vecteurs.
func BuildVamana(vectors [][]float32, cfg VamanaConfig) *VamanaGraph {
    n := len(vectors)
    g := &VamanaGraph{
        Nodes:   make([]Node, n),
        Vectors: vectors,
        Config:  cfg,
    }

    // 1. Initialiser graphe aléatoire
    for i := 0; i < n; i++ {
        g.Nodes[i].ID = int32(i)
        g.Nodes[i].Neighbors = randomNeighbors(n, i, cfg.MaxDegree)
    }

    // 2. Trouver le medoid
    g.Medoid = findMedoid(vectors)

    // 3. Itérer : pour chaque pass, pour chaque nœud, améliorer ses voisins
    for pass := 0; pass < cfg.NumBuildPasses; pass++ {
        order := rand.Perm(n)
        for _, idx := range order {
            // GreedySearch depuis le medoid pour trouver les voisins candidats
            candidates := g.greedySearch(g.Medoid, vectors[idx], cfg.SearchList)

            // RobustPrune pour sélectionner les meilleurs voisins
            newNeighbors := g.robustPrune(int32(idx), candidates, cfg.Alpha, cfg.MaxDegree)
            g.Nodes[idx].Neighbors = newNeighbors

            // Mettre à jour les voisins inverses
            for _, v := range newNeighbors {
                g.addNeighbor(v, int32(idx))
                if len(g.Nodes[v].Neighbors) > cfg.MaxDegree {
                    g.Nodes[v].Neighbors = g.robustPrune(v, g.neighborsAsItems(v), cfg.Alpha, cfg.MaxDegree)
                }
            }
        }
    }

    return g
}

// greedySearch effectue une beam search dans le graphe.
// Retourne les beam_width plus proches voisins de query.
func (g *VamanaGraph) greedySearch(entry int32, query []float32, beamWidth int) []SearchItem {
    visited := make(map[int32]bool)
    visited[entry] = true

    // Min-heap pour les candidats à explorer
    candidates := &searchHeap{}
    heap.Init(candidates)
    heap.Push(candidates, SearchItem{ID: entry, Dist: l2Distance(g.Vectors[entry], query)})

    // Max-heap borné pour les résultats
    results := make([]SearchItem, 0, beamWidth)
    results = append(results, SearchItem{ID: entry, Dist: l2Distance(g.Vectors[entry], query)})

    for candidates.Len() > 0 {
        closest := heap.Pop(candidates).(SearchItem)

        // Condition d'arrêt : le meilleur candidat est pire que le pire résultat
        worstResult := results[len(results)-1].Dist
        if len(results) >= beamWidth && closest.Dist > worstResult {
            break
        }

        // Explorer les voisins
        for _, neighborID := range g.Nodes[closest.ID].Neighbors {
            if visited[neighborID] {
                continue
            }
            visited[neighborID] = true
            dist := l2Distance(g.Vectors[neighborID], query)

            if len(results) < beamWidth || dist < results[len(results)-1].Dist {
                heap.Push(candidates, SearchItem{ID: neighborID, Dist: dist})
                results = insertSorted(results, SearchItem{ID: neighborID, Dist: dist}, beamWidth)
            }
        }
    }

    return results
}

// robustPrune sélectionne ≤ maxDegree voisins optimaux avec diversité géographique.
func (g *VamanaGraph) robustPrune(nodeID int32, candidates []SearchItem, alpha float64, maxDegree int) []int32 {
    // Trier par distance croissante
    sortByDist(candidates)

    var neighbors []int32
    for _, c := range candidates {
        if int32(c.ID) == nodeID {
            continue
        }
        if len(neighbors) >= maxDegree {
            break
        }

        keep := true
        for _, n := range neighbors {
            distCN := l2Distance(g.Vectors[c.ID], g.Vectors[n])
            distCP := c.Dist
            if distCN*alpha < distCP {
                keep = false // c est "couvert" par n
                break
            }
        }

        if keep {
            neighbors = append(neighbors, c.ID)
        }
    }

    return neighbors
}

// SearchItem représente un candidat avec sa distance.
type SearchItem struct {
    ID   int32
    Dist float64
}

// l2Distance calcule la distance L2 carrée entre deux vecteurs.
// On utilise L2² (pas la racine) car on compare seulement des distances.
func l2Distance(a, b []float32) float64 {
    var sum float64
    for i := range a {
        d := float64(a[i]) - float64(b[i])
        sum += d * d
    }
    return sum
}

// findMedoid trouve le point le plus proche du centroïde.
func findMedoid(vectors [][]float32) int32 {
    dim := len(vectors[0])
    centroid := make([]float32, dim)
    for _, v := range vectors {
        for j := range v {
            centroid[j] += v[j]
        }
    }
    n := float32(len(vectors))
    for j := range centroid {
        centroid[j] /= n
    }

    bestID := int32(0)
    bestDist := math.MaxFloat64
    for i, v := range vectors {
        d := l2Distance(v, centroid)
        if d < bestDist {
            bestDist = d
            bestID = int32(i)
        }
    }
    return bestID
}
```

---

## 5. Recherche avec deux phases (Search)

### 5.1 Pipeline de recherche

```
Query "What is X?"
    │
    ▼
① Embedding du query via serveur GPU (endpoint existant localhost:8003)
    │
    ▼
② Pré-traitement query : centrer, rotation (optionnel), binariser → queryBits
    │
    ▼
③ Beam search sur le graphe Vamana :
   - Commencer au medoid
   - À chaque hop, calculer distance APPROXIMATIVE via RaBitQ (POPCOUNT)
   - Explorer ef_search candidats (typiquement 64-128)
    │
    ▼
④ Reranking : pour les top-K candidats (ex: 20), charger les vecteurs
   complets depuis la table `embeddings` et calculer la similarité cosinus exacte
    │
    ▼
⑤ Retourner les top-K résultats finaux avec scores exacts
```

### 5.2 Détail de l'étape ③ — Beam search avec RaBitQ

La beam search est identique à celle de la construction (section 4.5), mais utilise **RaBitQ pour le calcul de distance** au lieu des vecteurs complets :

```go
// search.go

package vecindex

import (
    "database/sql"
    "math/bits"
)

// SearchConfig contient les paramètres de recherche.
type SearchConfig struct {
    EfSearch    int // Beam width pour la recherche (défaut: 64, plus = meilleur recall, plus lent)
    TopK        int // Nombre de résultats finaux (défaut: 5)
    RerankTopN  int // Nombre de candidats à reranker avec vecteurs complets (défaut: TopK * 4)
}

// VectorIndex est l'interface publique pour la recherche vectorielle.
type VectorIndex struct {
    DB        *sql.DB
    Encoder   *RaBitQEncoder
    Medoid    int32
    Dim       int
    NodeCache *NodeLRU // Cache LRU des nœuds les plus accédés (medoid + voisinage chaud)
}

// NodeLRU est un cache LRU borné pour les nœuds du graphe.
// À 1M nœuds (~450 bytes/nœud), un cache de 100K nœuds = ~45 MB de RAM.
// Le medoid et ses voisins proches sont accédés à CHAQUE recherche —
// sans cache, chaque search ferait ~100-300 queries SQLite (1 par hop × voisins).
// Avec cache, les premiers hops (les plus fréquents) sont en mémoire.
//
// Implémentation : map[int32]*IndexNode + doubly-linked list pour LRU eviction.
// Capacité configurable, défaut 100K nœuds.
type NodeLRU struct {
    Cap   int
    // ... implementation standard LRU (map + list)
}

// Search effectue une recherche ANN sur l'index Vamana + RaBitQ.
func (idx *VectorIndex) Search(queryVec []float32, cfg SearchConfig) ([]SearchResult, error) {
    // 1. Encoder le query en bits RaBitQ
    queryBits, querySqNorm, _ := idx.Encoder.Encode(queryVec)

    // 2. Beam search sur le graphe stocké dans SQLite
    candidates, err := idx.beamSearchSQLite(queryBits, querySqNorm, cfg.EfSearch)
    if err != nil {
        return nil, err
    }

    // 3. Reranking avec vecteurs complets
    rerankN := cfg.RerankTopN
    if rerankN == 0 {
        rerankN = cfg.TopK * 4
    }
    if rerankN > len(candidates) {
        rerankN = len(candidates)
    }
    topCandidates := candidates[:rerankN]

    results, err := idx.rerankWithFullVectors(queryVec, topCandidates, cfg.TopK)
    if err != nil {
        return nil, err
    }

    return results, nil
}

// beamSearchSQLite effectue la beam search en lisant le graphe depuis SQLite.
func (idx *VectorIndex) beamSearchSQLite(queryBits []byte, querySqNorm float64, beamWidth int) ([]SearchItem, error) {
    // Charger le nœud medoid
    entry, err := idx.loadNode(idx.Medoid)
    if err != nil {
        return nil, err
    }

    visited := make(map[int32]bool)
    visited[idx.Medoid] = true

    entryDist := rabitqDistance(queryBits, entry.RaBitQ, querySqNorm, entry.SqNorm, idx.Dim)

    // candidates : min-heap (plus proche en tête)
    // results : slice triée, bornée à beamWidth
    candidates := []SearchItem{{ID: idx.Medoid, Dist: entryDist}}
    results := []SearchItem{{ID: idx.Medoid, Dist: entryDist}}

    for len(candidates) > 0 {
        // Pop le plus proche
        closest := candidates[0]
        candidates = candidates[1:]

        if len(results) >= beamWidth && closest.Dist > results[len(results)-1].Dist {
            break
        }

        // Charger les voisins depuis SQLite
        node, err := idx.loadNode(closest.ID)
        if err != nil {
            continue
        }

        for _, neighborID := range node.Neighbors {
            if visited[neighborID] {
                continue
            }
            visited[neighborID] = true

            neighbor, err := idx.loadNode(neighborID)
            if err != nil {
                continue
            }

            dist := rabitqDistance(queryBits, neighbor.RaBitQ, querySqNorm, neighbor.SqNorm, idx.Dim)

            if len(results) < beamWidth || dist < results[len(results)-1].Dist {
                candidates = insertSorted(candidates, SearchItem{ID: neighborID, Dist: dist}, beamWidth*2)
                results = insertSorted(results, SearchItem{ID: neighborID, Dist: dist}, beamWidth)
            }
        }
    }

    return results, nil
}

// loadNode charge un nœud depuis SQLite (neighbors + rabitq code).
func (idx *VectorIndex) loadNode(nodeID int32) (*IndexNode, error) {
    row := idx.DB.QueryRow(`
        SELECT neighbors, rabitq, x_sqnorm FROM vec_index WHERE node_id = ?
    `, nodeID)

    var neighborsBlob, rabitqBlob []byte
    var sqNorm float64
    if err := row.Scan(&neighborsBlob, &rabitqBlob, &sqNorm); err != nil {
        return nil, err
    }

    return &IndexNode{
        ID:        nodeID,
        Neighbors: deserializeInt32s(neighborsBlob),
        RaBitQ:    rabitqBlob,
        SqNorm:    sqNorm,
    }, nil
}

// IndexNode est un nœud chargé depuis SQLite.
type IndexNode struct {
    ID        int32
    Neighbors []int32
    RaBitQ    []byte
    SqNorm    float64
}

// rabitqDistance calcule la distance approximative via POPCOUNT.
func rabitqDistance(queryBits, docBits []byte, querySqNorm, docSqNorm float64, dim int) float64 {
    popcount := 0
    // Traiter 8 bytes à la fois (64 bits) pour performance
    i := 0
    for ; i+8 <= len(queryBits); i += 8 {
        q := uint64(queryBits[i]) | uint64(queryBits[i+1])<<8 |
            uint64(queryBits[i+2])<<16 | uint64(queryBits[i+3])<<24 |
            uint64(queryBits[i+4])<<32 | uint64(queryBits[i+5])<<40 |
            uint64(queryBits[i+6])<<48 | uint64(queryBits[i+7])<<56
        d := uint64(docBits[i]) | uint64(docBits[i+1])<<8 |
            uint64(docBits[i+2])<<16 | uint64(docBits[i+3])<<24 |
            uint64(docBits[i+4])<<32 | uint64(docBits[i+5])<<40 |
            uint64(docBits[i+6])<<48 | uint64(docBits[i+7])<<56
        popcount += bits.OnesCount64(q & d)
    }
    // Octets restants
    for ; i < len(queryBits); i++ {
        popcount += bits.OnesCount8(queryBits[i] & docBits[i])
    }

    // Distance approximative
    import_scale := math.Sqrt(querySqNorm*docSqNorm) / float64(dim)
    ip := float64(2*popcount-dim) * import_scale
    return querySqNorm + docSqNorm - 2*ip
}

// rerankWithFullVectors charge les vecteurs complets depuis la table
// `embeddings` existante et recalcule la similarité cosinus exacte.
func (idx *VectorIndex) rerankWithFullVectors(queryVec []float32, candidates []SearchItem, topK int) ([]SearchResult, error) {
    // ... charger vecteurs complets depuis `embeddings` par chunk_id
    // ... calculer CosineSimilarityOptimized() (fonction existante dans storage/embeddings.go)
    // ... trier par score décroissant, retourner topK
    // Détail : voir section 7 (intégration)
    return nil, nil
}

// SearchResult est le résultat final retourné à l'appelant.
type SearchResult struct {
    ChunkID    []byte  // UUID 16 bytes
    Score      float64 // Similarité cosinus exacte (après reranking)
}
```

---

## 6. Sérialisation des données SQLite

### 6.1 Neighbors (adjacency list)

```go
// Sérialisation : []int32 → []byte (little-endian, 4 bytes par int32)
func serializeInt32s(ids []int32) []byte {
    buf := make([]byte, len(ids)*4)
    for i, id := range ids {
        binary.LittleEndian.PutUint32(buf[i*4:], uint32(id))
    }
    return buf
}

// Désérialisation : []byte → []int32
func deserializeInt32s(blob []byte) []int32 {
    ids := make([]int32, len(blob)/4)
    for i := range ids {
        ids[i] = int32(binary.LittleEndian.Uint32(blob[i*4:]))
    }
    return ids
}
```

### 6.2 Tailles pour 100K vecteurs de 1536 dimensions

| Donnée | Par nœud | Total 100K |
|--------|----------|------------|
| `rabitq` (1536/8) | 192 bytes | 19.2 MB |
| `neighbors` (64 × 4) | 256 bytes | 25.6 MB |
| `norm`, `x_sum`, `x_sqnorm` | 24 bytes | 2.4 MB |
| **Total vec_index** | **472 bytes** | **47.2 MB** |
| `embedding` complet (1536 × 4) | 6144 bytes | 614 MB |

L'index Vamana + RaBitQ est **13x plus petit** que les vecteurs complets. Il peut tenir en cache SQLite pour des performances optimales.

---

## 7. Intégration avec le code existant

### 7.1 Modifications à apporter

#### `handlers/search.go` — Remplacer `vectorSearchByEmbedding()`

La fonction actuelle (ligne 232) fait un scan complet. Elle doit être remplacée par un appel à `VectorIndex.Search()` :

```go
// AVANT (V1 brute-force) — lignes 232-281
func (h *Handlers) vectorSearchByEmbedding(queryEmbedding []float32, topK int, minScore float64) ([]QueryResult, error) {
    // ... full table scan de ALL embeddings ...
}

// APRÈS (V2 Vamana + RaBitQ)
func (h *Handlers) vectorSearchByEmbedding(queryEmbedding []float32, topK int, minScore float64) ([]QueryResult, error) {
    if h.VecIndex == nil {
        return h.vectorSearchBruteForce(queryEmbedding, topK, minScore) // fallback V1
    }

    searchResults, err := h.VecIndex.Search(queryEmbedding, vecindex.SearchConfig{
        EfSearch:   64,
        TopK:       topK,
        RerankTopN: topK * 4,
    })
    if err != nil {
        return h.vectorSearchBruteForce(queryEmbedding, topK, minScore)
    }

    // Convertir SearchResult → QueryResult (enrichir avec chunk_text, document_title via SQL)
    var results []QueryResult
    for _, sr := range searchResults {
        if sr.Score < minScore {
            continue
        }
        // Charger métadonnées
        var qr QueryResult
        err := h.DB.QueryRow(`
            SELECT c.chunk_id, c.document_id, c.chunk_text, c.chunk_index, d.title
            FROM chunks c JOIN documents d ON d.document_id = c.document_id
            WHERE c.chunk_id = ?
        `, sr.ChunkID).Scan(&qr.ChunkID, &qr.DocumentID, &qr.ChunkText, &qr.ChunkIndex, &qr.DocumentTitle)
        if err != nil {
            continue
        }
        qr.Score = sr.Score
        results = append(results, qr)
    }
    return results, nil
}
```

#### `handlers/embed.go` + `handlers/background_embed.go` — Ajouter insertion dans l'index

Après chaque batch d'embeddings stocké, appeler `VecIndex.InsertBatch()` pour ajouter les nouveaux vecteurs au graphe. L'insertion incrémentale dans Vamana suit le même algorithme que la construction : GreedySearch + RobustPrune pour le nouveau nœud.

#### Struct `Handlers` — Ajouter le champ `VecIndex`

```go
type Handlers struct {
    DB           *sql.DB
    Logger       *slog.Logger
    GPUSubmitter *gpufeeder.Submitter
    GW           *gateway.Service
    HTTPClient   *http.Client
    VecIndex     *vecindex.VectorIndex  // NOUVEAU
}
```

### 7.2 Construction initiale de l'index

Ajouter une commande CLI ou un handler qui :
1. Charge tous les vecteurs de la table `embeddings`
2. Appelle `BuildVamana()`
3. Encode chaque vecteur avec `RaBitQEncoder.Encode()`
4. Stocke le graphe + codes dans `vec_index`
5. Stocke les métadonnées dans `vec_index_meta`

```go
// cmd/buildindex/main.go (nouveau)
// Ou ajouter comme sous-commande du binaire existant
func buildIndex(db *sql.DB) error {
    // 1. Charger tous les embeddings
    rows, _ := db.Query("SELECT chunk_id, embedding, norm FROM embeddings")
    var chunkIDs [][]byte
    var vectors [][]float32
    for rows.Next() {
        var cid, blob []byte
        var norm float64
        rows.Scan(&cid, &blob, &norm)
        chunkIDs = append(chunkIDs, cid)
        vectors = append(vectors, storage.DeserializeVector(blob))
    }
    rows.Close()

    // 2. Construire le graphe Vamana
    graph := vecindex.BuildVamana(vectors, vecindex.VamanaConfig{
        MaxDegree:      64,
        SearchList:     128,
        Alpha:          1.2,
        NumBuildPasses: 2,
    })

    // 3. Encoder RaBitQ
    centroid := computeCentroid(vectors)
    encoder := vecindex.NewRaBitQEncoder(len(vectors[0]), centroid)

    // 4. Écrire dans SQLite
    vecindex.InitSchema(db)
    tx, _ := db.Begin()
    for i := range vectors {
        code, sqNorm, xSum := encoder.Encode(vectors[i])
        neighborsBlob := serializeInt32s(graph.Nodes[i].Neighbors)
        tx.Exec(`INSERT INTO vec_index (node_id, chunk_id, neighbors, rabitq, norm, x_sum, x_sqnorm)
                  VALUES (?, ?, ?, ?, ?, ?, ?)`,
            i, chunkIDs[i], neighborsBlob, code,
            math.Sqrt(sqNorm), xSum, sqNorm)
    }
    // Stocker métadonnées
    tx.Exec(`INSERT INTO vec_index_meta (key, value) VALUES ('medoid', ?)`,
        serializeInt32(graph.Medoid))
    tx.Commit()

    return nil
}
```

---

## 8. Insertion incrémentale et gestion du centroid drift

C'est le mode de fonctionnement principal, pas une optimisation future. Les corpus grandissent en continu (ingestion de documents, nouveaux chunks). Un rebuild complet à chaque insertion est impossible (1-3h pour 1M vecteurs).

### 8.1 Insertion incrémentale dans le graphe Vamana

L'insertion d'un nouveau vecteur suit exactement le même algorithme que la construction :

```
InsertSingle(graph, new_vector, node_id):
  1. Encoder le vecteur avec RaBitQ (centroïde existant)
  2. GreedySearch(medoid, new_vector, L) → trouver les L plus proches voisins
  3. RobustPrune(node_id, candidats, alpha, R) → sélectionner les voisins du nouveau nœud
  4. Pour chaque voisin v :
     - Ajouter node_id à la liste de voisins de v
     - Si degree(v) > R : RobustPrune(v, voisins(v), alpha, R)
  5. Persister : INSERT INTO vec_index + UPDATE des voisins modifiés
```

**Coût par insertion** : une greedy search (~2-5ms à 1M nœuds) + quelques updates SQL. Acceptable en continu.

**Batch insert** : pour l'ingestion en masse (nouveau document → 100+ chunks d'un coup), grouper les insertions dans une transaction SQLite et ne faire les updates de voisins inverses qu'à la fin du batch.

```go
// InsertBatch ajoute un lot de vecteurs au graphe existant.
func (idx *VectorIndex) InsertBatch(vectors [][]float32, chunkIDs [][]byte) error {
    return data.RunTransaction(idx.DB, func(tx *sql.Tx) error {
        for i, vec := range vectors {
            nodeID := idx.nextNodeID() // atomique, incrémenté
            code, sqNorm, _ := idx.Encoder.Encode(vec)

            // Greedy search pour trouver les voisins
            candidates := idx.greedySearchCached(vec, idx.Config.SearchList)
            neighbors := robustPrune(nodeID, candidates, idx.Config.Alpha, idx.Config.MaxDegree)

            // Persister le nouveau nœud
            tx.Exec(`INSERT INTO vec_index (node_id, chunk_id, neighbors, rabitq, x_sqnorm)
                      VALUES (?, ?, ?, ?, ?)`,
                nodeID, chunkIDs[i], serializeInt32s(neighbors), code, sqNorm)

            // Mettre à jour les voisins inverses
            for _, v := range neighbors {
                idx.addNeighborTx(tx, v, nodeID)
            }
        }

        // Incrémenter le compteur d'insertions
        idx.incrementInsertCounter(tx, len(vectors))
        return nil
    })
}
```

### 8.2 Centroid drift — Le problème central

RaBitQ encode chaque vecteur relativement au **centroïde global** (moyenne de tous les vecteurs). Quand des vecteurs sont ajoutés :
1. Le centroïde réel dérive par rapport au centroïde stocké
2. Les codes RaBitQ existants sont calculés avec l'ancien centroïde
3. Les distances approximatives deviennent progressivement fausses
4. Le **recall se dégrade silencieusement**

### 8.3 Solution : running centroid + rebuild conditionnel

```go
// CentroidTracker maintient un centroïde courant sans full scan.
type CentroidTracker struct {
    Centroid      []float32 // Centroïde courant (running average)
    BuildCentroid []float32 // Centroïde au moment du dernier build
    N             int64     // Nombre total de vecteurs
    InsertsSince  int64     // Insertions depuis le dernier build
}

// Update met à jour le centroïde courant en O(D) — pas de full scan.
// Running average : new_centroid = old_centroid + (new_vec - old_centroid) / (N+1)
func (ct *CentroidTracker) Update(newVec []float32) {
    ct.N++
    ct.InsertsSince++
    for i := range ct.Centroid {
        ct.Centroid[i] += (newVec[i] - ct.Centroid[i]) / float32(ct.N)
    }
}

// DriftRatio retourne la distance L2 normalisée entre le centroïde
// actuel et celui du dernier build. Valeur entre 0 (identique) et +∞.
func (ct *CentroidTracker) DriftRatio() float64 {
    var drift, buildNorm float64
    for i := range ct.Centroid {
        d := float64(ct.Centroid[i] - ct.BuildCentroid[i])
        drift += d * d
        buildNorm += float64(ct.BuildCentroid[i]) * float64(ct.BuildCentroid[i])
    }
    if buildNorm == 0 {
        return 0
    }
    return math.Sqrt(drift) / math.Sqrt(buildNorm)
}

// NeedsRebuild retourne true si le drift ou le ratio d'insertions
// dépasse les seuils configurés.
func (ct *CentroidTracker) NeedsRebuild() bool {
    // Seuil 1 : le centroïde a bougé de plus de 5% (calibrer empiriquement)
    if ct.DriftRatio() > 0.05 {
        return true
    }
    // Seuil 2 : plus de 30% de nœuds ajoutés depuis le dernier build
    if ct.N > 0 && float64(ct.InsertsSince)/float64(ct.N) > 0.30 {
        return true
    }
    return false
}
```

### 8.4 Stratégie de rebuild

Le rebuild complet est coûteux (1-3h pour 1M vecteurs). Il doit être :
- **Déclenché** par `NeedsRebuild()`, vérifié à chaque batch d'insertion
- **Exécuté en background** dans une goroutine, pendant que l'ancien index reste actif
- **Atomique** : build dans une table temporaire `vec_index_new`, puis `ALTER TABLE` swap
- **Planifiable** : un cron/ticker peut forcer un rebuild nocturne

```go
// RebuildAsync lance un rebuild en background sans bloquer les recherches.
func (idx *VectorIndex) RebuildAsync(ctx context.Context) {
    go func() {
        // 1. Charger tous les vecteurs depuis embeddings (streaming, pas tout en RAM)
        // 2. BuildVamana() sur les vecteurs
        // 3. Encoder RaBitQ avec le nouveau centroïde
        // 4. Écrire dans vec_index_new
        // 5. Swap atomique : DROP vec_index, ALTER TABLE vec_index_new RENAME TO vec_index
        // 6. Invalider le NodeCache
        // 7. Mettre à jour vec_index_meta (nouveau centroïde, timestamp, compteurs)
    }()
}
```

**Pour 10M+ vecteurs** : le rebuild ne peut pas charger tous les vecteurs en RAM (~60 GB). Solutions :
1. **Build par streaming** : itérer sur les vecteurs depuis SQLite, construire le graphe incrémentalement (mode batché)
2. **Build par partition** : découper le corpus en partitions (ex: par document), construire un sous-graphe par partition, fusionner

---

## 9. Ordre d'implémentation

### Phase 1 — RaBitQ (encodeur + distance POPCOUNT)
Package : `storage/vecindex/rabitq.go` + tests

- `RaBitQEncoder` : `Encode()` sans rotation (centrage + signe)
- `rabitqDistance()` : POPCOUNT 64-bit optimisé
- `CentroidTracker` : running average + drift ratio
- Sérialisation centroïde pour `vec_index_meta`
- **Tests** : order preservation (si dist(A,Q) < dist(B,Q) en exact, alors dist_approx(A,Q) < dist_approx(B,Q) dans >90% des cas)
- **Benchmark** : POPCOUNT sur 192 bytes (1536-bit) doit être < 100ns

### Phase 2 — Vamana graph (construction + insertion incrémentale)
Package : `storage/vecindex/vamana.go` + tests

- `BuildVamana()` : construction complète avec 2 passes
- `greedySearch()`, `robustPrune()`, `findMedoid()`
- `InsertSingle()` : insertion d'un nœud dans un graphe existant
- `InsertBatch()` : insertion par lot dans une transaction
- `l2Distance()` optimisée (déroulée par blocs de 8)
- Sérialisation `serializeInt32s()` / `deserializeInt32s()`
- **Tests** : 10K vecteurs dim 128, vérifier accessibilité depuis medoid. Recall@10 ≥ 85% vs brute-force

### Phase 3 — Persistence SQLite + cache
Package : `storage/vecindex/schema.go` + `cache.go`

- `InitSchema()`, `SaveGraph()`, `LoadIndex()`
- `loadNode()` avec intégration cache LRU
- `NodeLRU` : cache borné, éviction LRU, warm-up du medoid + voisinage au démarrage
- **Tests** : round-trip (build → save → load → search → verify recall)

### Phase 4 — Search (beam search + reranking)
Package : `storage/vecindex/search.go`

- `VectorIndex.Search()` : beam search RaBitQ + reranking vecteurs complets
- `beamSearchCached()` : beam search utilisant le cache LRU
- `rerankWithFullVectors()` : charge top-N vecteurs depuis `embeddings`, recalcule cosine exacte
- **Tests** : 100K vecteurs, recall@10 ≥ 90% vs brute-force. Latence < 10ms

### Phase 5 — Intégration dans handlers
- `handlers/search.go` : remplacer `vectorSearchByEmbedding()`, garder fallback brute-force
- `Handlers` struct : ajouter `VecIndex *vecindex.VectorIndex`
- `handlers/embed.go` + `background_embed.go` : après chaque batch, appeler `InsertBatch()`
- Vérifier `NeedsRebuild()` après chaque insertion, déclencher `RebuildAsync()` si nécessaire
- Commande CLI `buildindex` pour le build initial

### Phase 6 — Optimisations grande échelle
- Parallélisation du build (pool de goroutines + lock par nœud)
- Build par streaming pour >10M vecteurs (pas tout en RAM)
- Warm-up du cache au démarrage (pré-charger les nœuds les plus connectés)
- Métriques observables : drift ratio, insert counter, rebuild frequency, search latency P50/P99

---

## 10. Performance attendue aux volumes réels

### Search latency : V1 vs V2

| N chunks | V1 (brute-force float32) | V2 brute-force RaBitQ | V2 Vamana + RaBitQ |
|----------|--------------------------|----------------------|-------------------|
| 100K     | ~50ms                    | ~5ms (POPCOUNT)      | ~2ms              |
| 1M       | ~500ms (limite)          | ~50ms                | ~5ms              |
| 10M      | ~5s (inutilisable)       | ~500ms (limite)      | ~10ms             |
| 100M     | impossible               | ~5s (impossible)     | ~20ms             |

**Vamana est obligatoire dès 1M vecteurs.** Le brute-force RaBitQ (scan linéaire des codes 1-bit) est 10× plus rapide que le brute-force float32 grâce au POPCOUNT, mais reste O(N). Le graphe Vamana donne O(log N).

### RAM/Disque par volume

| N vecteurs | Embeddings complets (disque) | Index Vamana+RaBitQ | Cache LRU recommandé |
|-----------|-----------------------------|--------------------|---------------------|
| 100K      | 614 MB                      | 47 MB              | 47 MB (tout)        |
| 1M        | 6.1 GB                      | 450 MB             | 100-200 MB          |
| 10M       | 61 GB                       | 4.5 GB             | 500 MB - 1 GB       |
| 100M      | 614 GB                      | 45 GB              | 2-5 GB              |

### Coût de construction

| N vecteurs | Build complet (2 passes) | Insertion incrémentale (par vecteur) |
|-----------|-------------------------|--------------------------------------|
| 100K      | ~2-5 min                | ~0.5ms                               |
| 1M        | ~1-3h                   | ~2-5ms                               |
| 10M       | ~12-30h                 | ~5-15ms                              |

### Insertion incrémentale vs rebuild

| Scénario | Action | Coût |
|----------|--------|------|
| Nouveau document (100 chunks) | `InsertBatch()` | ~200-500ms |
| Drift centroïde < 5%, inserts < 30% | Rien | 0 |
| Drift centroïde > 5% OU inserts > 30% | `RebuildAsync()` en background | 1-3h (1M vecteurs) |
| Rebuild nocturne planifié | Cron/ticker | Prévisible |

---

## 11. Dépendances et contraintes

### Aucune nouvelle dépendance Go
Tout est implémentable avec la bibliothèque standard :
- `math/bits` pour `OnesCount64()` (POPCOUNT)
- `encoding/binary` pour sérialisation
- `math` pour `Sqrt()`
- `math/rand/v2` pour les permutations aléatoires
- `container/heap` pour les heaps
- `database/sql` pour SQLite (déjà importé)
- `sync` pour les locks du cache LRU et du build parallélisé

### Contrainte CGO_ENABLED=0 : respectée
Aucun code C, aucune dépendance CGO. Tout est pure Go + `modernc.org/sqlite`.

### Pas de matrice de rotation — décision définitive pour le MVP
Le plan initial mentionnait une rotation orthogonale pour améliorer le recall RaBitQ. **Ne pas l'implémenter** :
- Gram-Schmidt est numériquement instable en float32 à 1536 dimensions (les derniers vecteurs orthogonalisés accumulent assez d'erreur pour casser la propriété d'isométrie)
- La matrice D×D fait 9.4 MB — stockage et multiplication O(D²) par encodage
- La perte sans rotation est ~5% de recall, acceptable
- Si le recall sans rotation s'avère insuffisant un jour, passer directement à **FWHT** (Fast Walsh-Hadamard Transform) — jamais Gram-Schmidt. La FWHT est O(D log D), ~20 lignes de code, numériquement stable

### Compatibilité avec le pipeline existant
- La table `embeddings` N'EST PAS MODIFIÉE — source de vérité pour les vecteurs complets
- `vec_index` est un index dérivé, reconstructible à tout moment depuis `embeddings`
- Le fallback brute-force reste disponible si l'index n'est pas construit
- La recherche hybride FTS5 + vecteurs continue de fonctionner (seul le composant vectoriel change)
- L'insertion incrémentale se branche sur le pipeline existant (`handlers/embed.go` → `InsertBatch()`)

### Extraction future dans hazyhaar/pkg
Le package `rabitq` (encodeur, distance POPCOUNT, centroid tracker) est réutilisable indépendamment de Vamana. ~200 lignes, zéro dépendance. Candidat pour extraction dans `github.com/hazyhaar/pkg/rabitq` une fois stabilisé. Le graphe Vamana reste dans le projet applicatif tant qu'il n'est pas prouvé réutilisable.
