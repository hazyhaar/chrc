package gpufeeder

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Worker poll gpu_jobs SQLite et envoie vers serveurs vLLM HTTP persistants
type Worker struct {
	db         *sql.DB
	cfg        Config
	logger     *slog.Logger
	service    *Service // Référence au service pour vérifier instances
	httpClient *VLLMHTTPClient
}

// NewWorker crée nouveau worker
func NewWorker(db *sql.DB, cfg Config, logger *slog.Logger, service *Service) *Worker {
	return &Worker{
		db:         db,
		cfg:        cfg,
		logger:     logger,
		service:    service,
		httpClient: NewVLLMHTTPClient(logger),
	}
}

// jobResult captures per-job outcome for crash detection
type jobResult struct {
	job Job
	err error
}

// Run démarre boucle de polling
func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("GPU Worker starting",
		"poll_interval", w.cfg.PollInterval,
		"batch_size", w.cfg.BatchSize,
		"max_concurrency", w.cfg.MaxConcurrency)

	// Cleanup orphan processing jobs from previous crash/kill
	w.cleanupOrphanJobs(ctx)

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	// Auto-retry failed (not poison) gpu_jobs every 30s
	retryTicker := time.NewTicker(30 * time.Second)
	defer retryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("GPU Worker shutting down")
			return ctx.Err()
		case <-retryTicker.C:
			w.retryFailedGPUJobs(ctx)
		case <-ticker.C:
			if err := w.processBatch(ctx); err != nil {
				w.logger.Error("Batch processing failed", "error", err)
			}
		}
	}
}

// cleanupOrphanJobs runs on startup to fix state from previous crash/kill:
// 1. Reset orphan processing jobs back to pending
// 2. Early-poison pending/failed jobs near attempt limit with crash errors
//    (prevents allocator thrashing: 1 doomed think job shouldn't block 700 vision jobs)
func (w *Worker) cleanupOrphanJobs(ctx context.Context) {
	// Step 1: Reset orphan processing jobs
	result, err := w.db.ExecContext(ctx, `
		UPDATE gpu_jobs SET status = 'pending', batch_id = NULL, started_at = NULL
		WHERE status = 'processing'
	`)
	if err != nil {
		w.logger.Error("Failed to cleanup orphan jobs", "error", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		w.logger.Warn("Reset orphan processing jobs from previous run", "count", n)
	}

	// Step 2: Poison near-limit crash jobs (pending or failed with crash errors and attempts >= max-1)
	poisonResult, err := w.db.ExecContext(ctx, `
		UPDATE gpu_jobs SET status = 'poison'
		WHERE status IN ('pending', 'failed') AND attempts >= ? - 1
		AND (last_error LIKE '%EOF%' OR last_error LIKE '%connection reset%'
		     OR last_error LIKE '%connection refused%' OR last_error LIKE '%broken pipe%'
		     OR last_error LIKE '%i/o timeout%')
	`, w.cfg.MaxAttempts)
	if err != nil {
		w.logger.Error("Failed to early-poison crash jobs", "error", err)
	} else if n, _ := poisonResult.RowsAffected(); n > 0 {
		w.logger.Warn("Early-poisoned near-limit crash jobs on startup", "count", n)
	}
}

// retryFailedGPUJobs remet en pending les gpu_jobs failed dont attempts < max_attempts.
// Near-poison crash jobs (attempts >= max-1 with crash errors) are poisoned immediately
// to prevent allocator mode thrashing (1 failing think job shouldn't block vision for 180s).
func (w *Worker) retryFailedGPUJobs(ctx context.Context) {
	// Step 1: Poison near-limit crash jobs early (prevents allocator thrashing)
	poisonResult, err := w.db.ExecContext(ctx, `
		UPDATE gpu_jobs SET status = 'poison'
		WHERE status = 'failed' AND attempts >= ? - 1
		AND (last_error LIKE '%EOF%' OR last_error LIKE '%connection reset%'
		     OR last_error LIKE '%connection refused%' OR last_error LIKE '%broken pipe%'
		     OR last_error LIKE '%i/o timeout%')
	`, w.cfg.MaxAttempts)
	if err != nil {
		w.logger.Error("Failed to poison near-limit crash jobs", "error", err)
	} else if n, _ := poisonResult.RowsAffected(); n > 0 {
		w.logger.Warn("Early-poisoned near-limit crash jobs", "count", n)
	}

	// Step 2: Retry remaining failed jobs normally
	result, err := w.db.ExecContext(ctx, `
		UPDATE gpu_jobs SET status = 'pending', batch_id = NULL, started_at = NULL
		WHERE status = 'failed' AND attempts < ?
	`, w.cfg.MaxAttempts)
	if err != nil {
		w.logger.Error("Failed to retry failed gpu_jobs", "error", err)
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		w.logger.Info("Retried failed gpu_jobs", "count", n)
	}
}

// processBatch claim batch et traite jobs, with collective crash detection
func (w *Worker) processBatch(ctx context.Context) error {
	// 1. Sélectionner modèle (Think prioritaire, mais skip if server down)
	modelType := w.selectRunnableModel(ctx)
	if modelType == "" {
		return nil
	}

	serverURL := w.getServerURL(modelType)

	// 3. Claim batch atomique
	batchID := uuid.Must(uuid.NewV7())
	jobs, err := w.claimBatch(ctx, modelType, batchID)
	if err != nil {
		return fmt.Errorf("claim batch: %w", err)
	}

	if len(jobs) == 0 {
		return nil
	}

	w.logger.Info("Processing batch",
		"batch_id", batchID,
		"model", modelType,
		"jobs", len(jobs))

	// 4. Traiter jobs en parallèle, collect results
	results := w.runBatch(ctx, jobs, serverURL)

	// 5. Detect collective crash
	crashCount := 0
	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.err != nil {
			if isCrashError(r.err) {
				crashCount++
			}
			failCount++
		} else {
			successCount++
		}
	}

	w.logger.Info("Batch completed",
		"batch_id", batchID,
		"success", successCount,
		"failed", failCount,
		"crash_errors", crashCount)

	// Heuristic: >50% crash errors = collective crash (vLLM died)
	if len(jobs) > 1 && crashCount > len(jobs)/2 {
		w.logger.Warn("Collective crash detected, entering bisection mode",
			"batch_size", len(jobs),
			"crash_errors", crashCount)

		// Requeue all jobs from this batch (their failJob already incremented attempts,
		// but bisection will handle them fresh)
		w.requeueJobs(ctx, jobs)

		// Wait for vLLM to recover
		time.Sleep(5 * time.Second)
		if !w.service.IsInstanceRunning(modelType) {
			w.logger.Error("vLLM not recovered after crash, skipping bisection")
			return nil
		}

		w.bisectAndIsolate(ctx, jobs, modelType, serverURL, 0)
	}

	// Cooldown between batches to let GPU cool down
	if successCount > 0 && w.cfg.BatchCooldown > 0 {
		w.logger.Debug("Batch cooldown", "duration", w.cfg.BatchCooldown)
		time.Sleep(w.cfg.BatchCooldown)
	}

	return nil
}

// runBatch executes jobs in parallel and returns per-job results
func (w *Worker) runBatch(ctx context.Context, jobs []Job, serverURL string) []jobResult {
	sem := make(chan struct{}, w.cfg.MaxConcurrency)
	var wg sync.WaitGroup
	results := make([]jobResult, len(jobs))

	for i, job := range jobs {
		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, j Job) {
			defer wg.Done()
			defer func() { <-sem }()

			err := w.processJob(ctx, j, serverURL)
			results[idx] = jobResult{job: j, err: err}
		}(i, job)
	}

	wg.Wait()
	return results
}

// bisectAndIsolate recursively bisects a batch to isolate the poison pill
func (w *Worker) bisectAndIsolate(ctx context.Context, jobs []Job, modelType, serverURL string, depth int) {
	maxDepth := int(math.Log2(float64(w.cfg.BatchSize))) + 3
	if depth > maxDepth {
		w.logger.Error("Bisection max depth reached, giving up",
			"depth", depth,
			"remaining_jobs", len(jobs))
		return
	}

	// Terminal case: single job = the poison pill
	if len(jobs) == 1 {
		w.logger.Error("Poison pill isolated via bisection",
			"job_id", jobs[0].ID,
			"payload_path", jobs[0].PayloadPath)
		w.markPoison(ctx, jobs[0], "isolated as poison pill via bisection")
		return
	}

	// Check vLLM is still alive
	if !w.service.IsInstanceRunning(modelType) {
		w.logger.Error("vLLM down during bisection, requeueing all")
		w.requeueJobs(ctx, jobs)
		return
	}

	mid := len(jobs) / 2
	firstHalf := jobs[:mid]
	secondHalf := jobs[mid:]

	w.logger.Info("Bisection: testing first half",
		"depth", depth,
		"first_half", len(firstHalf),
		"second_half", len(secondHalf))

	// Test first half
	firstResults := w.runBatch(ctx, firstHalf, serverURL)
	firstCrashes := countCrashErrors(firstResults)

	if firstCrashes > len(firstHalf)/2 {
		// Poison in first half — requeue second half
		w.logger.Info("Bisection: crash in first half, requeueing second half")
		w.requeueJobs(ctx, secondHalf)

		// Wait for vLLM to recover
		time.Sleep(3 * time.Second)
		w.bisectAndIsolate(ctx, firstHalf, modelType, serverURL, depth+1)
	} else {
		// First half OK (already completed by runBatch) — test second half
		w.logger.Info("Bisection: first half OK, testing second half")

		// Wait briefly for vLLM stability
		time.Sleep(1 * time.Second)

		secondResults := w.runBatch(ctx, secondHalf, serverURL)
		secondCrashes := countCrashErrors(secondResults)

		if secondCrashes > len(secondHalf)/2 {
			// Poison in second half
			// Wait for vLLM to recover
			time.Sleep(3 * time.Second)
			w.bisectAndIsolate(ctx, secondHalf, modelType, serverURL, depth+1)
		} else {
			// Neither half crashes alone — system issue, not a poison pill
			w.logger.Warn("Bisection: neither half crashes alone, possible transient system issue")
		}
	}
}

// requeueJobs remet des jobs en pending (pour bisection)
func (w *Worker) requeueJobs(ctx context.Context, jobs []Job) {
	for _, j := range jobs {
		idBytes, _ := j.ID.MarshalBinary()
		_, err := w.db.ExecContext(ctx, `
			UPDATE gpu_jobs SET status = 'pending', batch_id = NULL, started_at = NULL
			WHERE id = ?
		`, idBytes)
		if err != nil {
			w.logger.Error("Failed to requeue job", "job_id", j.ID, "error", err)
		}
	}
	w.logger.Info("Requeued jobs", "count", len(jobs))
}

// markPoison forces a job to poison status (bypass attempt counter)
func (w *Worker) markPoison(ctx context.Context, job Job, reason string) {
	idBytes, _ := job.ID.MarshalBinary()
	now := time.Now().Unix()

	_, err := w.db.ExecContext(ctx, `
		UPDATE gpu_jobs SET status = 'poison', attempts = ?, last_error = ?, completed_at = ?
		WHERE id = ?
	`, w.cfg.MaxAttempts, reason, now, idBytes)
	if err != nil {
		w.logger.Error("Failed to mark job as poison", "job_id", job.ID, "error", err)
	}
}

// isCrashError returns true if the error looks like a vLLM crash (not a payload-level error)
func isCrashError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout")
}

// countCrashErrors counts crash-type errors in results
func countCrashErrors(results []jobResult) int {
	count := 0
	for _, r := range results {
		if r.err != nil && isCrashError(r.err) {
			count++
		}
	}
	return count
}

// selectRunnableModel returns the highest-priority model type that has pending jobs AND a running server.
// Returns "" if nothing can be processed right now.
func (w *Worker) selectRunnableModel(ctx context.Context) string {
	models := w.selectModelsWithPending(ctx)
	if len(models) == 0 {
		return ""
	}
	for _, modelType := range models {
		if w.service.IsInstanceRunning(modelType) {
			return modelType
		}
		w.logger.Info("vLLM instance not running, trying next priority",
			"model", modelType)
	}
	w.logger.Info("No runnable model found", "candidates", fmt.Sprintf("%v", models))
	return ""
}

// selectModelsWithPending returns model types with pending jobs, ordered by priority (think > vision > embed)
func (w *Worker) selectModelsWithPending(ctx context.Context) []string {
	var models []string
	for _, mt := range []string{"think", "vision", "embed"} {
		var count int
		err := w.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM gpu_jobs
			WHERE model_type = ? AND status = 'pending'
		`, mt).Scan(&count)
		if err != nil {
			w.logger.Error("Failed to count jobs", "model", mt, "error", err)
			continue
		}
		if count > 0 {
			models = append(models, mt)
		}
	}
	return models
}

// Pas de redéfinition de Job - utiliser celui de job.go

// selectModel décide quel modèle utiliser (Think > Vision > Embed) — legacy, use selectRunnableModel instead
func (w *Worker) selectModel(ctx context.Context) string {
	var thinkCount, visionCount, embedCount int

	// Check Think jobs (prioritaire)
	err := w.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM gpu_jobs
		WHERE model_type = 'think' AND status = 'pending'
	`).Scan(&thinkCount)
	if err != nil {
		w.logger.Error("Failed to count think jobs", "error", err)
	}

	if thinkCount > 0 {
		return "think"
	}

	// Check Vision jobs
	err = w.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM gpu_jobs
		WHERE model_type = 'vision' AND status = 'pending'
	`).Scan(&visionCount)
	if err != nil {
		w.logger.Error("Failed to count vision jobs", "error", err)
	}

	if visionCount > 0 {
		return "vision"
	}

	// Check Embed jobs (lowest priority)
	err = w.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM gpu_jobs
		WHERE model_type = 'embed' AND status = 'pending'
	`).Scan(&embedCount)
	if err != nil {
		w.logger.Error("Failed to count embed jobs", "error", err)
	}

	if embedCount > 0 {
		return "embed"
	}

	return ""
}

// getServerURL retourne URL serveur selon type modèle
func (w *Worker) getServerURL(modelType string) string {
	switch modelType {
	case "think":
		return w.cfg.ThinkServerURL
	case "embed":
		return w.cfg.EmbedServerURL
	default:
		return w.cfg.VisionServerURL
	}
}

// claimBatch claim batch atomique de jobs
func (w *Worker) claimBatch(ctx context.Context, modelType string, batchID uuid.UUID) ([]Job, error) {
	batchIDBytes, _ := batchID.MarshalBinary()
	now := time.Now().Unix()

	// Atomic claim: UPDATE puis SELECT
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// UPDATE status + batch_id
	_, err = tx.ExecContext(ctx, `
		UPDATE gpu_jobs
		SET status = 'processing',
			batch_id = ?,
			started_at = ?
		WHERE id IN (
			SELECT id FROM gpu_jobs
			WHERE model_type = ? AND status = 'pending'
			ORDER BY created_at ASC
			LIMIT ?
		)
	`, batchIDBytes, now, modelType, w.cfg.BatchSize)
	if err != nil {
		return nil, err
	}

	// SELECT jobs claimed
	rows, err := tx.QueryContext(ctx, `
		SELECT id, payload_path, parent_id, COALESCE(fragment_index,0), COALESCE(total_fragments,1), attempts
		FROM gpu_jobs
		WHERE batch_id = ?
	`, batchIDBytes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var j Job
		var idBytes, parentBytes []byte

		err := rows.Scan(&idBytes, &j.PayloadPath, &parentBytes, &j.FragmentIdx, &j.TotalFrags, &j.Attempts)
		if err != nil {
			return nil, err
		}

		// Parse UUID from bytes
		id, err := uuid.FromBytes(idBytes)
		if err != nil {
			w.logger.Error("Failed to parse UUID", "error", err, "bytes", len(idBytes))
			continue
		}
		j.ID = id
		j.ModelType = modelType

		if parentBytes != nil {
			parentID, err := uuid.FromBytes(parentBytes)
			if err == nil {
				j.ParentID = &parentID
			}
		}

		jobs = append(jobs, j)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return jobs, nil
}

// processJob traite 1 job
func (w *Worker) processJob(ctx context.Context, job Job, serverURL string) error {
	// 1. Charger payload JSON
	payload, err := w.loadPayload(job.PayloadPath)
	if err != nil {
		w.failJob(ctx, job, fmt.Errorf("load payload: %w", err))
		return err
	}

	// Embed jobs use a different API endpoint and request/response types
	if job.ModelType == "embed" {
		return w.processEmbedJob(ctx, job, serverURL, payload)
	}

	// 2. Construire requête vLLM
	req, err := w.buildVLLMRequest(payload, job.ModelType)
	if err != nil {
		w.failJob(ctx, job, fmt.Errorf("build request: %w", err))
		return err
	}

	// 3. Envoyer requête HTTP
	jobCtx, cancel := context.WithTimeout(ctx, w.cfg.WorkerTimeout)
	defer cancel()

	resp, err := w.httpClient.SendRequest(jobCtx, serverURL, req)
	if err != nil {
		w.failJob(ctx, job, fmt.Errorf("vllm request: %w", err))
		return err
	}

	// 4. Sauver résultat
	if err := w.completeJob(ctx, job, resp); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	// 5. Check fan-in
	if job.ParentID != nil {
		w.checkFanIn(ctx, job.ID)
	}

	return nil
}

// processEmbedJob handles embedding jobs via /v1/embeddings endpoint
func (w *Worker) processEmbedJob(ctx context.Context, job Job, serverURL string, payload map[string]interface{}) error {
	req, err := w.buildEmbedRequest(payload)
	if err != nil {
		w.failJob(ctx, job, fmt.Errorf("build embed request: %w", err))
		return err
	}

	jobCtx, cancel := context.WithTimeout(ctx, w.cfg.WorkerTimeout)
	defer cancel()

	resp, err := w.httpClient.SendEmbeddingRequest(jobCtx, serverURL, req)
	if err != nil {
		w.failJob(ctx, job, fmt.Errorf("embed request: %w", err))
		return err
	}

	if err := w.completeEmbedJob(ctx, job, resp); err != nil {
		return fmt.Errorf("complete embed job: %w", err)
	}

	return nil
}

// buildEmbedRequest constructs an embedding request from payload
func (w *Worker) buildEmbedRequest(payload map[string]interface{}) (EmbeddingRequest, error) {
	textsRaw, ok := payload["texts"].([]interface{})
	if !ok {
		return EmbeddingRequest{}, fmt.Errorf("missing texts in embed payload")
	}

	texts := make([]string, 0, len(textsRaw))
	for _, t := range textsRaw {
		if s, ok := t.(string); ok {
			texts = append(texts, s)
		}
	}

	if len(texts) == 0 {
		return EmbeddingRequest{}, fmt.Errorf("no valid texts in embed payload")
	}

	return EmbeddingRequest{
		Model: "/models/gte-Qwen2-1.5B-instruct",
		Input: texts,
	}, nil
}

// completeEmbedJob saves embedding result and marks job done
func (w *Worker) completeEmbedJob(ctx context.Context, job Job, resp *EmbeddingResponse) error {
	idBytes, _ := job.ID.MarshalBinary()
	now := time.Now().Unix()

	resultPath := job.PayloadPath + ".result"
	resultData, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(resultPath, resultData, 0644); err != nil {
		return err
	}

	result, err := w.db.ExecContext(ctx, `
		UPDATE gpu_jobs
		SET status = 'done',
			result_path = ?,
			completed_at = ?
		WHERE id = ?
	`, resultPath, now, idBytes)
	if err != nil {
		w.logger.Error("Failed to update embed job status", "job_id", job.ID, "error", err)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		w.logger.Warn("Embed job UPDATE affected 0 rows", "job_id", job.ID)
	} else {
		w.logger.Info("Embed job completed",
			"job_id", job.ID,
			"embeddings", len(resp.Data),
			"tokens", resp.Usage.TotalTokens)
	}

	return nil
}

// loadPayload charge payload JSON depuis fichier
func (w *Worker) loadPayload(payloadPath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(payloadPath)
	if err != nil {
		return nil, err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	return payload, nil
}

// buildVLLMRequest construit requête vLLM selon type modèle
func (w *Worker) buildVLLMRequest(payload map[string]interface{}, modelType string) (VLLMRequest, error) {
	if modelType == "vision" {
		return w.buildVisionRequest(payload)
	}
	return w.buildThinkRequest(payload)
}

// buildVisionRequest construit requête OCR Vision
func (w *Worker) buildVisionRequest(payload map[string]interface{}) (VLLMRequest, error) {
	// Extraire image_data (base64)
	imageData, ok := payload["image_data"].(string)
	if !ok {
		return VLLMRequest{}, fmt.Errorf("missing image_data in payload")
	}

	format, _ := payload["format"].(string)
	if format == "" {
		format = "png"
	}

	// Data URL
	dataURL := fmt.Sprintf("data:image/%s;base64,%s", format, imageData)

	systemPrompt := "You are an expert OCR system. Extract ALL text from the image exactly as it appears."

	return VLLMRequest{
		Model: "/models/qwen2-vl-7b-instruct",
		Messages: []ChatMessage{
			{
				Role: "user",
				Content: []ContentPart{
					{
						Type: "image_url",
						ImageURL: &ImageURL{
							URL: dataURL,
						},
					},
					{
						Type: "text",
						Text: systemPrompt + "\n\nExtract all text:",
					},
				},
			},
		},
		MaxTokens:   2000,
		Temperature: 0.1,
	}, nil
}

// buildThinkRequest construit requête Think LLM.
// Supports both new format (system_prompt + user_prompt) and legacy (prompt).
func (w *Worker) buildThinkRequest(payload map[string]interface{}) (VLLMRequest, error) {
	systemPrompt, _ := payload["system_prompt"].(string)
	userPrompt, _ := payload["user_prompt"].(string)

	// Fallback legacy: champ "prompt" unique
	if userPrompt == "" {
		prompt, ok := payload["prompt"].(string)
		if !ok {
			return VLLMRequest{}, fmt.Errorf("missing prompt/user_prompt in payload")
		}
		userPrompt = prompt
	}

	var messages []ChatMessage
	if systemPrompt != "" {
		messages = append(messages, ChatMessage{
			Role:    "system",
			Content: []ContentPart{{Type: "text", Text: systemPrompt}},
		})
	}
	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: []ContentPart{{Type: "text", Text: userPrompt}},
	})

	maxTokens := 1000
	if mt, ok := payload["max_tokens"].(float64); ok {
		maxTokens = int(mt)
	}
	temperature := float32(0.7)
	if t, ok := payload["temperature"].(float64); ok {
		temperature = float32(t)
	}

	return VLLMRequest{
		Model:       "/models/Qwen3-8B-NVFP4",
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}, nil
}

// completeJob marque job done et sauve résultat
func (w *Worker) completeJob(ctx context.Context, job Job, resp *VLLMResponse) error {
	idBytes, _ := job.ID.MarshalBinary()
	now := time.Now().Unix()

	// Sauver résultat JSON
	resultPath := job.PayloadPath + ".result"
	resultData, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(resultPath, resultData, 0644); err != nil {
		return err
	}

	// Update DB
	result, err := w.db.ExecContext(ctx, `
		UPDATE gpu_jobs
		SET status = 'done',
			result_path = ?,
			completed_at = ?
		WHERE id = ?
	`, resultPath, now, idBytes)

	if err != nil {
		w.logger.Error("Failed to update job status",
			"job_id", job.ID,
			"error", err)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		w.logger.Warn("Job UPDATE affected 0 rows",
			"job_id", job.ID,
			"id_bytes_len", len(idBytes))
	} else {
		w.logger.Info("Job completed",
			"job_id", job.ID,
			"tokens", resp.Usage.TotalTokens)
	}

	return nil
}

// failJob marque job failed et incrémente attempts
func (w *Worker) failJob(ctx context.Context, job Job, jobErr error) {
	idBytes, _ := job.ID.MarshalBinary()
	now := time.Now().Unix()

	newAttempts := job.Attempts + 1
	newStatus := "failed"
	if newAttempts >= w.cfg.MaxAttempts {
		newStatus = "poison"
		w.logger.Error("Job marked as poison pill",
			"job_id", job.ID,
			"attempts", newAttempts,
			"error", jobErr)
	}

	w.db.ExecContext(ctx, `
		UPDATE gpu_jobs
		SET status = ?,
			attempts = ?,
			last_error = ?,
			completed_at = ?
		WHERE id = ?
	`, newStatus, newAttempts, jobErr.Error(), now, idBytes)
}

// checkFanIn vérifie si tous les fragments d'un parent sont terminés et agrège les résultats
func (w *Worker) checkFanIn(ctx context.Context, jobID uuid.UUID) {
	// 1. Récupérer le job pour obtenir le parent_id
	job, err := w.getJob(ctx, jobID)
	if err != nil {
		w.logger.Error("Failed to get job for fan-in", "job_id", jobID, "error", err)
		return
	}

	// Si pas de parent, pas de fan-in
	if job.ParentID == nil {
		return
	}

	parentID := *job.ParentID
	parentIDBytes, _ := parentID.MarshalBinary()

	// 2. Compter les fragments done vs total
	var doneCount, totalFrags int
	err = w.db.QueryRowContext(ctx, `
		SELECT
			COUNT(CASE WHEN status = 'done' THEN 1 END) as done_count,
			MAX(total_fragments) as total_frags
		FROM gpu_jobs
		WHERE parent_id = ?
	`, parentIDBytes).Scan(&doneCount, &totalFrags)

	if err != nil {
		w.logger.Error("Failed to count fragments", "parent_id", parentID, "error", err)
		return
	}

	w.logger.Info("Fan-in progress",
		"parent_id", parentID,
		"done", doneCount,
		"total", totalFrags)

	// 3. Si tous les fragments sont done, agréger
	if doneCount == totalFrags {
		if err := w.aggregateFragments(ctx, parentID, totalFrags); err != nil {
			w.logger.Error("Failed to aggregate fragments", "parent_id", parentID, "error", err)
		} else {
			w.logger.Info("Fan-in completed", "parent_id", parentID, "fragments", totalFrags)
		}
	}
}

// getJob récupère un job par son ID
func (w *Worker) getJob(ctx context.Context, jobID uuid.UUID) (*Job, error) {
	idBytes, _ := jobID.MarshalBinary()

	var j Job
	var parentBytes []byte

	err := w.db.QueryRowContext(ctx, `
		SELECT payload_path, parent_id, COALESCE(fragment_index,0), COALESCE(total_fragments,1), model_type, attempts
		FROM gpu_jobs
		WHERE id = ?
	`, idBytes).Scan(&j.PayloadPath, &parentBytes, &j.FragmentIdx, &j.TotalFrags, &j.ModelType, &j.Attempts)

	if err != nil {
		return nil, err
	}

	j.ID = jobID

	if parentBytes != nil {
		parentID, err := uuid.FromBytes(parentBytes)
		if err == nil {
			j.ParentID = &parentID
		}
	}

	return &j, nil
}

// aggregateFragments agrège les résultats de tous les fragments
func (w *Worker) aggregateFragments(ctx context.Context, parentID uuid.UUID, totalFrags int) error {
	parentIDBytes, _ := parentID.MarshalBinary()

	// 1. Récupérer tous les fragments done dans l'ordre
	rows, err := w.db.QueryContext(ctx, `
		SELECT result_path, fragment_index
		FROM gpu_jobs
		WHERE parent_id = ? AND status = 'done'
		ORDER BY fragment_index ASC
	`, parentIDBytes)
	if err != nil {
		return fmt.Errorf("query fragments: %w", err)
	}
	defer rows.Close()

	// 2. Charger et concaténer les résultats OCR
	var aggregatedText strings.Builder
	var totalTokens int

	for rows.Next() {
		var resultPath string
		var fragIdx int

		if err := rows.Scan(&resultPath, &fragIdx); err != nil {
			return fmt.Errorf("scan fragment: %w", err)
		}

		// Charger le résultat JSON
		data, err := os.ReadFile(resultPath)
		if err != nil {
			w.logger.Warn("Failed to read fragment result", "path", resultPath, "error", err)
			continue
		}

		var resp VLLMResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			w.logger.Warn("Failed to parse fragment result", "path", resultPath, "error", err)
			continue
		}

		// Extraire le texte OCR
		if len(resp.Choices) > 0 {
			aggregatedText.WriteString(resp.Choices[0].Message.Content)
			aggregatedText.WriteString("\n\n---\n\n") // Séparateur entre fragments
		}

		totalTokens += resp.Usage.TotalTokens
	}

	// 3. Créer le résultat agrégé
	aggregatedResult := map[string]interface{}{
		"parent_id":       parentID.String(),
		"total_fragments": totalFrags,
		"total_tokens":    totalTokens,
		"aggregated_text": aggregatedText.String(),
		"timestamp":       time.Now().Unix(),
	}

	// 4. Sauvegarder le résultat agrégé
	resultPath := fmt.Sprintf("/tmp/gpu_feeder_v3/stage_vision/pending/%s_aggregated.json", parentID.String())
	resultData, err := json.MarshalIndent(aggregatedResult, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal aggregated result: %w", err)
	}

	if err := os.WriteFile(resultPath, resultData, 0644); err != nil {
		return fmt.Errorf("write aggregated result: %w", err)
	}

	w.logger.Info("Aggregated result saved",
		"parent_id", parentID,
		"result_path", resultPath,
		"total_tokens", totalTokens,
		"text_length", aggregatedText.Len())

	return nil
}
