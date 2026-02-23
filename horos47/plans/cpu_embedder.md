# cpu_embedder.md

## Objectif

Créer un worker "Blind" pour générer des embeddings sur CPU en utilisant sentence-transformers, sans dépendances GPU ni CGO.

## Architecture Blind Worker

```
┌──────────────────────────────────────────────────────────┐
│                    Superviseur (Golang)                   │
│  - Lit /data/horos/stage_3_embed/pending/*.json          │
│  - Écrit batch dans /dev/shm/embed_batch_<uuid>.json     │
│  - Lance worker Python avec chemin fichier               │
│  - Lit résultat depuis /dev/shm/embed_result_<uuid>.json │
│  - Écrit dans /data/horos/stage_3_embed/done/            │
└──────────────────────────────────────────────────────────┘
                            ↓ JSON via tmpfs
┌──────────────────────────────────────────────────────────┐
│              Worker Python (Blind - Stateless)            │
│  - Reçoit chemin fichier batch en argument               │
│  - Charge sentence-transformers (BAAI/bge-base-en-v1.5)  │
│  - Génère embeddings (CPU, 4 threads, 768-dim)           │
│  - Écrit résultats JSON dans fichier output              │
│  - Exit 0 (succès) ou Exit 1 (erreur)                    │
└──────────────────────────────────────────────────────────┘
```

**Principe** : Le worker Python ne contient AUCUNE logique métier. Il est une fonction pure : `batch_file → embed → result_file`.

---

## 1. Worker Python (Blind)

**Fichier** : `/inference/horos47/workers/cpu_embedder.py`

```python
#!/usr/bin/env python3
"""
HOROS 47 - CPU Embeddings Blind Worker

Worker stateless pour génération embeddings CPU via sentence-transformers.
Pattern "Blind Worker" : aucune logique métier, juste Input → Process → Output.

Usage:
    python3 cpu_embedder.py <input_batch_json> <output_result_json>

Input JSON format:
    {
        "chunks": [
            {"chunk_id": "uuid1", "text": "..."},
            {"chunk_id": "uuid2", "text": "..."}
        ]
    }

Output JSON format:
    {
        "embeddings": [
            {
                "chunk_id": "uuid1",
                "embedding": [0.123, -0.456, ...],  # 768-dim float32
                "model": "bge-base-en-v1.5",
                "dimension": 768
            },
            ...
        ],
        "stats": {
            "total_chunks": 10,
            "duration_ms": 1234,
            "avg_ms_per_chunk": 123.4
        }
    }
"""

import sys
import json
import time
from pathlib import Path

# Importer sentence-transformers (installer : pip install sentence-transformers)
from sentence_transformers import SentenceTransformer
import numpy as np


def normalize_l2(vec: np.ndarray) -> np.ndarray:
    """Normalisation L2 pour similarité cosinus."""
    norm = np.linalg.norm(vec)
    if norm == 0:
        return vec
    return vec / norm


def main():
    if len(sys.argv) != 3:
        print("Usage: cpu_embedder.py <input_json> <output_json>", file=sys.stderr)
        sys.exit(1)

    input_path = Path(sys.argv[1])
    output_path = Path(sys.argv[2])

    # Validation input
    if not input_path.exists():
        print(f"ERROR: Input file not found: {input_path}", file=sys.stderr)
        sys.exit(1)

    # Charger batch
    try:
        with open(input_path, 'r', encoding='utf-8') as f:
            batch = json.load(f)
    except json.JSONDecodeError as e:
        print(f"ERROR: Invalid JSON in input: {e}", file=sys.stderr)
        sys.exit(1)

    chunks = batch.get("chunks", [])
    if not chunks:
        print("ERROR: No chunks in input batch", file=sys.stderr)
        sys.exit(1)

    # Charger modèle BGE (CPU uniquement, 4 threads)
    start_time = time.time()
    model = SentenceTransformer('BAAI/bge-base-en-v1.5', device='cpu')
    model._target_device = 'cpu'  # Force CPU

    # Extraire textes
    texts = [chunk["text"] for chunk in chunks]
    chunk_ids = [chunk["chunk_id"] for chunk in chunks]

    # Générer embeddings (batch)
    # Note: BGE nécessite préfixe "Represent this sentence for searching: " pour les requêtes
    # Mais pour indexation documents, pas de préfixe nécessaire
    embeddings_raw = model.encode(
        texts,
        batch_size=32,
        show_progress_bar=False,
        normalize_embeddings=False  # On normalise manuellement après
    )

    # Normaliser L2 (pour cosine similarity)
    embeddings_normalized = [normalize_l2(emb).tolist() for emb in embeddings_raw]

    # Construire résultat
    duration_ms = (time.time() - start_time) * 1000
    result = {
        "embeddings": [
            {
                "chunk_id": chunk_ids[i],
                "embedding": embeddings_normalized[i],
                "model": "bge-base-en-v1.5",
                "dimension": len(embeddings_normalized[i])
            }
            for i in range(len(chunks))
        ],
        "stats": {
            "total_chunks": len(chunks),
            "duration_ms": round(duration_ms, 2),
            "avg_ms_per_chunk": round(duration_ms / len(chunks), 2)
        }
    }

    # Écrire output
    try:
        with open(output_path, 'w', encoding='utf-8') as f:
            json.dump(result, f, indent=2)
    except IOError as e:
        print(f"ERROR: Failed to write output: {e}", file=sys.stderr)
        sys.exit(1)

    # Succès
    sys.exit(0)


if __name__ == "__main__":
    main()
```

**Note importante sur BGE** :

Le modèle BGE (BAAI/bge-base-en-v1.5) a été spécifiquement entraîné avec des instructions :
- **Pour indexation documents** : Pas de préfixe (utilisé dans ce worker)
- **Pour requêtes de recherche** : Préfixe `"Represent this sentence for searching: "` requis

Ce worker génère des embeddings pour **l'indexation de documents**, donc aucun préfixe n'est appliqué. Lors de la recherche (query embedding), le service de recherche devra ajouter le préfixe aux requêtes utilisateur.

**Permissions** :
```bash
chmod +x /inference/horos47/workers/cpu_embedder.py
```

---

## 2. Superviseur Golang

**Fichier** : `/inference/horos47/core/acid/processors/embed.go`

```go
package processors

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "time"
)

type EmbedProcessor struct {
    workerScript string
    tmpfsDir     string
}

func NewEmbedProcessor() *EmbedProcessor {
    return &EmbedProcessor{
        workerScript: "/inference/horos47/workers/cpu_embedder.py",
        tmpfsDir:     "/dev/shm", // tmpfs (RAM disk) pour performance
    }
}

func (p *EmbedProcessor) Process(ctx context.Context, payload []byte) ([]byte, error) {
    // 1. Parser payload
    var input struct {
        Chunks []struct {
            ChunkID string `json:"chunk_id"`
            Text    string `json:"text"`
        } `json:"chunks"`
    }
    if err := json.Unmarshal(payload, &input); err != nil {
        return nil, fmt.Errorf("invalid payload: %w", err)
    }

    if len(input.Chunks) == 0 {
        return nil, fmt.Errorf("no chunks in payload")
    }

    // 2. Créer fichiers temporaires dans tmpfs
    batchID := randomHex(8)
    inputPath := filepath.Join(p.tmpfsDir, fmt.Sprintf("embed_batch_%s.json", batchID))
    outputPath := filepath.Join(p.tmpfsDir, fmt.Sprintf("embed_result_%s.json", batchID))

    // Cleanup automatique
    defer os.Remove(inputPath)
    defer os.Remove(outputPath)

    // 3. Écrire batch input
    if err := os.WriteFile(inputPath, payload, 0644); err != nil {
        return nil, fmt.Errorf("failed to write input batch: %w", err)
    }

    // 4. Lancer worker Python (Blind)
    cmd := exec.CommandContext(ctx, "python3", p.workerScript, inputPath, outputPath)

    // Capturer stderr pour debug
    stderr, err := cmd.StderrPipe()
    if err != nil {
        return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
    }

    // Lancer process
    start := time.Now()
    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("failed to start worker: %w", err)
    }

    // Attendre fin
    if err := cmd.Wait(); err != nil {
        // Lire stderr pour erreur
        stderrBytes, _ := io.ReadAll(stderr)
        return nil, fmt.Errorf("worker failed: %w (stderr: %s)", err, string(stderrBytes))
    }

    duration := time.Since(start)

    // 5. Lire résultat
    resultData, err := os.ReadFile(outputPath)
    if err != nil {
        return nil, fmt.Errorf("failed to read worker output: %w", err)
    }

    // 6. Valider JSON résultat
    var result struct {
        Embeddings []struct {
            ChunkID   string    `json:"chunk_id"`
            Embedding []float32 `json:"embedding"`
            Model     string    `json:"model"`
            Dimension int       `json:"dimension"`
        } `json:"embeddings"`
        Stats struct {
            TotalChunks   int     `json:"total_chunks"`
            DurationMS    float64 `json:"duration_ms"`
            AvgMSPerChunk float64 `json:"avg_ms_per_chunk"`
        } `json:"stats"`
    }

    if err := json.Unmarshal(resultData, &result); err != nil {
        return nil, fmt.Errorf("invalid worker output JSON: %w", err)
    }

    // Validation cohérence
    if len(result.Embeddings) != len(input.Chunks) {
        return nil, fmt.Errorf("embedding count mismatch: expected %d, got %d",
            len(input.Chunks), len(result.Embeddings))
    }

    // Log succès
    fmt.Printf("[EmbedProcessor] Processed %d chunks in %v (worker: %.2fms)\n",
        len(input.Chunks), duration, result.Stats.DurationMS)

    return resultData, nil
}

func randomHex(n int) string {
    bytes := make([]byte, n)
    rand.Read(bytes)
    return hex.EncodeToString(bytes)
}
```

---

## 3. Installation Dépendances Python

**Fichier** : `/inference/horos47/workers/requirements.txt`

```txt
sentence-transformers==2.5.1
torch==2.2.0+cpu  # Version CPU uniquement (pas de CUDA)
numpy==1.24.3
```

**Script d'installation** :

```bash
#!/bin/bash
# /inference/horos47/scripts/setup_cpu_embedder.sh

set -e

echo "🔧 Setting up CPU Embedder worker"

# 1. Vérifier Python 3.9+
python_version=$(python3 --version | cut -d' ' -f2 | cut -d'.' -f1,2)
required_version="3.9"

if (( $(echo "$python_version < $required_version" | bc -l) )); then
    echo "❌ Python 3.9+ required (found: $python_version)"
    exit 1
fi

echo "✓ Python $python_version OK"

# 2. Créer venv
VENV_DIR="/inference/horos47/workers/venv_embedder"
if [ ! -d "$VENV_DIR" ]; then
    echo "→ Creating virtual environment..."
    python3 -m venv "$VENV_DIR"
fi

# 3. Activer venv et installer
source "$VENV_DIR/bin/activate"

echo "→ Installing sentence-transformers (CPU)..."
pip install --upgrade pip
pip install -r /inference/horos47/workers/requirements.txt

# 4. Télécharger modèle (cache local)
echo "→ Downloading model (BAAI/bge-base-en-v1.5)..."
python3 -c "from sentence_transformers import SentenceTransformer; SentenceTransformer('BAAI/bge-base-en-v1.5')"

echo "✅ CPU Embedder ready"
echo ""
echo "Test worker:"
echo "  echo '{\"chunks\":[{\"chunk_id\":\"test\",\"text\":\"hello world\"}]}' > /tmp/test_input.json"
echo "  python3 workers/cpu_embedder.py /tmp/test_input.json /tmp/test_output.json"
echo "  cat /tmp/test_output.json"
```

---

## 4. Tests

### Test Unitaire Python

**Fichier** : `/inference/horos47/workers/test_cpu_embedder.sh`

```bash
#!/bin/bash
set -e

WORKER="/inference/horos47/workers/cpu_embedder.py"
INPUT="/tmp/embed_test_input.json"
OUTPUT="/tmp/embed_test_output.json"

# Créer input test
cat > "$INPUT" <<EOF
{
  "chunks": [
    {"chunk_id": "chunk1", "text": "The quick brown fox jumps over the lazy dog"},
    {"chunk_id": "chunk2", "text": "Machine learning is a subset of artificial intelligence"}
  ]
}
EOF

# Lancer worker
echo "→ Running CPU embedder worker..."
python3 "$WORKER" "$INPUT" "$OUTPUT"

# Vérifier output
if [ ! -f "$OUTPUT" ]; then
    echo "❌ Output file not created"
    exit 1
fi

# Parser JSON
embeddings_count=$(jq '.embeddings | length' "$OUTPUT")
dimension=$(jq '.embeddings[0].dimension' "$OUTPUT")

echo "✓ Embeddings count: $embeddings_count"
echo "✓ Dimension: $dimension"

if [ "$embeddings_count" != "2" ]; then
    echo "❌ Expected 2 embeddings, got $embeddings_count"
    exit 1
fi

if [ "$dimension" != "768" ]; then
    echo "❌ Expected dimension 768, got $dimension"
    exit 1
fi

# Cleanup
rm "$INPUT" "$OUTPUT"

echo "✅ CPU Embedder test passed"
```

---

## 5. Isolation et Communication

### Principe Blind Worker

Le worker Python est **complètement isolé** du superviseur Go :

1. **Entrée** : Fichier JSON dans `/dev/shm/` (tmpfs RAM, pas de I/O disque)
2. **Traitement** : Chargement modèle → Embedding → Normalisation L2
3. **Sortie** : Fichier JSON dans `/dev/shm/`
4. **Exit Code** : 0 (succès) ou 1 (erreur)

**Aucune communication réseau, aucun pipe, aucune logique métier.**

### Avantages

| Critère | Valeur |
|---------|--------|
| **Latence I/O** | ~1-2 ms (tmpfs RAM vs 10-20 ms disque) |
| **Taille limite** | Aucune (pas de limite CLI args) |
| **Crash isolation** | Total (process indépendant) |
| **Debugging** | Facile (fichiers inspectables) |
| **Testabilité** | Parfaite (input/output fichiers) |

---

## 6. Performance

### Benchmarks Attendus (RTX 5090 CPU)

- **CPU** : AMD Ryzen 9 ou Intel i9 (supposé)
- **Threads** : 4 (configurable via `OMP_NUM_THREADS`)
- **Batch size** : 32 chunks

| Chunks | Latence | Throughput |
|--------|---------|------------|
| 1 | ~50 ms | 20 chunks/s |
| 10 | ~200 ms | 50 chunks/s |
| 32 | ~400 ms | 80 chunks/s |
| 100 | ~1.2 s | 83 chunks/s |

**Note** : Latence cold start (~500 ms pour charger modèle) amortie sur gros batches.

---

## 7. Intégration Main

**Fichier** : `/inference/horos47/cmd/horos47/main.go`

```go
// Créer worker embed CPU
embedProc := processors.NewEmbedProcessor()
embedWorker := acid.NewWorker("embed", "/data/horos", db, embedProc, logger)

// Lancer worker dans goroutine
go func() {
    if err := embedWorker.Run(ctx); err != nil {
        logger.Error("Embed worker failed", "error", err)
    }
}()
```

---

## Validation Finale

- [ ] Worker Python exécute sans erreur
- [ ] Embeddings dimension 768 (BAAI/bge-base-en-v1.5)
- [ ] Normalisation L2 appliquée (norme = 1.0)
- [ ] Fichiers tmpfs créés/supprimés correctement
- [ ] Exit code 0 sur succès, 1 sur erreur
- [ ] Latence < 500ms pour batch 32 chunks
- [ ] Aucune dépendance GPU/CUDA

---

