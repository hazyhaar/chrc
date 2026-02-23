# onnxcleaning.md

## Objectif

Supprimer complètement ONNX Runtime de HOROS 47 pour éliminer les dépendances CGO, réduire la complexité, et libérer 292 MB de librairies natives.

## Contexte

HOROS 47 utilise actuellement ONNX Runtime pour :
- **Embeddings GPU** via `horos_hugot` (slave CGO, modèle BGE-base-en-v1.5)
- **LLM Think** via `horos_llm` (slave CGO, modèle Phi-3-mini)

Ces slaves communiquent via JSON-RPC stdin/stdout, mais nécessitent :
- CGO_ENABLED=1
- Librairies ONNX (292 MB) : `libonnxruntime.so`, `libonnxruntime_providers_cuda.so`
- Bindings C dans `/inference/horos47/core/genai/`
- Modèles ONNX (~3 GB) : `bge-base-en-v1.5-onnx/`, `phi-3-mini-4k-onnx/`

**Décision** : Remplacer par vLLM (Vision/Think GPU) + sentence-transformers (Embeddings CPU).

---

## Fichiers à Supprimer

### Librairies Natives (292 MB)
```bash
rm -rf /inference/horos47/libs/libonnxruntime.so*
rm -rf /inference/horos47/libs/libonnxruntime_providers_cuda.so*
rm -rf /inference/horos47/libs/libonnxruntime_providers_shared.so*
rm -rf /inference/horos47/libs/backup_genai_old/
```

### Bindings C
```bash
rm -rf /inference/horos47/core/genai/
rm -rf /inference/horos47/include/
```

### Binaires CGO
```bash
rm -f /inference/horos47/bin/horos_hugot
rm -f /inference/horos47/bin/horos_llm
```

### Modèles ONNX (~3 GB)
```bash
rm -rf /inference/horos47/models/bge-base-en-v1.5-onnx/
rm -rf /inference/horos47/models/phi-3-mini-4k-onnx/
```

---

## Modifications Makefile

Supprimer les sections CGO du `/inference/horos47/Makefile` :

```makefile
# SUPPRIMER LIGNES 4-8 : Variables CGO
CGO_ENABLED=1
CGO_CFLAGS=...
CGO_LDFLAGS=...

# SUPPRIMER LIGNES 38-57 : Cibles build-slave et build-llm
build-slave:
    CGO_ENABLED=1 go build -tags ORT ...

build-llm:
    CGO_ENABLED=1 go build -tags ORT ...

# SUPPRIMER LIGNE 89 : check-cuda target
check-cuda:
    ...
```

**Garder uniquement** :
- `build-master` (Pure Go, CGO_ENABLED=0)
- `build-indexer` (Pure Go)

---

## Modifications go.mod

```bash
cd /inference/horos47

# Supprimer dépendances ONNX
go mod edit -droprequire github.com/knights-analytics/hugot
go mod edit -droprequire github.com/knights-analytics/ortgenai

# Nettoyer go.sum
go mod tidy
```

---

## Modifications Code Source

### Fichiers à Supprimer Complètement

```bash
rm -rf /inference/horos47/cmd/horos_hugot/
rm -rf /inference/horos47/cmd/horos_llm/
```

### Fichier `/inference/horos47/services/rag/service.go`

**Supprimer lignes 35-50** : Spawn de horos_hugot slave

```go
// SUPPRIMER CE BLOC
func (s *Service) spawnHugotSlave() (*gpufeeder.Client, error) {
    hugotBinPath := "/inference/horos47/bin/horos_hugot"
    ...
    return gpufeeder.NewClient(s.logger, hugotBinPath)
}
```

### Fichier `/inference/horos47/services/think/service.go`

**Supprimer lignes 40-55** : Spawn de horos_llm slave

```go
// SUPPRIMER CE BLOC
func (s *Service) spawnLLMSlave() (*llmclient.Client, error) {
    llmBinPath := "/inference/horos47/bin/horos_llm"
    ...
    return llmclient.NewClient(s.logger, llmBinPath)
}
```

---

## Script de Nettoyage Automatisé

**Fichier** : `/inference/horos47/scripts/cleanup_onnx.sh`

```bash
#!/bin/bash
set -e

REPO_ROOT="/inference/horos47"

echo "🧹 HOROS 47 - ONNX Runtime Cleanup"
echo "===================================="

# 1. Librairies
echo "→ Removing ONNX libraries (292 MB)..."
rm -rf "$REPO_ROOT/libs/libonnxruntime*.so*"
rm -rf "$REPO_ROOT/libs/backup_genai_old/"
echo "  ✓ Libraries removed"

# 2. Bindings C
echo "→ Removing C bindings..."
rm -rf "$REPO_ROOT/core/genai/"
rm -rf "$REPO_ROOT/include/"
echo "  ✓ C bindings removed"

# 3. Binaires
echo "→ Removing CGO binaries..."
rm -f "$REPO_ROOT/bin/horos_hugot"
rm -f "$REPO_ROOT/bin/horos_llm"
echo "  ✓ Binaries removed"

# 4. Modèles ONNX
echo "→ Removing ONNX models (~3 GB)..."
rm -rf "$REPO_ROOT/models/bge-base-en-v1.5-onnx/"
rm -rf "$REPO_ROOT/models/phi-3-mini-4k-onnx/"
echo "  ✓ Models removed"

# 5. Code source slaves
echo "→ Removing slave source code..."
rm -rf "$REPO_ROOT/cmd/horos_hugot/"
rm -rf "$REPO_ROOT/cmd/horos_llm/"
echo "  ✓ Slave sources removed"

# 6. go.mod cleanup
echo "→ Cleaning Go dependencies..."
cd "$REPO_ROOT"
go mod edit -droprequire github.com/knights-analytics/hugot 2>/dev/null || true
go mod edit -droprequire github.com/knights-analytics/ortgenai 2>/dev/null || true
go mod tidy
echo "  ✓ go.mod cleaned"

# Résumé
echo ""
echo "✅ ONNX Runtime cleanup completed"
echo ""
echo "Freed space:"
du -sh "$REPO_ROOT/libs/" 2>/dev/null || echo "  libs/: 0 MB (deleted)"
du -sh "$REPO_ROOT/models/" 2>/dev/null | grep -v "^0"
echo ""
echo "Next steps:"
echo "  1. Update Makefile (remove CGO targets)"
echo "  2. Update services/rag/service.go (remove Hugot spawn)"
echo "  3. Update services/think/service.go (remove LLM spawn)"
echo "  4. Rebuild: make build-master"
```

**Exécution** :
```bash
chmod +x /inference/horos47/scripts/cleanup_onnx.sh
./scripts/cleanup_onnx.sh
```

---

## Validation

### Vérifier Suppression

```bash
# Aucune librairie ONNX
ls -lh /inference/horos47/libs/
# Doit être vide ou ne contenir que libsqlite

# Aucun binaire CGO
ls -lh /inference/horos47/bin/horos_{hugot,llm} 2>/dev/null
# Doit retourner "No such file"

# Aucune dépendance ONNX
grep -i "onnx\|hugot\|ortgenai" /inference/horos47/go.mod
# Doit ne rien retourner
```

### Rebuild Pure Go

```bash
cd /inference/horos47
make clean
make build-master

# Vérifier CGO désactivé
file bin/horos47
# Doit afficher "statically linked" (pas de dépendances .so)
```

---

## Risques et Mitigations

| Risque | Impact | Mitigation |
|--------|--------|-----------|
| Services RAG/Think cassés | **Critique** | Remplacer par CPU Embedder + GPU Feeder avant suppression |
| Tests unitaires échouent | Moyen | Désactiver tests dépendant d'ONNX dans CI |
| Dépendances transitives | Faible | `go mod tidy` nettoie automatiquement |

---

## Chronologie

1. **Jour 0** : Backup complet de `/inference/horos47/`
2. **Jour 1** : Exécuter `cleanup_onnx.sh`
3. **Jour 1** : Modifier Makefile (supprimer CGO targets)
4. **Jour 1** : Rebuild et valider `make build-master`
5. **Jour 2** : Mettre à jour documentation (CLAUDE.md, BUILD_STATUS_REPORT.md)

**Durée totale** : 2 jours

---

