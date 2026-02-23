# CLI Admin - Plan de Développement

## Objectif
Outil CLI pour administrer HOROS 47 : inspecter jobs, retry échecs, vérifier intégrité, réparer.

## Binaire
```
cmd/horos_admin/main.go
bin/horos_admin
```

## Commandes

### `status`
Vue d'ensemble du système.
```bash
horos_admin status

Stage       | Status     | Count | Oldest Job       | Avg Duration
------------|------------|-------|------------------|-------------
ocr         | pending    |    12 | 14:32:11         | 2.3s
ocr         | processing |     3 | 14:35:22         | -
embed       | pending    |   145 | 14:30:05         | 0.8s
embed       | done       |  8923 | -                | 0.7s
embed       | failed     |     2 | 14:20:11         | -
```

### `retry-failed`
Réessayer jobs échoués.
```bash
horos_admin retry-failed <stage>
horos_admin retry-failed ocr

✓ Retried: abc123 (attempt 1 → 2)
✓ Retried: def456 (attempt 2 → 3)
```

### `inspect`
Inspecter job détaillé.
```bash
horos_admin inspect <job_sha256>

Job: abc123def456...
Type: image_to_ocr
Status: failed
Attempts: 2
Created: 2026-02-04 14:32:11
Started: 2026-02-04 14:32:15
Failed: 2026-02-04 14:32:18
Error: vLLM timeout after 30s
Payload:
  image_path: /data/page_042.png
  _workflow:
    chain: ["ocr_to_db", "db_to_chunk"]
```

### `check-integrity`
Vérifier cohérence filesystem/SQLite.
```bash
horos_admin check-integrity

Checking SHA256 hashes...
✓ 9823 jobs verified
✗ 2 corrupted files:
  - /data/stage_1_ocr/pending/abc123.json (SHA256 mismatch)
  - /data/stage_3_embed/done/def456.json (file missing)
```

### `repair`
Réparer incohérences détectées.
```bash
horos_admin repair

Repairing corrupted jobs...
✓ abc123: payload re-hashed, updated in DB
✓ def456: orphan DB entry deleted
```

### `purge`
Nettoyer vieux jobs terminés.
```bash
horos_admin purge --older-than 30d --status done

Purging jobs older than 30 days with status 'done'...
✓ Deleted 45032 jobs
✓ Freed 2.3 GB
```

### `trace`
Tracer workflow complet.
```bash
horos_admin trace <initial_job_sha256>

Workflow Trace:
pdf_to_images (abc123)
  ├─ image_to_ocr (def001) → done (2.1s)
  ├─ image_to_ocr (def002) → done (2.3s)
  ├─ image_to_ocr (def003) → failed
  └─ ... (397 more)

ocr_to_db (ghi001) → done (0.5s)
db_to_chunk (jkl001) → processing
```

### `workflows`
Gérer définitions workflows.
```bash
horos_admin workflows list
horos_admin workflows show <workflow_id>
horos_admin workflows validate <workflow_id>
horos_admin workflows test <workflow_id> --payload test.json
```

## Fichiers
```
cmd/horos_admin/
├── main.go
├── status.go
├── retry.go
├── inspect.go
├── integrity.go
├── repair.go
├── purge.go
└── trace.go
```

## Implémentation
Réutilise `core/data/` pour accès SQLite.
Pas de duplication logique, juste couche CLI sur actions existantes.

## Output Format
- Terminal : Tables ASCII formattées
- JSON : `--format json` pour scripting
- Quiet : `--quiet` pour CI/CD

## Tests
```bash
scripts/test_admin_cli.sh
```
Créer scénario avec jobs variés, vérifier chaque commande.

## Livrables
- Binaire `horos_admin` avec 8 commandes
- Tests automatisés
- Man page ou `--help` détaillé
