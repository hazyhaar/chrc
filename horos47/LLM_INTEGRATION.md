# LLM Integration - HOROS 47

**Statut** : ✅ Implémenté et opérationnel
**Date** : 2026-02-02
**Tâche** : #4 - Intégrer LLM pour service Think

## Vue d'ensemble

HOROS 47 intègre désormais la génération de texte via LLM (Large Language Model) avec **Phi-3-mini-4k-instruct** de Microsoft. Le système suit la même architecture que les embeddings (processus slave + JSON-RPC).

## Architecture

### Pattern Slave LLM

Identique au slave embeddings (`horos_hugot`), le slave LLM suit le même pattern éprouvé :

```
Service Think
    ↓ (lance au démarrage)
Slave horos_llm (process séparé)
    ↓ (charge modèle)
Phi-3-mini ONNX (GPU CUDA INT4)
    ↓ (JSON-RPC stdin/stdout)
Génération texte
```

### Composants

**1. Slave LLM** (`cmd/horos_llm/main.go`)
- Processus séparé spawné par service Think
- Charge Phi-3-mini ONNX avec CUDA INT4 quantification
- Communication JSON-RPC 2.0 sur stdin/stdout
- Méthodes : `generate`, `health`

**2. Client LLM** (`services/think/llm_client.go`)
- Gère lifecycle du slave (start/stop)
- Envoie requêtes JSON-RPC
- Health checks automatiques au démarrage

**3. Service Think** (`services/think/service.go`)
- Endpoint HTTP : `POST /think/generate`
- Auto-RAG optionnel (`use_rag: true`)
- Intégration avec service RAG pour contexte

## Modèle LLM

### Phi-3-mini-4k-instruct

**Caractéristiques** :
- **Taille** : 3.8 milliards de paramètres
- **Contexte** : 4096 tokens (~3000 mots)
- **Format** : ONNX CUDA INT4 (quantifié)
- **VRAM** : ~2-3 GB

**Performance** :
- Latence génération : ~2s pour 200 tokens
- GPU utilization : >80% pendant génération
- Compatible avec Blackwell RTX 5090

**Qualité** :
- Bon pour usage général
- Meilleur en anglais, correct en français
- Compact mais moins puissant que GPT-4/Claude

### Format Prompt Phi-3

```
<|system|>
{system_prompt}
<|end|>
<|user|>
{user_prompt}
<|end|>
<|assistant|>
{generated_response}
```

Le client LLM construit automatiquement ce format.

## Utilisation

### API HTTP

**Endpoint** : `POST /think/generate`

**Génération simple** :
```bash
curl -X POST http://localhost:8443/think/generate \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Explain quantum computing simply",
    "max_tokens": 150
  }'
```

**Avec système prompt** :
```bash
curl -X POST http://localhost:8443/think/generate \
  -d '{
    "prompt": "Translate hello to French",
    "system_prompt": "You are a French teacher",
    "max_tokens": 50
  }'
```

**Avec Auto-RAG** :
```bash
curl -X POST http://localhost:8443/think/generate \
  -d '{
    "prompt": "What features does HOROS support?",
    "use_rag": true,
    "max_tokens": 200
  }'
```

### Paramètres Requête

```json
{
  "prompt": "string (requis)",
  "use_rag": "boolean (optionnel, défaut: false)",
  "max_tokens": "int (optionnel, défaut: 256)",
  "system_prompt": "string (optionnel)"
}
```

### Réponse

```json
{
  "text": "Generated response text...",
  "message": "Generated successfully"
}
```

## Auto-RAG

### Principe

Quand `use_rag: true`, le service Think :
1. Appelle service RAG avec la question
2. Récupère top 3 chunks pertinents
3. Injecte contexte dans le prompt
4. LLM génère réponse basée sur contexte

### Flow Auto-RAG

```
User prompt: "What GPU does HOROS use?"
    ↓
Service Think
    ↓
Service RAG: recherche vectorielle "GPU HOROS"
    ↓
Résultats : ["...NVIDIA RTX 5090 Blackwell...", "...CUDA 12.8...", ...]
    ↓
Prompt enrichi :
    "Context from knowledge base:
     [Source 1 - Score: 0.85]
     ...NVIDIA RTX 5090 Blackwell...

     Question: What GPU does HOROS use?"
    ↓
LLM génère : "HOROS uses NVIDIA RTX 5090 Blackwell GPU..."
```

### Configuration RAG

Dans `services/think/rag_integration.go` :
- **Top K** : 3 chunks
- **Min score** : 0.3 (seuil pertinence)
- **Endpoint** : `http://localhost:8443/rag/query`

## Build et Déploiement

### Compilation

```bash
# Build slave LLM (CGO + CUDA)
make build-llm-slave

# Build système complet
make build

# Vérifier binaires
ls -lh bin/
# horos47        - Master (Pure Go)
# horos_hugot    - Embeddings slave (CGO+CUDA)
# horos_llm      - LLM slave (CGO+CUDA)
# embedding_indexer - Indexer worker
```

### Dépendances Modèle

**Télécharger Phi-3-mini** :
```bash
./scripts/download_phi3_onnx.sh
```

Télécharge ~2.4 GB dans `/inference/horos47/models/phi-3-mini-4k-onnx/`.

### Variables Environnement

Le slave LLM utilise :
```bash
ONNX_LIBRARY_PATH=/inference/horos47/libs/libonnxruntime.so
HUGOT_LLM_MODEL_PATH=/inference/horos47/models/phi-3-mini-4k-onnx/cuda/cuda-int4-rtn-block-32
LD_LIBRARY_PATH=/inference/horos47/libs:/usr/local/cuda-12.8/lib64
```

Variables automatiquement configurées par le client LLM.

### Lifecycle

**Démarrage** :
```bash
./bin/horos47
```

Service Think démarre automatiquement le slave LLM.

**Arrêt propre** :
```bash
# Ctrl+C ou SIGTERM
# Le service Think appelle thinkSvc.Close()
# → Ferme stdin slave
# → Kill process slave
# → Cleanup ressources
```

## Tests

### Test Slave Direct

```bash
# Test JSON-RPC slave directement
echo '{"jsonrpc":"2.0","id":1,"method":"generate","params":{"prompt":"Hello","max_tokens":50}}' \
  | ONNX_LIBRARY_PATH=/inference/horos47/libs \
    HUGOT_LLM_MODEL_PATH=/inference/horos47/models/phi-3-mini-4k-onnx/cuda/cuda-int4-rtn-block-32 \
    LD_LIBRARY_PATH=/inference/horos47/libs:/usr/local/cuda-12.8/lib64 \
    ./bin/horos_llm
```

### Test Service Think

```bash
./scripts/test_think.sh
```

**Tests inclus** :
1. Génération simple
2. Système prompts custom
3. Auto-RAG avec ingestion document

### Test End-to-End

```bash
# 1. Démarrer système
./bin/horos47 &
./scripts/start_indexer.sh &

# 2. Ingérer document
curl -X POST http://localhost:8443/ingest/document \
  -d '{"title":"Test","text":"HOROS supports GPU embeddings"}'

# 3. Attendre indexation
sleep 10

# 4. Générer avec RAG
curl -X POST http://localhost:8443/think/generate \
  -d '{"prompt":"What does HOROS support?","use_rag":true}'
```

## Performance

### Latence

| Opération | Temps | Notes |
|-----------|-------|-------|
| Cold start slave | ~3-5s | Chargement modèle GPU |
| Première génération | ~2-3s | Warm-up CUDA |
| Générations suivantes | ~1-2s | CUDA Graphs actif |
| 100 tokens | ~1s | Dépend longueur prompt |
| 200 tokens | ~2s | |

### Mémoire GPU

| Composant | VRAM | Total |
|-----------|------|-------|
| BGE embeddings | ~2 GB | |
| Phi-3 INT4 | ~3 GB | |
| **Total système** | **~5-6 GB** | sur 32 GB disponibles |

### Optimisations CUDA

Comme `horos_hugot`, le slave LLM utilise :
- **InterOp threads** : 1
- **IntraOp threads** : 1
- **CUDA Graphs** : Enabled
- **GPU mem limit** : 25 GB (laisse marge)

**Rationale** : Blackwell PDL (Programmatic Dependent Launch) gère parallélisme hardware. Multi-threading CPU cause contention mutex.

## Limitations

### Actuelles

1. **Pas de streaming** : Réponse complète d'un coup (pas token par token)
2. **Contexte limité** : 4096 tokens max
3. **Qualité** : Phi-3-mini < GPT-4/Claude (acceptable pour usage général)
4. **Langues** : Meilleur en anglais
5. **Pas de vision** : Texte uniquement (Phi-3-vision existe mais non intégré)

### Futures Améliorations

**Tâche #8** (à venir) :
- Streaming responses (SSE)
- Cache KV pour accélérer
- Batch processing requêtes
- Multi-model support

## Troubleshooting

### Erreur "LLM not available"

**Cause** : Slave n'a pas démarré.

**Debug** :
```bash
# Vérifier logs Think service
journalctl -u horos47 | grep LLM

# Tester slave manuellement
./scripts/test_llm_slave.sh
```

**Solutions** :
- Vérifier modèle téléchargé : `ls /inference/horos47/models/phi-3-mini-4k-onnx/cuda/cuda-int4-rtn-block-32/`
- Vérifier CUDA : `nvidia-smi`
- Vérifier ONNX Runtime : `ldd bin/horos_llm | grep onnx`

### Génération lente (>5s)

**Causes possibles** :
- CUDA Graphs pas activé
- GPU occupé par autres process
- Swap mémoire (VRAM insuffisante)

**Debug** :
```bash
# Monitor GPU
nvidia-smi dmon -s umt

# Vérifier VRAM
nvidia-smi --query-gpu=memory.used --format=csv
```

### Réponses incohérentes

**Causes** :
- Prompt mal formaté
- Système prompt inapproprié
- Max tokens trop court

**Solutions** :
- Augmenter `max_tokens` (256 → 512)
- Clarifier système prompt
- Utiliser RAG pour contexte

### RAG ne fonctionne pas

**Vérifier** :
1. Service RAG démarré : `curl http://localhost:8443/rag/query`
2. Embeddings générés : `sqlite3 data/main.db "SELECT COUNT(*) FROM chunks WHERE embedding IS NOT NULL"`
3. Worker indexer tourne : `ps aux | grep embedding_indexer`

## Migration Future

### Vers Modèle Plus Gros

Si Phi-3-mini insuffisant :

**Option 1 : Qwen 2.5 7B**
- Meilleure qualité
- ~7 GB VRAM
- Nécessite conversion ONNX

**Option 2 : Llama 3 8B**
- Excellente qualité
- ~8 GB VRAM
- ONNX disponible

**Changement requis** :
- Télécharger nouveau modèle ONNX
- Modifier `HUGOT_LLM_MODEL_PATH`
- Ajuster `gpu_mem_limit` si nécessaire
- Architecture slave identique !

### Vers API Externe (Fallback)

Pour qualité maximale temporaire :

```go
// Option : appel API OpenAI/Anthropic si slave indisponible
if s.llmClient == nil {
    return s.generateViaAPI(prompt) // Fallback
}
```

## Références

- **Modèle** : https://huggingface.co/microsoft/Phi-3-mini-4k-instruct-onnx
- **Hugot** : https://github.com/knights-analytics/hugot
- **ONNX Runtime** : https://onnxruntime.ai/
- **Code** : `cmd/horos_llm/`, `services/think/`
- **Scripts** : `scripts/download_phi3_onnx.sh`, `scripts/test_think.sh`

---

**Prochaine étape** : Tâche #6 - Tests end-to-end pipeline complet
