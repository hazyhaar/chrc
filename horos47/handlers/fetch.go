package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
)

// attachmentMeta matches one entry in payload["attachments"].
type attachmentMeta struct {
	AttachmentID string `json:"attachment_id"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
}

// fileMeta matches the JSON returned by HORUM attachment meta endpoint.
type fileMeta struct {
	FilePath      string      `json:"FilePath"`
	FileSizeBytes int64       `json:"FileSizeBytes"`
	SHA256Hash    string      `json:"SHA256Hash"`
	TotalChunks   int         `json:"TotalChunks"`
	Chunks        []chunkMeta `json:"Chunks"`
}

// chunkMeta matches a single chunk entry from the meta endpoint.
type chunkMeta struct {
	ChunkIndex  int    `json:"ChunkIndex"`
	OffsetBytes int64  `json:"OffsetBytes"`
	SizeBytes   int64  `json:"SizeBytes"`
	SHA256Hash  string `json:"SHA256Hash"`
}

// HandleFetchAndIngest downloads attachments from HORUM via chunked transfer.
func (h *Handlers) HandleFetchAndIngest(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("fetch_and_ingest: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)

	attachments, err := extractAttachments(payload)
	if err != nil || len(attachments) == 0 {
		h.Logger.Info("fetch_and_ingest: no attachments, completing", "envelope_id", envelopeID.String())
		result := map[string]interface{}{"status": "no_attachments"}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("submit next step: %w", err)
		}
		return result, nil
	}

	horumURL, err := h.GW.GetConfigParam("horum_pull_url")
	if err != nil || horumURL == "" {
		return nil, fmt.Errorf("fetch_and_ingest: horum_pull_url not configured")
	}

	ingestDir := "/inference/agents/sources/inbox"
	os.MkdirAll(ingestDir, 0755)

	var ingestedFiles []string
	for _, att := range attachments {
		outPath, err := h.downloadAndReassemble(ctx, horumURL, att, envelopeID.String(), ingestDir)
		if err != nil {
			h.Logger.Error("fetch_and_ingest: download failed",
				"attachment_id", att.AttachmentID, "error", err)
			continue
		}
		ingestedFiles = append(ingestedFiles, outPath)
	}

	result := map[string]interface{}{
		"status":         "ingestion_started",
		"files_ingested": len(ingestedFiles),
		"files":          ingestedFiles,
	}
	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("submit next step: %w", err)
	}
	return result, nil
}

func (h *Handlers) downloadAndReassemble(ctx context.Context, horumURL string, att attachmentMeta, envelopeIDStr, ingestDir string) (string, error) {
	// Use H3Client (QUIC/HTTP3) for large file downloads, fallback to HTTPClient
	httpClient := h.H3Client
	if httpClient == nil {
		httpClient = h.HTTPClient
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	// Resolve H3 URL: replace port with :9444 for QUIC transfers
	h3URL := horumURL
	if h.H3Client != nil {
		if resolved, err := h.resolveH3URL(horumURL); err == nil {
			h3URL = resolved
		}
	}

	metaURL := fmt.Sprintf("%s/api/internal/edge/attachment/%s/meta", h3URL, att.AttachmentID)
	req, err := http.NewRequestWithContext(ctx, "GET", metaURL, nil)
	if err != nil {
		return "", fmt.Errorf("create meta request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		// Fallback to TCP client on QUIC failure
		h.Logger.Warn("H3 meta request failed, falling back to TCP", "error", err)
		httpClient = h.HTTPClient
		if httpClient == nil {
			httpClient = http.DefaultClient
		}
		h3URL = horumURL
		metaURL = fmt.Sprintf("%s/api/internal/edge/attachment/%s/meta", h3URL, att.AttachmentID)
		req, _ = http.NewRequestWithContext(ctx, "GET", metaURL, nil)
		resp, err = httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("meta request: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("meta returned %d: %s", resp.StatusCode, string(body))
	}

	var meta fileMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return "", fmt.Errorf("decode meta: %w", err)
	}

	h.Logger.Info("downloading attachment",
		"attachment_id", att.AttachmentID,
		"filename", att.Filename,
		"total_chunks", meta.TotalChunks,
		"size_bytes", meta.FileSizeBytes,
		"protocol", resp.Proto)

	stagingDir := "/inference/agents/sources/staging"
	if sd, err := h.GW.GetConfigParam("staging_dir"); err == nil && sd != "" {
		stagingDir = sd
	}
	chunkDir := filepath.Join(stagingDir, "fetch", envelopeIDStr, att.AttachmentID)
	os.MkdirAll(chunkDir, 0755)

	// Download chunks in parallel (max 4 concurrent)
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	finalClient := httpClient
	finalURL := h3URL

	for _, chunk := range meta.Chunks {
		wg.Add(1)
		go func(chunk chunkMeta) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := downloadChunk(ctx, finalClient, finalURL, att.AttachmentID, chunk, chunkDir); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(chunk)
	}
	wg.Wait()

	if firstErr != nil {
		return "", fmt.Errorf("chunk download: %w", firstErr)
	}

	// Reassemble
	outPath := filepath.Join(ingestDir, att.Filename)
	outFile, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("create output: %w", err)
	}

	fileHasher := sha256.New()
	writer := io.MultiWriter(outFile, fileHasher)
	for i := 0; i < meta.TotalChunks; i++ {
		chunkPath := filepath.Join(chunkDir, fmt.Sprintf("chunk_%04d", i))
		chunkData, err := os.ReadFile(chunkPath)
		if err != nil {
			outFile.Close()
			os.Remove(outPath)
			return "", fmt.Errorf("read chunk %d for reassembly: %w", i, err)
		}
		writer.Write(chunkData)
	}
	outFile.Close()

	if hex.EncodeToString(fileHasher.Sum(nil)) != meta.SHA256Hash {
		os.Remove(outPath)
		return "", fmt.Errorf("global SHA256 mismatch")
	}

	os.RemoveAll(chunkDir)
	h.Logger.Info("attachment downloaded and reassembled",
		"attachment_id", att.AttachmentID,
		"filename", att.Filename,
		"output", outPath)
	return outPath, nil
}

// downloadChunk downloads and verifies a single chunk to disk.
func downloadChunk(ctx context.Context, client *http.Client, baseURL, attachmentID string, chunk chunkMeta, chunkDir string) error {
	chunkURL := fmt.Sprintf("%s/api/internal/edge/attachment/%s/chunk/%d", baseURL, attachmentID, chunk.ChunkIndex)
	chunkReq, err := http.NewRequestWithContext(ctx, "GET", chunkURL, nil)
	if err != nil {
		return fmt.Errorf("chunk %d request: %w", chunk.ChunkIndex, err)
	}

	chunkResp, err := client.Do(chunkReq)
	if err != nil {
		return fmt.Errorf("chunk %d download: %w", chunk.ChunkIndex, err)
	}
	chunkData, err := io.ReadAll(chunkResp.Body)
	chunkResp.Body.Close()

	if chunkResp.StatusCode != http.StatusOK {
		return fmt.Errorf("chunk %d returned %d", chunk.ChunkIndex, chunkResp.StatusCode)
	}
	if err != nil {
		return fmt.Errorf("read chunk %d: %w", chunk.ChunkIndex, err)
	}

	chunkHash := sha256.New()
	chunkHash.Write(chunkData)
	if hex.EncodeToString(chunkHash.Sum(nil)) != chunk.SHA256Hash {
		return fmt.Errorf("chunk %d SHA256 mismatch", chunk.ChunkIndex)
	}

	chunkPath := filepath.Join(chunkDir, fmt.Sprintf("chunk_%04d", chunk.ChunkIndex))
	return os.WriteFile(chunkPath, chunkData, 0644)
}

// resolveH3URL resolves the HTTP/3 URL from config or defaults to port :9444.
func (h *Handlers) resolveH3URL(horumURL string) (string, error) {
	// Check config first
	if h3URL, err := h.GW.GetConfigParam("horum_h3_url"); err == nil && h3URL != "" {
		return h3URL, nil
	}
	// Default: same host, port 9444
	u, err := url.Parse(horumURL)
	if err != nil {
		return "", err
	}
	u.Host = u.Hostname() + ":9444"
	return u.String(), nil
}

func extractAttachments(payload map[string]interface{}) ([]attachmentMeta, error) {
	var attsRaw interface{}
	if a, ok := payload["attachments"]; ok {
		attsRaw = a
	} else if payloadStr, ok := payload["payload"].(string); ok {
		var nested map[string]interface{}
		if err := json.Unmarshal([]byte(payloadStr), &nested); err == nil {
			attsRaw = nested["attachments"]
		}
	}
	if attsRaw == nil {
		return nil, nil
	}
	rawJSON, err := json.Marshal(attsRaw)
	if err != nil {
		return nil, fmt.Errorf("marshal attachments: %w", err)
	}
	var attachments []attachmentMeta
	if err := json.Unmarshal(rawJSON, &attachments); err != nil {
		return nil, fmt.Errorf("unmarshal attachments: %w", err)
	}
	return attachments, nil
}
