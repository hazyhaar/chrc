# HOROS 47 SINGULARITY - Build & Architecture

## Build

Pure Go, no CGO:

```bash
cd /inference/horos47
CGO_ENABLED=0 go build -o bin/horos47 ./cmd/horos47/
```

Workers:
```bash
CGO_ENABLED=0 go build -o bin/embedding-indexer ./workers/embedding-indexer/
```

## Architecture (post-refactor Feb 2025)

### Services (long-lived, with HTTP/QUIC endpoints)

| Service | Path | Role |
|---------|------|------|
| **gateway** | `services/gateway/` | SAS IN/OUT: envelope lifecycle, routing, dispatch, HORUM polling |
| **gpufeeder** | `services/gpufeeder/` | Sole GPU access point (vLLM Vision, OCR, embeddings) |

### Handlers (stateless job functions, `handlers/`)

All job handler functions live in `handlers/` as methods on a shared `Handlers` struct.
Registered on the job worker via `handlers.RegisterAll(worker)`.

| File | Handlers | Purpose |
|------|----------|---------|
| `pdf.go` | `pdf_to_images` | PDF to page images via pdftoppm |
| `ocr.go` | `image_to_ocr` | Image OCR via GPU Feeder |
| `complete.go` | `ocr_to_database` | Insert OCR+blobs, create chunks |
| `format.go` | `detect_format`, `complete_ingest` | Format detection, PDF pipeline submission |
| `clarify.go` | `clarify_intent` | Intent clarification with uncertainty detection |
| `fetch.go` | `fetch_and_ingest` | Download attachments with SHA256 verification |
| `search.go` | `rag_retrieve` | Hybrid search (FTS5 BM25 + vector cosine) |
| `generate.go` | `generate_answer` | LLM generation (stub, will use gpufeeder) |
| `embed.go` | `embed_chunks` | Embeddings (stub, will use gpufeeder) |
| `stubs.go` | 7 agent stubs | Placeholder handlers for future agent workflows |
| `deps.go` | — | Handlers struct, EnvelopeManager interface, utilities |
| `register.go` | — | RegisterAll: wires all handlers to job worker |

### Storage (`storage/`)

Pure data layer — schema init, types, CRUD operations, chunking:

| File | Contents |
|------|----------|
| `documents.go` | Document, Chunk, PDFPage types + SaveDocument, SavePDFPages, SplitImageBlobs |
| `embeddings.go` | Embeddings schema, vector serialization, cosine similarity |
| `chunker.go` | ChunkText, ChunkBySentences (pure text splitting functions) |

### Core (`core/`)

| Package | Role |
|---------|------|
| `core/jobs/` | Job queue + worker (DO NOT MODIFY) |
| `core/data/` | UUID v7, DB helpers, ExecWithRetry, RunTransaction |
| `core/trace/` | Workflow execution tracing |
| `core/chassis/` | Unified QUIC/HTTP3 server |

### Dependency Graph

```
main.go
  ├── handlers/     → storage/, core/*, services/gateway (EnvelopeManager interface)
  ├── storage/      → core/data
  ├── services/gateway/ → core/*, storage/ (for horum_poller)
  └── services/gpufeeder/ → (standalone)
```

No circular dependencies. `handlers/` depends on `gateway` only for `UncertaintyDetector` type.
`gateway` depends on `storage` only for `ChunkBySentences`/types in `horum_poller.go`.

## Running

```bash
./bin/horos47
# Listens on :8443 (QUIC/HTTP3)
# Database: /inference/horos47/data/main.db
```

Graceful shutdown via SIGTERM/SIGINT.

## Key Design Decisions

- **gateway is the only service with HTTP endpoints** — all other logic is pure job handlers
- **gpufeeder is the sole GPU access point** — handlers never talk to GPU directly
- **EnvelopeManager interface** decouples handlers/ from gateway/ (no circular imports)
- **storage/ is shared** by both handlers/ and gateway/ (horum_poller uses it for text ingestion)
- **Workflow chains** in job payloads drive multi-step processing (each handler shifts and submits next)
