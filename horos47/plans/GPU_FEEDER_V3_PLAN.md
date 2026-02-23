# GPU Feeder V3 - Plan de Migration
## Fusion Architecture Serveur HTTP Persistant (v1) + Tracking SQLite (v2)

**Date**: 2026-02-04
**Objectif**: Créer GPU Feeder v3 en fusionnant les meilleures parties de v1 et v2

---

## 🎯 Architecture Cible V3

### Principe
- **Serveurs vLLM persistants** (v1) lancés au démarrage, modèle chargé 1×
- **Requêtes HTTP individuelles** vers OpenAI API `/v1/chat/completions`
- **Continuous batching automatique** géré par vLLM (pas de fichiers JSONL)
- **Tracking SQLite** (v2) avec fan-in pour jobs fragmentés
- **Granularité 512 tokens** avec chunking et overlap 120 tokens
- **Orchestration intelligente** (v1) avec stratégie d'allocation dynamique

### Composants à Fusionner

| Composant | Source | Raison |
|-----------|--------|--------|
| `manager.go` | V1 | Lifecycle containers persistants |
| `service.go` | V1 | Orchestration 3 goroutines |
| `allocator.go` | V1 | Stratégie allocation dynamique |
| `monitor.go` | V1 | GPU monitoring nvidia-smi |
| `health.go` | V1 | Health checking continu |
| `schema.sql` | V2 | Table `gpu_jobs` avec fan-in |
| `fanin.go` | V2 | Logique agrégation fragments |
| `db.go` | V2 | Opérations atomiques SQLite |

---

## 📂 Structure Fichiers V3

```
/inference/horos47/
├── cmd/
│   └── gpu_feeder_v3/
│       └── main.go              ← Nouveau: Fusion v1 JSON-RPC + v2 worker
├── services/gpufeeder/
│   ├── service.go               ← V1 (orchestration)
│   ├── manager.go               ← V1 (lifecycle containers)
│   ├── allocator.go             ← V1 (stratégie allocation)
│   ├── monitor.go               ← V1 (GPU monitoring)
│   ├── health.go                ← V1 (health checking)
│   ├── types.go                 ← V1 (structures communes)
│   ├── worker.go                ← NOUVEAU: Worker qui poll SQLite
│   ├── db.go                    ← V2 (opérations SQLite)
│   ├── fanin.go                 ← V2 (agrégation fragments)
│   ├── schema.sql               ← V2 (table gpu_jobs)
│   ├── http_client.go           ← NOUVEAU: Client HTTP pour vLLM
│   └── config.go                ← NOUVEAU: Configuration v3
└── bin/
    └── gpu_feeder_v3            ← Binaire final
```

---

## 🔧 Modifications Critiques

### 1. Nouveau Fichier: `worker.go`

**Rôle**: Poll la table `gpu_jobs` et envoie requêtes HTTP vers serveurs vLLM persistants.

```go
package gpufeeder

// Worker poll gpu_jobs et envoie vers vLLM HTTP
type Worker struct {
    db          *sql.DB
    logger      *slog.Logger
    manager     *ProcessManager
    httpClient  *VLLMHTTPClient
    pollInterval time.Duration
}

// Run démarre boucle de polling
func (w *Worker) Run(ctx context.Context) error {
    ticker := time.NewTicker(w.pollInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            w.processBatch(ctx)
        }
    }
}

// processBatch claim batch et envoie requêtes HTTP
func (w *Worker) processBatch(ctx context.Context) error {
    // 1. Décider modèle (Think prioritaire)
    modelType := w.selectModel(ctx)
    if modelType == "" {
        return nil
    }

    // 2. Claim batch atomique
    jobs, err := w.claimBatch(ctx, modelType, batchSize)
    if err != nil {
        return err
    }

    // 3. Vérifier que serveur vLLM est actif
    serverURL := w.getServerURL(modelType)
    if !w.manager.IsInstanceRunning(modelType) {
        w.logger.Warn("vLLM instance not running", "model", modelType)
        w.releaseBatch(ctx, jobs)
        return nil
    }

    // 4. Envoyer requêtes HTTP en parallèle
    var wg sync.WaitGroup
    for _, job := range jobs {
        wg.Add(1)
        go func(j Job) {
            defer wg.Done()
            w.processJob(ctx, j, serverURL)
        }(job)
    }
    wg.Wait()

    return nil
}

// processJob envoie 1 requête HTTP vers vLLM
func (w *Worker) processJob(ctx context.Context, job Job, serverURL string) error {
    // Parser payload
    payload, err := w.loadPayload(job.PayloadPath)
    if err != nil {
        w.failJob(ctx, job, err)
        return err
    }

    // Construire requête OpenAI
    req := w.buildVLLMRequest(payload, job.ModelType)

    // Envoyer HTTP POST
    resp, err := w.httpClient.SendRequest(ctx, serverURL, req)
    if err != nil {
        w.failJob(ctx, job, err)
        return err
    }

    // Sauver résultat
    if err := w.completeJob(ctx, job, resp); err != nil {
        return err
    }

    // Check fan-in
    w.checkFanIn(ctx, job.ID)

    return nil
}
```

### 2. Nouveau Fichier: `http_client.go`

**Rôle**: Client HTTP réutilisable pour communiquer avec serveurs vLLM.

```go
package gpufeeder

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

// VLLMHTTPClient client HTTP pour vLLM OpenAI API
type VLLMHTTPClient struct {
    client *http.Client
    logger *slog.Logger
}

func NewVLLMHTTPClient(logger *slog.Logger) *VLLMHTTPClient {
    return &VLLMHTTPClient{
        client: &http.Client{
            Timeout: 120 * time.Second,
        },
        logger: logger,
    }
}

// SendRequest envoie requête vers vLLM et retourne réponse
func (c *VLLMHTTPClient) SendRequest(ctx context.Context, serverURL string, req VLLMRequest) (*VLLMResponse, error) {
    reqJSON, err := json.Marshal(req)
    if err != nil {
        return nil, fmt.Errorf("marshal request: %w", err)
    }

    httpReq, err := http.NewRequestWithContext(ctx, "POST", serverURL+"/v1/chat/completions", bytes.NewReader(reqJSON))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")

    c.logger.Debug("Sending vLLM request", "url", serverURL, "size", len(reqJSON))

    resp, err := c.client.Do(httpReq)
    if err != nil {
        return nil, fmt.Errorf("http request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("vLLM error %d: %s", resp.StatusCode, string(body))
    }

    var vllmResp VLLMResponse
    if err := json.NewDecoder(resp.Body).Decode(&vllmResp); err != nil {
        return nil, fmt.Errorf("decode response: %w", err)
    }

    return &vllmResp, nil
}
```

### 3. Modification: `service.go`

**Ajouter** méthode pour que Worker puisse vérifier si instance est active:

```go
// IsInstanceRunning vérifie si instance vLLM est active
func (s *Service) IsInstanceRunning(modelType string) bool {
    instanceName := "vllm-vision"
    if modelType == "think" {
        instanceName = "vllm-think"
    }

    _, exists := s.manager.GetInstance(instanceName)
    return exists
}

// GetServerURL retourne URL serveur selon type modèle
func (s *Service) GetServerURL(modelType string) string {
    if modelType == "think" {
        return "http://localhost:8002"
    }
    return "http://localhost:8001"
}
```

### 4. Modification: `main.go`

**Architecture hybride**: JSON-RPC pour client externe + Worker interne pour SQLite.

```go
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "io"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "horos47/core/data"
    "horos47/services/gpufeeder"
)

func main() {
    logger := setupLogger()
    logger.Info("GPU Feeder V3 starting")

    // 1. Connexion DB principale (workload stats)
    mainDB, err := data.OpenDB("/inference/horos47/data/main.db")
    if err != nil {
        logger.Error("Failed to open main DB", "error", err)
        os.Exit(1)
    }
    defer mainDB.Close()

    // 2. Connexion DB jobs (gpu_jobs table)
    jobsDB, err := sql.Open("sqlite", "/tmp/gpu_feeder_v3/jobs.db")
    if err != nil {
        logger.Error("Failed to open jobs DB", "error", err)
        os.Exit(1)
    }
    defer jobsDB.Close()

    // Init schema
    if err := gpufeeder.InitSchema(jobsDB); err != nil {
        logger.Error("Failed to init schema", "error", err)
        os.Exit(1)
    }

    // 3. Créer service (orchestration + containers)
    svc := gpufeeder.New(mainDB, logger)

    // 4. Créer worker (poll gpu_jobs)
    worker := gpufeeder.NewWorker(jobsDB, logger, svc)

    // Context avec signal handling
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sigChan
        logger.Info("Shutdown signal received")
        cancel()
    }()

    // 5. Démarrer service (lance containers)
    if err := svc.Start(ctx); err != nil {
        logger.Error("Failed to start service", "error", err)
        os.Exit(1)
    }

    // 6. Démarrer worker (poll jobs)
    go func() {
        if err := worker.Run(ctx); err != nil {
            logger.Error("Worker failed", "error", err)
        }
    }()

    logger.Info("GPU Feeder V3 ready")
    logger.Info("- Orchestration: monitoring GPU, allocating resources")
    logger.Info("- Worker: polling gpu_jobs table")
    logger.Info("- JSON-RPC: listening on stdin (MCP compatible)")

    // 7. JSON-RPC loop (pour clients externes si nécessaire)
    runJSONRPCServer(ctx, logger, svc)

    // Shutdown
    if err := svc.Close(); err != nil {
        logger.Error("Error during shutdown", "error", err)
    }
}
```

---

## 🗂️ Granularité et Chunking

### Stratégie Chunking 512 Tokens

**Où chunker?**
En **amont** lors de la création des jobs dans `handler_image_to_ocr.go`.

**Modification** `/inference/horos47/services/ingest/handler_image_to_ocr.go`:

```go
// Au lieu de créer 1 job OCR par page, créer N jobs fragmentés
func (s *Service) createOCRJobsWithChunking(ctx context.Context, imagePath string, docID string, pageNum int) error {
    // Si image > 4MB, fragmenter en tiles de 512x512 avec overlap 120px
    imageSize := getImageSize(imagePath)

    if imageSize > 4*1024*1024 {
        // Créer job parent
        parentID := data.NewUUID()

        // Découper image en tiles
        tiles := splitImageIntoTiles(imagePath, 512, 120)

        // Créer 1 job GPU par tile
        for idx, tile := range tiles {
            payload := map[string]interface{}{
                "image_path": tile.Path,
                "fragment_index": idx + 1,
                "total_fragments": len(tiles),
            }

            // Insert dans gpu_jobs avec parent_id
            insertGPUJob(ctx, parentID, payload, "vision")
        }
    } else {
        // Image petite: 1 seul job
        payload := map[string]interface{}{
            "image_path": imagePath,
            "fragment_index": 1,
            "total_fragments": 1,
        }
        insertGPUJob(ctx, nil, payload, "vision")
    }
}
```

**Note**: Pour l'instant, garder chunking simple (1 job = 1 image). Optimiser après validation architecture.

---

## 🧪 Configuration vLLM Optimale

### Paramètres Gemini (déjà dans v1)

```go
// manager.go - LaunchVisionVLLM
args := []string{
    "--model", "/models/qwen2-vl-7b-instruct",
    "--dtype", "bfloat16",
    "--gpu-memory-utilization", "0.75",
    "--max-model-len", "16384",
    "--max-num-seqs", "8",
    "--enable-chunked-prefill",            // Chunked prefill activé
    "--max-num-batched-tokens", "2048",    // NOUVEAU: Optimal throughput
    "--disable-log-requests",              // NOUVEAU: Réduire overhead logs
}
```

**Références**:
- Default `max_num_batched_tokens=512` pour meilleure latence (ITL)
- Valeur `2048` pour meilleur throughput (TTFT)
- Source: [vLLM Optimization Guide](https://docs.vllm.ai/en/stable/configuration/optimization/)

---

## 🔄 Flux de Données V3

```
┌─────────────────────────────────────────────────────────────┐
│  Handler Image → OCR (ingest service)                       │
│  - Crée jobs dans gpu_jobs table                           │
│  - 1 job = 1 image (ou N fragments si chunking)            │
│  - parent_id pour fan-in                                    │
└─────────────────────────────────────────────────────────────┘
                            ↓ INSERT gpu_jobs
┌─────────────────────────────────────────────────────────────┐
│  SQLite: gpu_jobs table                                     │
│  - status='pending', model_type='vision'                    │
│  - payload_sha256 pour idempotence                          │
└─────────────────────────────────────────────────────────────┘
                            ↓ POLL (5s)
┌─────────────────────────────────────────────────────────────┐
│  Worker.processBatch()                                      │
│  - SELECT jobs WHERE status='pending' LIMIT 32              │
│  - UPDATE status='processing', batch_id=uuid                │
│  - Vérifier serveur vLLM actif                             │
└─────────────────────────────────────────────────────────────┘
                            ↓ HTTP POST (parallèle)
┌─────────────────────────────────────────────────────────────┐
│  Serveur vLLM Vision (persistant, port 8001)               │
│  - Continuous batching automatique                          │
│  - Traite requêtes en parallèle (max_num_seqs=8)          │
│  - Retourne réponses OpenAI format                         │
└─────────────────────────────────────────────────────────────┘
                            ↓ Réponse HTTP
┌─────────────────────────────────────────────────────────────┐
│  Worker.completeJob()                                       │
│  - Parse réponse vLLM                                       │
│  - Sauve résultat dans result_path                          │
│  - UPDATE status='done', completed_at=now                   │
│  - Trigger fan-in si fragments                              │
└─────────────────────────────────────────────────────────────┘
                            ↓ Fan-in check
┌─────────────────────────────────────────────────────────────┐
│  fanin.go: checkFanIn()                                     │
│  - COUNT fragments WHERE parent_id=X AND status='done'      │
│  - Si tous done: UPDATE parent status='done'                │
│  - Trigger next workflow stage                              │
└─────────────────────────────────────────────────────────────┘
```

---

## 📋 Checklist Implémentation

### Phase 1: Nettoyage
- [ ] Supprimer binaires v1 et v2 (`/inference/horos47/bin/horos_gpu_feeder`, `gpu_feeder_v2`)
- [ ] Archiver anciens plans (`/inference/horos47/plans/gpu_feeder*.md`)
- [ ] Créer répertoire v3 (`/inference/horos47/cmd/gpu_feeder_v3/`)

### Phase 2: Fichiers Nouveaux
- [ ] Créer `worker.go` (worker SQLite)
- [ ] Créer `http_client.go` (client HTTP vLLM)
- [ ] Créer `config.go` (configuration v3)
- [ ] Créer `cmd/gpu_feeder_v3/main.go` (hybride JSON-RPC + worker)

### Phase 3: Fichiers V1 (copier sans modification)
- [ ] Copier `service.go` (v1)
- [ ] Copier `manager.go` (v1)
- [ ] Copier `allocator.go` (v1)
- [ ] Copier `monitor.go` (v1)
- [ ] Copier `health.go` (v1)
- [ ] Copier `types.go` (v1)

### Phase 4: Fichiers V2 (copier sans modification)
- [ ] Copier `db.go` (v2)
- [ ] Copier `fanin.go` (v2)
- [ ] Copier `schema.sql` (v2)

### Phase 5: Modifications V1
- [ ] Ajouter `IsInstanceRunning()` dans `service.go`
- [ ] Ajouter `GetServerURL()` dans `service.go`
- [ ] Ajouter `--max-num-batched-tokens=2048` dans `manager.go`

### Phase 6: Build et Test
- [ ] Compiler: `go build -o bin/gpu_feeder_v3 ./cmd/gpu_feeder_v3`
- [ ] Créer DB test: `/tmp/gpu_feeder_v3/jobs.db`
- [ ] Insérer jobs test (réutiliser script v2)
- [ ] Lancer binaire et vérifier logs
- [ ] Valider requêtes HTTP vers vLLM
- [ ] Valider fan-in sur jobs fragmentés

---

## 🎯 Validation Finale

### Critères de Succès

1. **Serveurs persistants**: Conteneurs vLLM lancés 1× au démarrage, pas de restart
2. **HTTP fonctionnel**: Requêtes POST vers `localhost:8001` et `8002` réussies
3. **Continuous batching**: vLLM batche automatiquement requêtes concurrentes
4. **Tracking SQLite**: Jobs transition `pending → processing → done`
5. **Fan-in**: Jobs fragmentés agrégés correctement (parent marqué done)
6. **Orchestration**: Stratégie d'allocation change selon workload
7. **Health check**: Instances unhealthy détectées et relancées
8. **GPU monitoring**: nvidia-smi toutes les 1s, alertes >80°C

### Commandes Test

```bash
# 1. Créer DB test
cd /tmp
mkdir -p gpu_feeder_v3/stage_vision/pending
sqlite3 gpu_feeder_v3/jobs.db < /inference/horos47/services/gpufeeder/schema.sql

# 2. Insérer job test
sqlite3 gpu_feeder_v3/jobs.db "
INSERT INTO gpu_jobs (id, payload_sha256, model_type, payload_path, created_at)
VALUES (randomblob(16), 'test123', 'vision', '/tmp/gpu_feeder_v3/stage_vision/pending/test.json', strftime('%s', 'now'));
"

# 3. Lancer GPU Feeder v3
/inference/horos47/bin/gpu_feeder_v3 &

# 4. Vérifier conteneurs actifs
docker ps | grep vllm

# 5. Vérifier jobs processés
sqlite3 gpu_feeder_v3/jobs.db "SELECT status, COUNT(*) FROM gpu_jobs GROUP BY status;"

# 6. Tester requête HTTP directe
curl -X POST http://localhost:8001/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"/models/qwen2-vl-7b-instruct","messages":[{"role":"user","content":"test"}],"max_tokens":10}'
```

---

## 📚 Références

- [vLLM Continuous Batching](https://voice.ai/hub/tts/vllm-continuous-batching/) - Architecture continuous batching
- [vLLM Optimization Parameters](https://medium.com/@kaige.yang0110/vllm-throughput-optimization-1-basic-of-vllm-parameters-c39ace00a519) - Tuning `max_num_batched_tokens`
- [vLLM Official Docs](https://docs.vllm.ai/en/stable/configuration/optimization/) - Configuration optimale

---

## 🚀 Prochaines Étapes (Post-V3)

1. **Chunking images**: Implémenter découpage tiles 512x512 avec overlap 120px
2. **Batching adaptatif**: Ajuster `max_num_seqs` selon queue depth
3. **Multi-GPU**: Support tensor parallelism pour gros modèles
4. **Métriques Prometheus**: Exposer latence, throughput, queue depth
5. **Ray Data**: Intégrer Ray Data pour batching optimisé inter-workers

---

**Durée estimée**: 3-4h
**Priorité**: CRITIQUE (bloque pipeline OCR)
