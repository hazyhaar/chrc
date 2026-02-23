package gpufeeder

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// GPUSubmitter is a lightweight client that submits Think jobs to the gpu_jobs table
// and polls for results. Used by horos47 handlers to invoke the GPU Feeder V3 daemon.
type GPUSubmitter struct {
	db      *sql.DB
	dataDir string
	logger  *slog.Logger
}

// GenerateResult holds the parsed response from a Think GPU job.
type GenerateResult struct {
	Text         string
	Model        string
	TokensUsed   int
	FinishReason string
}

// NewGPUSubmitter opens the shared gpu_jobs database and ensures the schema exists.
func NewGPUSubmitter(jobsDBPath, dataDir string, logger *slog.Logger) (*GPUSubmitter, error) {
	// Ensure directories exist
	if err := os.MkdirAll(filepath.Dir(jobsDBPath), 0755); err != nil {
		return nil, fmt.Errorf("mkdir jobs db: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "think"), 0755); err != nil {
		return nil, fmt.Errorf("mkdir think data: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "embed"), 0755); err != nil {
		return nil, fmt.Errorf("mkdir embed data: %w", err)
	}

	dsn := jobsDBPath + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(10000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open jobs db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Apply schema to guarantee gpu_jobs table exists
	schemaSQL, err := os.ReadFile("/inference/horos47/services/gpufeeder/schema.sql")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("read schema: %w", err)
	}
	if _, err := db.Exec(string(schemaSQL)); err != nil {
		db.Close()
		return nil, fmt.Errorf("exec schema: %w", err)
	}

	return &GPUSubmitter{
		db:      db,
		dataDir: dataDir,
		logger:  logger,
	}, nil
}

// Generate submits a Think job and polls until completion or context cancellation.
// Implémente la déduplication sémantique par prompt_hash : si un job identique
// (même system+user prompt et maxTokens) est déjà done/processing/pending,
// le résultat existant est réutilisé sans créer de nouveau job.
func (s *GPUSubmitter) Generate(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (*GenerateResult, error) {
	// 1. Calculer prompt_hash pour déduplication sémantique
	promptHashRaw := sha256.Sum256([]byte(systemPrompt + "\x00" + userPrompt + "\x00" + fmt.Sprintf("%d", maxTokens)))
	promptHash := fmt.Sprintf("%x", promptHashRaw)

	// 2. Chercher job existant avec le même prompt_hash
	var existingID []byte
	var existingStatus string
	var existingResultPath sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, status, result_path FROM gpu_jobs
		WHERE prompt_hash = ? AND status IN ('done', 'processing', 'pending')
		ORDER BY created_at DESC LIMIT 1
	`, promptHash).Scan(&existingID, &existingStatus, &existingResultPath)

	if err == nil {
		// Job existant trouvé
		switch existingStatus {
		case "done":
			if existingResultPath.Valid {
				s.logger.Info("Think prompt_hash cache hit (done)",
					"prompt_hash", promptHash[:16])
				return s.readResult(existingResultPath.String)
			}
		case "processing", "pending":
			s.logger.Info("Think prompt_hash dedup (attaching to existing job)",
				"prompt_hash", promptHash[:16],
				"status", existingStatus)
			return s.pollJobResult(ctx, existingID)
		}
	}

	// 3. Pas de job existant — créer un nouveau
	jobID := uuid.Must(uuid.NewV7())
	idBytes, _ := jobID.MarshalBinary()

	payload := map[string]interface{}{
		"system_prompt": systemPrompt,
		"user_prompt":   userPrompt,
		"max_tokens":    maxTokens,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	payloadPath := filepath.Join(s.dataDir, "think", jobID.String()+".json")
	if err := os.WriteFile(payloadPath, payloadJSON, 0644); err != nil {
		return nil, fmt.Errorf("write payload: %w", err)
	}

	hash := sha256.Sum256(payloadJSON)
	payloadSHA := fmt.Sprintf("%x", hash)

	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO gpu_jobs (id, payload_sha256, model_type, payload_path, status, created_at, prompt_hash)
		VALUES (?, ?, 'think', ?, 'pending', ?, ?)
	`, idBytes, payloadSHA, payloadPath, now, promptHash)
	if err != nil {
		os.Remove(payloadPath)
		// Race condition : un autre goroutine a inséré le même prompt_hash entre temps
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			s.logger.Info("Think prompt_hash race condition, retrying dedup lookup",
				"prompt_hash", promptHash[:16])
			return s.Generate(ctx, systemPrompt, userPrompt, maxTokens)
		}
		return nil, fmt.Errorf("insert gpu_job: %w", err)
	}

	s.logger.Info("GPU job submitted",
		"job_id", jobID.String(),
		"model", "think",
		"max_tokens", maxTokens,
		"prompt_hash", promptHash[:16])

	return s.pollJobResult(ctx, idBytes)
}

// pollJobResult attend qu'un job GPU soit terminé et retourne le résultat.
func (s *GPUSubmitter) pollJobResult(ctx context.Context, idBytes []byte) (*GenerateResult, error) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("gpu job timed out: %w", ctx.Err())
		case <-ticker.C:
			var status string
			var resultPath sql.NullString
			err := s.db.QueryRowContext(ctx, `
				SELECT status, result_path FROM gpu_jobs WHERE id = ?
			`, idBytes).Scan(&status, &resultPath)
			if err != nil {
				return nil, fmt.Errorf("poll gpu_job: %w", err)
			}

			switch status {
			case "done":
				if !resultPath.Valid {
					return nil, fmt.Errorf("gpu job done but no result_path")
				}
				return s.readResult(resultPath.String)
			case "poison":
				return nil, fmt.Errorf("gpu job failed permanently (poison)")
			case "failed":
				continue
			case "pending", "processing":
				continue
			default:
				return nil, fmt.Errorf("unexpected gpu job status: %s", status)
			}
		}
	}
}

// readResult reads and parses a VLLMResponse from the result file.
func (s *GPUSubmitter) readResult(resultPath string) (*GenerateResult, error) {
	data, err := os.ReadFile(resultPath)
	if err != nil {
		return nil, fmt.Errorf("read result: %w", err)
	}

	var resp VLLMResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse result: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("vllm response has no choices")
	}

	return &GenerateResult{
		Text:         resp.Choices[0].Message.Content,
		Model:        resp.Model,
		TokensUsed:   resp.Usage.TotalTokens,
		FinishReason: resp.Choices[0].FinishReason,
	}, nil
}

// VisionResult holds the parsed response from a Vision OCR GPU job.
type VisionResult struct {
	Text       string
	Model      string
	TokensUsed int
}

// SubmitVision submits a Vision OCR job and polls until completion or context cancellation.
func (s *GPUSubmitter) SubmitVision(ctx context.Context, imageBase64, format string) (*VisionResult, error) {
	jobID := uuid.Must(uuid.NewV7())
	idBytes, _ := jobID.MarshalBinary()

	payload := map[string]interface{}{
		"image_data": imageBase64,
		"format":     format,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal vision payload: %w", err)
	}

	payloadPath := filepath.Join(s.dataDir, "think", jobID.String()+".json")
	if err := os.WriteFile(payloadPath, payloadJSON, 0644); err != nil {
		return nil, fmt.Errorf("write vision payload: %w", err)
	}

	hash := sha256.Sum256(payloadJSON)
	payloadSHA := fmt.Sprintf("%x", hash)

	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO gpu_jobs (id, payload_sha256, model_type, payload_path, status, created_at)
		VALUES (?, ?, 'vision', ?, 'pending', ?)
	`, idBytes, payloadSHA, payloadPath, now)
	if err != nil {
		os.Remove(payloadPath)
		return nil, fmt.Errorf("insert vision gpu_job: %w", err)
	}

	s.logger.Info("Vision GPU job submitted", "job_id", jobID.String())

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("vision gpu job timed out: %w", ctx.Err())
		case <-ticker.C:
			var status string
			var resultPath sql.NullString
			err := s.db.QueryRowContext(ctx, `
				SELECT status, result_path FROM gpu_jobs WHERE id = ?
			`, idBytes).Scan(&status, &resultPath)
			if err != nil {
				return nil, fmt.Errorf("poll vision gpu_job: %w", err)
			}

			switch status {
			case "done":
				if !resultPath.Valid {
					return nil, fmt.Errorf("vision gpu job done but no result_path")
				}
				return s.readVisionResult(resultPath.String)
			case "poison":
				return nil, fmt.Errorf("vision gpu job failed permanently (poison)")
			case "failed":
				continue
			case "pending", "processing":
				continue
			default:
				return nil, fmt.Errorf("unexpected vision gpu job status: %s", status)
			}
		}
	}
}

// readVisionResult reads and parses a VLLMResponse as a VisionResult.
func (s *GPUSubmitter) readVisionResult(resultPath string) (*VisionResult, error) {
	data, err := os.ReadFile(resultPath)
	if err != nil {
		return nil, fmt.Errorf("read vision result: %w", err)
	}

	var resp VLLMResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse vision result: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("vision response has no choices")
	}

	return &VisionResult{
		Text:       resp.Choices[0].Message.Content,
		Model:      resp.Model,
		TokensUsed: resp.Usage.TotalTokens,
	}, nil
}

// EmbedResult holds the parsed response from an Embed GPU job.
type EmbedResult struct {
	Embeddings [][]float32
	Model      string
	Dimension  int
	TokensUsed int
}

// Embed submits an embedding job and polls until completion or context cancellation.
func (s *GPUSubmitter) Embed(ctx context.Context, texts []string) (*EmbedResult, error) {
	jobID := uuid.Must(uuid.NewV7())
	idBytes, _ := jobID.MarshalBinary()

	payload := map[string]interface{}{
		"texts": texts,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embed payload: %w", err)
	}

	payloadPath := filepath.Join(s.dataDir, "embed", jobID.String()+".json")
	if err := os.WriteFile(payloadPath, payloadJSON, 0644); err != nil {
		return nil, fmt.Errorf("write embed payload: %w", err)
	}

	hash := sha256.Sum256(payloadJSON)
	payloadSHA := fmt.Sprintf("%x", hash)

	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO gpu_jobs (id, payload_sha256, model_type, payload_path, status, created_at)
		VALUES (?, ?, 'embed', ?, 'pending', ?)
	`, idBytes, payloadSHA, payloadPath, now)
	if err != nil {
		os.Remove(payloadPath)
		return nil, fmt.Errorf("insert embed gpu_job: %w", err)
	}

	s.logger.Info("Embed GPU job submitted",
		"job_id", jobID.String(),
		"texts", len(texts))

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("embed gpu job timed out: %w", ctx.Err())
		case <-ticker.C:
			var status string
			var resultPath sql.NullString
			err := s.db.QueryRowContext(ctx, `
				SELECT status, result_path FROM gpu_jobs WHERE id = ?
			`, idBytes).Scan(&status, &resultPath)
			if err != nil {
				return nil, fmt.Errorf("poll embed gpu_job: %w", err)
			}

			switch status {
			case "done":
				if !resultPath.Valid {
					return nil, fmt.Errorf("embed gpu job done but no result_path")
				}
				return s.readEmbedResult(resultPath.String)
			case "poison":
				return nil, fmt.Errorf("embed gpu job failed permanently (poison)")
			case "failed":
				continue
			case "pending", "processing":
				continue
			default:
				return nil, fmt.Errorf("unexpected embed gpu job status: %s", status)
			}
		}
	}
}

// readEmbedResult reads and parses an EmbeddingResponse from the result file.
func (s *GPUSubmitter) readEmbedResult(resultPath string) (*EmbedResult, error) {
	data, err := os.ReadFile(resultPath)
	if err != nil {
		return nil, fmt.Errorf("read embed result: %w", err)
	}

	var resp EmbeddingResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse embed result: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embedding response has no data")
	}

	embeddings := make([][]float32, len(resp.Data))
	for _, d := range resp.Data {
		if d.Index < len(embeddings) {
			embeddings[d.Index] = d.Embedding
		}
	}

	return &EmbedResult{
		Embeddings: embeddings,
		Model:      resp.Model,
		Dimension:  len(resp.Data[0].Embedding),
		TokensUsed: resp.Usage.TotalTokens,
	}, nil
}

// Close closes the database connection.
func (s *GPUSubmitter) Close() error {
	return s.db.Close()
}
