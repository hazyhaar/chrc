package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"horos47/core/data"

	"github.com/go-chi/chi/v5"
)

// ChunksInitRequest is the body for POST /api/v1/sas/chunks/init.
type ChunksInitRequest struct {
	EnvelopeID  string `json:"envelope_id"`
	TotalChunks int    `json:"total_chunks"`
	FileSize    int64  `json:"file_size"`
	SHA256      string `json:"sha256"`
}

// handleChunksInit creates a new chunked_payloads entry and returns its payload_id.
// POST /api/v1/sas/chunks/init
func (s *Service) handleChunksInit(w http.ResponseWriter, r *http.Request) {
	var req ChunksInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.EnvelopeID == "" || req.TotalChunks <= 0 || req.FileSize <= 0 || req.SHA256 == "" {
		http.Error(w, "envelope_id, total_chunks, file_size, sha256 required", http.StatusBadRequest)
		return
	}

	envelopeID, err := data.ParseUUID(req.EnvelopeID)
	if err != nil {
		http.Error(w, "Invalid envelope_id", http.StatusBadRequest)
		return
	}

	payloadID := data.NewUUID()

	// Get staging_dir from config
	stagingDir, err := s.GetConfigParam("staging_dir")
	if err != nil || stagingDir == "" {
		stagingDir = "/inference/agents/sources/staging"
	}

	storageDir := filepath.Join(stagingDir, "chunks", payloadID.String())
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		s.logger.Error("Failed to create chunk storage dir", "error", err)
		http.Error(w, "Failed to create storage", http.StatusInternalServerError)
		return
	}

	_, err = data.ExecWithRetry(s.db, `
		INSERT INTO chunked_payloads
			(payload_id, envelope_id, total_chunks, received_chunks, file_size, file_sha256, storage_dir, status, created_at)
		VALUES (?, ?, ?, 0, ?, ?, ?, 'receiving', unixepoch())
	`, payloadID, envelopeID, req.TotalChunks, req.FileSize, req.SHA256, storageDir)
	if err != nil {
		s.logger.Error("Failed to insert chunked_payload", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Chunk reception initialized",
		"payload_id", payloadID.String(),
		"envelope_id", req.EnvelopeID,
		"total_chunks", req.TotalChunks)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"payload_id": payloadID.String(),
	})
}

// handleChunkReceive receives a single chunk for an ongoing chunked payload.
// POST /api/v1/sas/chunks/{payload_id}/chunk
// Headers: X-Chunk-Index, X-Chunk-SHA256
// Body: binary data (max 10 MiB)
func (s *Service) handleChunkReceive(w http.ResponseWriter, r *http.Request) {
	payloadIDStr := chi.URLParam(r, "payload_id")
	payloadID, err := data.ParseUUID(payloadIDStr)
	if err != nil {
		http.Error(w, "Invalid payload_id", http.StatusBadRequest)
		return
	}

	chunkIndexStr := r.Header.Get("X-Chunk-Index")
	chunkSHA256 := r.Header.Get("X-Chunk-SHA256")
	if chunkIndexStr == "" || chunkSHA256 == "" {
		http.Error(w, "X-Chunk-Index and X-Chunk-SHA256 headers required", http.StatusBadRequest)
		return
	}

	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil {
		http.Error(w, "Invalid X-Chunk-Index", http.StatusBadRequest)
		return
	}

	// Read chunked payload record
	var storageDir, status string
	var totalChunks, receivedChunks int
	var envelopeID data.UUID
	err = s.db.QueryRow(`
		SELECT envelope_id, storage_dir, status, total_chunks, received_chunks
		FROM chunked_payloads WHERE payload_id = ?
	`, payloadID).Scan(&envelopeID, &storageDir, &status, &totalChunks, &receivedChunks)
	if err != nil {
		http.Error(w, "Payload not found", http.StatusNotFound)
		return
	}
	if status != "receiving" {
		http.Error(w, "Payload is not in receiving state", http.StatusConflict)
		return
	}

	// Read body (max 10 MiB + 1 byte to detect overflow)
	const maxChunk = 10<<20 + 1
	body, err := io.ReadAll(io.LimitReader(r.Body, maxChunk))
	if err != nil {
		http.Error(w, "Failed to read chunk body", http.StatusBadRequest)
		return
	}
	if int64(len(body)) >= maxChunk {
		http.Error(w, "Chunk exceeds 10 MiB", http.StatusRequestEntityTooLarge)
		return
	}

	// Verify chunk hash
	h := sha256.New()
	h.Write(body)
	actualHash := hex.EncodeToString(h.Sum(nil))
	if actualHash != chunkSHA256 {
		http.Error(w, "Chunk SHA256 mismatch", http.StatusUnprocessableEntity)
		return
	}

	// Write chunk file
	chunkPath := filepath.Join(storageDir, fmt.Sprintf("chunk_%04d", chunkIndex))
	if err := os.WriteFile(chunkPath, body, 0644); err != nil {
		s.logger.Error("Failed to write chunk", "error", err)
		http.Error(w, "Failed to store chunk", http.StatusInternalServerError)
		return
	}

	// Increment received_chunks
	newReceived := receivedChunks + 1
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE chunked_payloads SET received_chunks = ?
		WHERE payload_id = ?
	`, newReceived, payloadID)

	s.logger.Info("Chunk received",
		"payload_id", payloadIDStr,
		"chunk_index", chunkIndex,
		"received", newReceived,
		"total", totalChunks)

	// Check if all chunks received → trigger reconstruction
	if newReceived >= totalChunks {
		go s.reconstructFile(payloadID, envelopeID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"received": newReceived,
		"total":    totalChunks,
		"complete": newReceived >= totalChunks,
	})
}

// reconstructFile concatenates ordered chunks and verifies the whole-file SHA256.
func (s *Service) reconstructFile(payloadID, envelopeID data.UUID) {
	var storageDir, fileSHA256 string
	var totalChunks int
	err := s.db.QueryRow(`
		SELECT storage_dir, file_sha256, total_chunks
		FROM chunked_payloads WHERE payload_id = ?
	`, payloadID).Scan(&storageDir, &fileSHA256, &totalChunks)
	if err != nil {
		s.logger.Error("Failed to read payload for reconstruction", "error", err)
		return
	}

	// List and sort chunk files
	entries, err := os.ReadDir(storageDir)
	if err != nil {
		s.logger.Error("Failed to read chunk dir", "error", err)
		s.failChunkedPayload(payloadID, "read chunk dir: "+err.Error())
		return
	}

	var chunkFiles []string
	for _, e := range entries {
		if !e.IsDir() {
			chunkFiles = append(chunkFiles, e.Name())
		}
	}
	sort.Strings(chunkFiles)

	if len(chunkFiles) < totalChunks {
		s.failChunkedPayload(payloadID, fmt.Sprintf("expected %d chunks, found %d files", totalChunks, len(chunkFiles)))
		return
	}

	// Create output file
	outputPath := filepath.Join(storageDir, "reconstructed")
	outFile, err := os.Create(outputPath)
	if err != nil {
		s.failChunkedPayload(payloadID, "create output: "+err.Error())
		return
	}

	fileHasher := sha256.New()
	writer := io.MultiWriter(outFile, fileHasher)

	for _, name := range chunkFiles {
		chunkPath := filepath.Join(storageDir, name)
		chunkData, err := os.ReadFile(chunkPath)
		if err != nil {
			outFile.Close()
			s.failChunkedPayload(payloadID, "read chunk: "+err.Error())
			return
		}
		if _, err := writer.Write(chunkData); err != nil {
			outFile.Close()
			s.failChunkedPayload(payloadID, "write output: "+err.Error())
			return
		}
	}
	outFile.Close()

	// Verify whole-file SHA256
	actualHash := hex.EncodeToString(fileHasher.Sum(nil))
	if actualHash != fileSHA256 {
		s.failChunkedPayload(payloadID, fmt.Sprintf("SHA256 mismatch: expected %s, got %s", fileSHA256, actualHash))
		return
	}

	// Mark complete
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE chunked_payloads SET status = 'complete', completed_at = ?
		WHERE payload_id = ?
	`, time.Now().Unix(), payloadID)

	// Enrich envelope payload with attachment path
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET payload_json = json_set(payload_json, '$.attachment_path', ?)
		WHERE envelope_id = ?
	`, outputPath, envelopeID)

	s.logger.Info("File reconstructed successfully",
		"payload_id", payloadID.String(),
		"output", outputPath,
		"sha256", actualHash)
}

func (s *Service) failChunkedPayload(payloadID data.UUID, errMsg string) {
	s.logger.Error("Chunk reconstruction failed", "payload_id", payloadID.String(), "error", errMsg)
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE chunked_payloads SET status = 'failed'
		WHERE payload_id = ?
	`, payloadID)
}
