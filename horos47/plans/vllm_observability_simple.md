# vLLM Observability Simple - Plan de Développement

## Principe

**Pas de stack complexe** (Prometheus/Loki/Grafana ignorés).
**Deux destinations** : SQLite (metrics queryables) + fichiers .log (événements bruts).

---

## Architecture

```
vLLM Container
  ├─ stdout/stderr → Parser Go → /data/vllm_logs/YYYY-MM-DD.jsonl
  └─ :8000/metrics → Scraper Go → SQLite metrics.db
```

**Philosophie** : Tout capturer, stocker simple (fichiers + SQLite), analyser après.

---

## 1. Capture Logs vLLM → Fichiers JSONL

### Source
vLLM émet logs structurés sur stdout/stderr :
```
INFO: Started server on 0.0.0.0:8000
INFO: Received request batch_size=3 prompt_tokens=142
WARNING: KV cache usage at 87%
ERROR: Request timeout after 30s
```

### Implémentation Go

**Fichier** : `services/vllm_logger/capture.go`

**Principe** :
- Docker logs stream capté via `docker logs -f vllm-vision`
- Parser ligne par ligne
- Extraction timestamp, level, message, context
- Écriture fichier JSONL rotatif

**Format sortie** : `/data/vllm_logs/vision/2026-02-04.jsonl`
```jsonl
{"timestamp":"2026-02-04T14:32:11Z","level":"INFO","container":"vllm-vision","message":"Started server","port":8000}
{"timestamp":"2026-02-04T14:32:15Z","level":"WARNING","container":"vllm-vision","message":"KV cache usage high","usage_pct":87}
```

**Rotation** : Un fichier par jour, compression automatique après 7 jours.

---

## 2. Scrape Metrics vLLM → SQLite

### Source
vLLM expose endpoint Prometheus : `http://localhost:8001/metrics`

```
# HELP vllm_gpu_kv_cache_usage_perc GPU KV cache usage percentage
# TYPE vllm_gpu_kv_cache_usage_perc gauge
vllm_gpu_kv_cache_usage_perc 67.4

# HELP vllm_request_waiting_time_seconds Request waiting time
# TYPE vllm_request_waiting_time_seconds histogram
vllm_request_waiting_time_seconds_sum 12.5
vllm_request_waiting_time_seconds_count 42

# HELP vllm_tokens_per_second Tokens generation rate
# TYPE vllm_tokens_per_second gauge
vllm_tokens_per_second 124.3
```

### Implémentation Go

**Fichier** : `services/vllm_logger/scraper.go`

**Principe** :
- HTTP GET sur `:8001/metrics` toutes les 5s
- Parser format Prometheus (simple text)
- Insertion SQLite batch (1 transaction pour tous les metrics)

### Table SQLite

**Fichier** : `/data/vllm_metrics.db`

```sql
CREATE TABLE metrics (
    timestamp INTEGER NOT NULL,           -- Unix epoch milliseconds
    container TEXT NOT NULL,              -- 'vllm-vision' ou 'vllm-think'
    metric_name TEXT NOT NULL,            -- 'gpu_kv_cache_usage_perc', etc.
    value REAL NOT NULL,                  -- Valeur numérique
    labels TEXT,                          -- JSON des labels si présents
    PRIMARY KEY (timestamp, container, metric_name)
);

CREATE INDEX idx_metrics_name_time ON metrics(metric_name, timestamp DESC);
CREATE INDEX idx_metrics_container ON metrics(container, timestamp DESC);
```

**Requête exemple** :
```sql
-- KV cache usage dernières 5 minutes
SELECT timestamp, value
FROM metrics
WHERE metric_name = 'gpu_kv_cache_usage_perc'
  AND container = 'vllm-vision'
  AND timestamp > unixepoch() - 300
ORDER BY timestamp DESC;
```

---

## 3. Binaire vLLM Logger

### Architecture

**Fichier** : `cmd/horos_vllm_logger/main.go`

**Deux goroutines** :
1. Log capture (stdout/stderr → JSONL)
2. Metrics scraper (Prometheus → SQLite)

**Lifecycle** :
- Démarré par GPU Feeder quand conteneur vLLM démarre
- Arrêté proprement (flush buffers) quand conteneur stop

### Configuration

**Fichier** : `configs/vllm_logger.yaml`

```yaml
log_capture:
  output_dir: /data/vllm_logs
  rotation: daily
  compression: gzip
  retention_days: 30

metrics_scraper:
  interval_seconds: 5
  db_path: /data/vllm_metrics.db
  batch_size: 100  # Nombre metrics avant commit

containers:
  - name: vllm-vision
    logs_stream: docker logs -f vllm-vision
    metrics_endpoint: http://localhost:8001/metrics
  - name: vllm-think
    logs_stream: docker logs -f vllm-think
    metrics_endpoint: http://localhost:8002/metrics
```

---

## 4. Intégration GPU Feeder

### Spawn Logger

Quand GPU Feeder démarre un conteneur vLLM :

```go
// services/gpufeeder/simple_feeder.go

func (f *SimpleFeeder) startVisionContainer() {
    // Start vLLM container
    containerID := f.runDockerCommand(vllmArgs, "vllm-vision")

    // Start logger
    f.startLogger("vllm-vision", 8001)
}

func (f *SimpleFeeder) startLogger(containerName string, metricsPort int) {
    cmd := exec.Command("horos_vllm_logger",
        "--container", containerName,
        "--metrics-port", metricsPort,
        "--log-dir", "/data/vllm_logs/"+containerName,
        "--db", "/data/vllm_metrics.db",
    )
    cmd.Start()

    f.loggerProcesses[containerName] = cmd.Process
}

func (f *SimpleFeeder) stopLogger(containerName string) {
    if proc := f.loggerProcesses[containerName]; proc != nil {
        proc.Signal(syscall.SIGTERM)  // Graceful shutdown
        proc.Wait()
    }
}
```

---

## 5. CLI Inspection

### Logs

```bash
# Voir logs Vision aujourd'hui
cat /data/vllm_logs/vision/2026-02-04.jsonl | jq .

# Filtrer warnings
cat /data/vllm_logs/vision/*.jsonl | jq 'select(.level=="WARNING")'

# Chercher timeouts
grep "timeout" /data/vllm_logs/vision/*.jsonl
```

### Metrics

```bash
# Requête SQLite directe
sqlite3 /data/vllm_metrics.db "
  SELECT datetime(timestamp, 'unixepoch'), value
  FROM metrics
  WHERE metric_name = 'tokens_per_second'
  ORDER BY timestamp DESC
  LIMIT 10
"

# Export CSV pour analyse
sqlite3 -csv /data/vllm_metrics.db "
  SELECT * FROM metrics WHERE metric_name = 'gpu_kv_cache_usage_perc'
" > cache_usage.csv
```

### CLI dédié

**Fichier** : `cmd/horos_metrics/main.go`

```bash
# Stats rapides
horos-metrics stats --container vllm-vision --last 1h

Output:
GPU KV Cache Usage: avg=67.2% max=89.1% min=42.3%
Request Waiting Time: avg=0.12s p95=0.34s p99=0.87s
Tokens/s: avg=124.3 max=156.7 min=98.2

# Export pour graphing externe
horos-metrics export --metric gpu_kv_cache_usage_perc --format csv > data.csv
```

---

## 6. Alertes Simples (Optionnel)

### Fichier Alertes

**Fichier** : `configs/vllm_alerts.yaml`

```yaml
alerts:
  - name: KV Cache Saturation
    condition: gpu_kv_cache_usage_perc > 90
    duration: 30s  # Sustained 30s
    action: log_error

  - name: High Waiting Time
    condition: request_waiting_time_seconds > 2.0
    duration: 60s
    action: log_warning

  - name: Low Throughput
    condition: tokens_per_second < 50
    duration: 120s
    action: log_warning
```

### Implémentation

Simple évaluateur dans scraper :
- Après chaque scrape, évaluer conditions
- Si seuil dépassé pendant durée → écrire dans `/data/vllm_logs/alerts.log`
- Pas d'envoi email, pas de webhook, juste log

---

## Livrables

1. **Binaire** : `horos_vllm_logger` (capture logs + scrape metrics)
2. **CLI** : `horos-metrics` (inspection rapide SQLite)
3. **Tables SQLite** : `metrics` dans `/data/vllm_metrics.db`
4. **Logs JSONL** : `/data/vllm_logs/{container}/YYYY-MM-DD.jsonl`
5. **Intégration** : GPU Feeder spawn/stop logger automatiquement
6. **Documentation** : Requêtes SQL utiles, format JSONL

---

## Avantages Architecture Simple

### vs Prometheus/Grafana
- ✅ Zéro dépendance externe (pas de stack à maintenir)
- ✅ SQLite queryable directement (pas de PromQL à apprendre)
- ✅ Export CSV trivial (analyse dans n'importe quel outil)
- ✅ Pas de retention policy complexe (juste rotation fichiers)

### vs Loki
- ✅ grep/jq suffisent (pas de LogQL)
- ✅ Fichiers JSONL inspectables à la main
- ✅ Compression gzip standard (pas de format propriétaire)

### Observabilité
- ✅ Tout capturé (logs + metrics)
- ✅ Stockage durable (SQLite + fichiers)
- ✅ Analysable post-mortem (rejouable)
- ✅ Intégration simple (binaire Go unique)

---

## Tests

### Test Capture Logs

```bash
# Démarrer vLLM test
docker run -d --name vllm-test vllm/vllm-openai:latest ...

# Démarrer logger
horos_vllm_logger --container vllm-test --log-dir /tmp/test_logs

# Vérifier fichiers créés
ls -lh /tmp/test_logs/
cat /tmp/test_logs/2026-02-04.jsonl | jq .
```

### Test Scraper Metrics

```bash
# Vérifier endpoint accessible
curl http://localhost:8001/metrics

# Démarrer scraper
horos_vllm_logger --metrics-port 8001 --db /tmp/test_metrics.db

# Attendre 30s (6 scrapes)
sleep 30

# Vérifier données
sqlite3 /tmp/test_metrics.db "SELECT COUNT(*) FROM metrics"
# Attendu: ~30-40 metrics (5-7 metrics par scrape, 6 scrapes)
```

---

## Priorité

**HAUTE** - TIER 1 (Phase 1)

Remplace stack Prometheus/Loki/Grafana inutilisée par solution simple fichiers + SQLite.
