package workflow_trace

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"horos47/core/data"

	_ "modernc.org/sqlite"
)

// WorkflowTracer gère la traçabilité granulaire workflow dans workflow_execution_trace
type WorkflowTracer struct {
	db            *sql.DB
	workflowName  string
	workflowRunID string
	machineName   string
	workerPID     int
}

// NewWorkflowTracer crée tracer pour workflow donné
// db: connexion SQLite partagée (réutilise connexion existante)
// workflowName: identifiant workflow (ex: pdf_vision_ingest, horos_rag_indexer)
// workflowRunID: identifiant unique exécution (ex: job_id)
// machineName: nom machine exécution (ex: workspace-local, inference-local)
func NewWorkflowTracer(db *sql.DB, workflowName, workflowRunID, machineName string) (*WorkflowTracer, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	return &WorkflowTracer{
		db:            db,
		workflowName:  workflowName,
		workflowRunID: workflowRunID,
		machineName:   machineName,
		workerPID:     os.Getpid(),
	}, nil
}

// Close libère ressources (DB partagée, pas fermée ici)
func (wt *WorkflowTracer) Close() error {
	// DB est partagée, ne pas la fermer
	wt.db = nil
	return nil
}

// TraceStepStart enregistre début étape workflow
// stepName: nom étape (ex: chunk_documents, generate_embeddings)
// stepIndex: position séquentielle étape (0-based)
// inputPath: chemin absolu fichier input (ou "" si pas de fichier)
// metadata: map métadonnées JSON extensibles
// Retourne traceID utilisé pour TraceStepComplete/Failed
func (wt *WorkflowTracer) TraceStepStart(stepName string, stepIndex int, inputPath string, metadata map[string]interface{}) (string, error) {
	traceID := generateTraceID()

	// Calculer hash input si fichier existe
	var inputHash sql.NullString
	if inputPath != "" {
		hash, err := HashFile(inputPath)
		if err == nil {
			inputHash = sql.NullString{String: hash, Valid: true}
		} else {
			// Log warning mais continue sans bloquer
			fmt.Fprintf(os.Stderr, "[WARN] Failed to hash input file %s: %v\n", inputPath, err)
		}
	}

	// Marshaler metadata JSON
	metadataJSON := "{}"
	if len(metadata) > 0 {
		jsonBytes, err := json.Marshal(metadata)
		if err != nil {
			return "", fmt.Errorf("marshal metadata: %w", err)
		}
		metadataJSON = string(jsonBytes)
	}

	startedAt := currentTimeMillis()

	_, err := data.ExecWithRetry(wt.db, `
		INSERT INTO workflow_execution_trace (
			trace_id, workflow_name, workflow_run_id, step_name, step_index,
			step_status, input_file_path, input_sha256, machine_name, worker_pid,
			started_at, step_metadata
		) VALUES (?, ?, ?, ?, ?, 'started', ?, ?, ?, ?, ?, ?)
	`, traceID, wt.workflowName, wt.workflowRunID, stepName, stepIndex,
		inputPath, inputHash, wt.machineName, wt.workerPID, startedAt, metadataJSON)

	if err != nil {
		return "", fmt.Errorf("insert trace step start: %w", err)
	}

	return traceID, nil
}

// TraceStepComplete marque étape complétée avec output path et artifacts
// traceID: identifiant retourné par TraceStepStart
// outputPath: chemin absolu fichier output principal (ou "" si pas de fichier)
// artifactPaths: array chemins absolus artifacts générés supplémentaires
// metadata: map métadonnées additionnelles à merger avec existantes
func (wt *WorkflowTracer) TraceStepComplete(traceID string, outputPath string, artifactPaths []string, metadata map[string]interface{}) error {
	completedAt := currentTimeMillis()

	// Hash output
	var outputHash sql.NullString
	if outputPath != "" {
		hash, err := HashFile(outputPath)
		if err == nil {
			outputHash = sql.NullString{String: hash, Valid: true}
		} else {
			fmt.Fprintf(os.Stderr, "[WARN] Failed to hash output file %s: %v\n", outputPath, err)
		}
	}

	// JSON array artifact paths
	artifactsJSON := "[]"
	if len(artifactPaths) > 0 {
		jsonBytes, err := json.Marshal(artifactPaths)
		if err != nil {
			return fmt.Errorf("marshal artifact paths: %w", err)
		}
		artifactsJSON = string(jsonBytes)
	}

	// Metadata JSON
	metadataJSON := "{}"
	if len(metadata) > 0 {
		jsonBytes, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
		metadataJSON = string(jsonBytes)
	}

	_, err := data.ExecWithRetry(wt.db, `
		UPDATE workflow_execution_trace
		SET step_status = 'completed',
		    output_file_path = ?,
		    output_sha256 = ?,
		    artifact_paths = ?,
		    completed_at = ?,
		    duration_ms = ? - started_at,
		    step_metadata = json_patch(step_metadata, ?)
		WHERE trace_id = ?
	`, outputPath, outputHash, artifactsJSON, completedAt, completedAt, metadataJSON, traceID)

	if err != nil {
		return fmt.Errorf("update trace step complete: %w", err)
	}

	return nil
}

// TraceStepFailed marque étape échec avec erreur
// traceID: identifiant retourné par TraceStepStart
// errorCode: code erreur (ex: GPU_OVERHEAT, FILE_NOT_FOUND, VALIDATION_ERROR)
// errorMsg: message erreur détaillé
func (wt *WorkflowTracer) TraceStepFailed(traceID string, errorCode, errorMsg string) error {
	completedAt := currentTimeMillis()

	metadata := map[string]interface{}{
		"error_code":    errorCode,
		"error_message": errorMsg,
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal error metadata: %w", err)
	}

	_, err = data.ExecWithRetry(wt.db, `
		UPDATE workflow_execution_trace
		SET step_status = 'failed',
		    completed_at = ?,
		    duration_ms = ? - started_at,
		    step_metadata = json_patch(step_metadata, ?)
		WHERE trace_id = ?
	`, completedAt, completedAt, string(metadataJSON), traceID)

	if err != nil {
		return fmt.Errorf("update trace step failed: %w", err)
	}

	return nil
}

// CheckDuplicate vérifie si input hash déjà traité (idempotence)
// inputHash: hash SHA256 fichier input
// Retourne: (isDuplicate, existingTraceID, error)
// Si isDuplicate=true, job peut être skipped car déjà traité avec succès
func (wt *WorkflowTracer) CheckDuplicate(inputHash string) (bool, string, error) {
	var existingTraceID string
	err := wt.db.QueryRow(`
		SELECT trace_id
		FROM workflow_execution_trace
		WHERE workflow_name = ? AND input_sha256 = ? AND step_status = 'completed'
		ORDER BY started_at DESC
		LIMIT 1
	`, wt.workflowName, inputHash).Scan(&existingTraceID)

	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("check duplicate: %w", err)
	}

	return true, existingTraceID, nil
}

// GetWorkflowRun retourne toutes les étapes d'un workflow run donné
func (wt *WorkflowTracer) GetWorkflowRun(workflowRunID string) ([]map[string]interface{}, error) {
	rows, err := wt.db.Query(`
		SELECT trace_id, step_name, step_index, step_status, input_file_path,
		       output_file_path, input_sha256, output_sha256, started_at,
		       completed_at, duration_ms, step_metadata
		FROM workflow_execution_trace
		WHERE workflow_run_id = ?
		ORDER BY step_index ASC
	`, workflowRunID)
	if err != nil {
		return nil, fmt.Errorf("query workflow run: %w", err)
	}
	defer rows.Close()

	var steps []map[string]interface{}
	for rows.Next() {
		var traceID, stepName, stepStatus string
		var inputPath, outputPath, inputHash, outputHash, metadata sql.NullString
		var stepIndex, startedAt sql.NullInt64
		var completedAt, durationMs sql.NullInt64

		err := rows.Scan(&traceID, &stepName, &stepIndex, &stepStatus, &inputPath,
			&outputPath, &inputHash, &outputHash, &startedAt, &completedAt,
			&durationMs, &metadata)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		step := map[string]interface{}{
			"trace_id":    traceID,
			"step_name":   stepName,
			"step_index":  int(stepIndex.Int64),
			"step_status": stepStatus,
		}

		if inputPath.Valid {
			step["input_file_path"] = inputPath.String
		}
		if outputPath.Valid {
			step["output_file_path"] = outputPath.String
		}
		if inputHash.Valid {
			step["input_sha256"] = inputHash.String
		}
		if outputHash.Valid {
			step["output_sha256"] = outputHash.String
		}
		if startedAt.Valid {
			step["started_at"] = startedAt.Int64
		}
		if completedAt.Valid {
			step["completed_at"] = completedAt.Int64
		}
		if durationMs.Valid {
			step["duration_ms"] = durationMs.Int64
		}
		if metadata.Valid {
			step["step_metadata"] = metadata.String
		}

		steps = append(steps, step)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return steps, nil
}

// HashFile calcule hash SHA256 fichier en streaming
// Utilise io.Copy pour éviter charger fichier complet en RAM
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// generateTraceID génère identifiant unique trace format trace_{uuidv7}
func generateTraceID() string {
	return "trace_" + data.NewUUID().String()
}

// currentTimeMillis retourne timestamp Unix milliseconds
func currentTimeMillis() int64 {
	return time.Now().UnixMilli()
}
