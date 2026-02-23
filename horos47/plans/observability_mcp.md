# Observabilité MCP - Plan de Développement

## Objectif
Exposer toutes les actions métier en dual HTTP/MCP pour supervision LLM complète.

## Principe Dual Transport
Chaque handler HTTP est automatiquement exposé en MCP.
Pas de code dupliqué, même logique métier.

## Actions d'Observabilité Requises

### Jobs & Workflows
```go
GET  /jobs?status=pending&type=ocr    → MCP tool "list_jobs"
GET  /jobs/{uuid}                     → MCP tool "inspect_job"
GET  /workflows                       → MCP tool "list_workflows"
GET  /workflows/{id}/trace            → MCP tool "trace_workflow"
```

### Logs & Métriques
```go
GET  /logs?level=error&since=ts       → MCP tool "get_logs"
GET  /metrics/jobs                    → MCP tool "get_job_metrics"
GET  /metrics/performance             → MCP tool "get_performance_stats"
```

### GPU Feeder
```go
GET  /gpu/status                      → MCP tool "gpu_status"
GET  /gpu/history                     → MCP tool "gpu_history"
POST /gpu/force-mode                  → MCP tool "gpu_force_mode"
```

### Documents & Chunks
```go
GET  /documents/{uuid}                → MCP tool "get_document"
GET  /documents/{uuid}/chunks         → MCP tool "get_document_chunks"
GET  /chunks/{uuid}                   → MCP tool "get_chunk"
```

## Implémentation Chassis
### `core/chassis/dual_transport.go`
```go
type DualHandler struct {
    HTTPHandler func(w http.ResponseWriter, r *http.Request)
    MCPSchema   *mcp.Tool
}

func (c *Chassis) RegisterDual(path string, handler DualHandler) {
    // Register HTTP
    c.router.Get(path, handler.HTTPHandler)

    // Register MCP
    c.mcpServer.AddTool(handler.MCPSchema, mcpAdapter(handler.HTTPHandler))
}
```

## Logs Structurés JSON
Tous les logs émis en JSON avec champs standards :
```json
{
  "timestamp": "2026-02-04T14:32:11Z",
  "level": "INFO",
  "service": "rag",
  "message": "Job completed",
  "job_id": "abc123",
  "duration_ms": 234
}
```

## Tables Audit
### `audit_log`
Trace actions importantes (création workflow, changement mode GPU, etc.)
```sql
CREATE TABLE audit_log (
    id BLOB PRIMARY KEY,
    timestamp INTEGER,
    action TEXT,
    actor TEXT,  -- "llm_supervisor", "user", "system"
    details TEXT -- JSON
);
```

## Implémentation Services
Chaque service expose actions complètes :
- Pas seulement POST (créer), mais aussi GET (lire), PUT (modifier), DELETE
- Endpoints avec filtres appropriés (status, date range, etc.)
- Pagination pour grandes listes

## Livrables
- Chassis dual transport généralisé
- Audit de tous services pour complétude actions
- Table audit_log + migrations
- Tests MCP pour chaque tool exposé
