package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"horos47/core/data"
	"horos47/storage"
)

// HorumPoller polls HORUM's pull API for pending mentions and clarification answers.
// Ported from horosync QUICClient patterns: persistent connection, keepalive,
// auto-reconnect with exponential backoff.
type HorumPoller struct {
	horumURL   string       // base URL, e.g. "https://forum.docbusinessia.fr"
	httpClient *http.Client // persistent HTTP/2 client
	logger     *slog.Logger
	svc        *Service // for CreateEnvelopeFromRequest()

	mu        sync.RWMutex
	connected bool

	keepAliveInterval time.Duration // 15s
	pollInterval      time.Duration // 5s
	reconnectBackoff  time.Duration // initial: 1s
	maxReconnectDelay time.Duration // 30s
}

// NewHorumPoller creates a new poller targeting the given HORUM base URL.
func NewHorumPoller(svc *Service, horumURL string, logger *slog.Logger) *HorumPoller {
	return &HorumPoller{
		horumURL:          horumURL,
		httpClient:        svc.httpClient,
		logger:            logger,
		svc:               svc,
		keepAliveInterval: 15 * time.Second,
		pollInterval:      5 * time.Second,
		reconnectBackoff:  1 * time.Second,
		maxReconnectDelay: 30 * time.Second,
	}
}

// Run starts the poll loop. Blocks until ctx is cancelled.
func (p *HorumPoller) Run(ctx context.Context) {
	p.logger.Info("HorumPoller starting", "url", p.horumURL)

	// Start keepalive goroutine
	go p.keepAliveLoop(ctx)

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("HorumPoller stopped")
			return
		case <-ticker.C:
			if err := p.ensureConnected(ctx); err != nil {
				continue
			}
			p.pollPending(ctx)
		}
	}
}

// ensureConnected checks connectivity and reconnects if needed.
// Pattern from horosync client.go:145-154.
func (p *HorumPoller) ensureConnected(ctx context.Context) error {
	p.mu.RLock()
	if p.connected {
		p.mu.RUnlock()
		return nil
	}
	p.mu.RUnlock()

	return p.reconnectWithBackoff(ctx)
}

// reconnectWithBackoff attempts reconnection with exponential backoff.
// Pattern from horosync client.go:157-196.
func (p *HorumPoller) reconnectWithBackoff(ctx context.Context) error {
	backoff := p.reconnectBackoff

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		p.logger.Info("HorumPoller reconnect attempt", "backoff_s", backoff.Seconds())

		err := p.healthCheck(ctx)
		if err == nil {
			p.mu.Lock()
			p.connected = true
			p.mu.Unlock()
			p.logger.Info("HorumPoller connected", "url", p.horumURL)
			return nil
		}

		p.logger.Warn("HorumPoller reconnect failed", "error", err, "backoff_s", backoff.Seconds())

		select {
		case <-time.After(backoff):
			backoff *= 2
			if backoff > p.maxReconnectDelay {
				backoff = p.maxReconnectDelay
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// healthCheck verifies connectivity to HORUM by doing a lightweight GET.
func (p *HorumPoller) healthCheck(ctx context.Context) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := p.horumURL + "/api/internal/edge/pending"
	req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) // drain

	if resp.StatusCode >= 500 {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}

	return nil
}

// keepAliveLoop pings HORUM every keepAliveInterval to detect silent disconnections.
// Pattern from horosync client.go:199-251.
func (p *HorumPoller) keepAliveLoop(ctx context.Context) {
	ticker := time.NewTicker(p.keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.RLock()
			if !p.connected {
				p.mu.RUnlock()
				continue
			}
			p.mu.RUnlock()

			if err := p.healthCheck(ctx); err != nil {
				p.logger.Warn("HorumPoller keepalive failed, marking disconnected", "error", err)
				p.markDisconnected()
			}
		}
	}
}

// markDisconnected atomically marks the poller as disconnected.
// Pattern from horosync client.go:254-258.
func (p *HorumPoller) markDisconnected() {
	p.mu.Lock()
	p.connected = false
	p.mu.Unlock()
}

// polledMention matches the JSON returned by HORUM's GET /api/internal/edge/pending.
type polledMention struct {
	MentionID     string         `json:"mention_id"`
	NodeID        string         `json:"node_id"`
	MentionedUID  string         `json:"mentioned_user_id"`
	MentionedByID string         `json:"mentioned_by_user_id"`
	TargetAgent   string         `json:"target_agent"`
	Payload       map[string]any `json:"payload"`
	Provenance    map[string]any `json:"provenance"`
	MessageType   string         `json:"message_type"`
}

// pollPending fetches pending mentions from HORUM and creates task_envelopes.
func (p *HorumPoller) pollPending(ctx context.Context) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := p.horumURL + "/api/internal/edge/pending"
	req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
	if err != nil {
		p.logger.Error("HorumPoller: create request failed", "error", err)
		return
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.Warn("HorumPoller: poll failed", "error", err)
		p.markDisconnected()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		p.logger.Warn("HorumPoller: poll returned error", "status", resp.StatusCode, "body", string(body))
		if resp.StatusCode >= 500 {
			p.markDisconnected()
		}
		return
	}

	var mentions []polledMention
	if err := json.NewDecoder(resp.Body).Decode(&mentions); err != nil {
		p.logger.Error("HorumPoller: decode response failed", "error", err)
		return
	}

	if len(mentions) == 0 {
		return
	}

	p.logger.Info("HorumPoller: received items", "count", len(mentions))

	var mentionAckIDs []string
	var contentAckIDs []string

	for _, m := range mentions {
		switch m.MessageType {
		case "clarification_answer":
			if err := p.handleClarificationAnswer(ctx, m); err != nil {
				p.logger.Error("HorumPoller: clarification answer failed",
					"mention_id", m.MentionID, "error", err)
				continue
			}
			mentionAckIDs = append(mentionAckIDs, m.MentionID)

		case "content_sync":
			if err := p.handleContentSync(ctx, m); err != nil {
				p.logger.Error("HorumPoller: content sync failed",
					"queue_id", m.MentionID, "error", err)
				continue
			}
			contentAckIDs = append(contentAckIDs, m.MentionID)

		default: // "mention" or empty
			submitReq := SubmitRequest{
				OriginMentionID: m.MentionID,
				OriginNodeID:    m.NodeID,
				OriginUserID:    m.MentionedByID,
				AgentName:       m.TargetAgent,
				Payload:         m.Payload,
				Provenance:      m.Provenance,
			}

			envelopeID, err := p.svc.CreateEnvelopeFromRequest(submitReq)
			if err != nil {
				p.logger.Error("HorumPoller: create envelope failed",
					"mention_id", m.MentionID, "error", err)
				continue
			}

			p.logger.Info("HorumPoller: envelope created",
				"mention_id", m.MentionID,
				"envelope_id", envelopeID.String(),
				"agent", m.TargetAgent)
			mentionAckIDs = append(mentionAckIDs, m.MentionID)
		}
	}

	// Send acknowledgment
	if len(mentionAckIDs) > 0 || len(contentAckIDs) > 0 {
		p.sendAck(ctx, mentionAckIDs, contentAckIDs)
	}
}

// handleClarificationAnswer processes a clarification answer from the pull queue.
func (p *HorumPoller) handleClarificationAnswer(ctx context.Context, m polledMention) error {
	envelopeID, _ := m.Payload["envelope_id"].(string)
	if envelopeID == "" {
		return fmt.Errorf("no envelope_id in clarification answer payload")
	}

	answersRaw, _ := m.Payload["clarification_answers"]
	answersJSON, err := json.Marshal(answersRaw)
	if err != nil {
		return fmt.Errorf("marshal clarification answers: %w", err)
	}

	// Call the gateway's clarification handler internally
	p.svc.processClarificationAnswerInternal(ctx, envelopeID, answersJSON)

	p.logger.Info("HorumPoller: clarification answer processed",
		"mention_id", m.MentionID,
		"envelope_id", envelopeID)

	return nil
}

// sendAck acknowledges received mentions and content items to HORUM.
func (p *HorumPoller) sendAck(ctx context.Context, mentionIDs, contentIDs []string) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string][]string{
		"mention_ids": mentionIDs,
		"content_ids": contentIDs,
	})

	url := p.horumURL + "/api/internal/edge/ack"
	req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewReader(body))
	if err != nil {
		p.logger.Error("HorumPoller: create ack request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.Warn("HorumPoller: ack failed", "error", err)
		return
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) // drain

	p.logger.Info("HorumPoller: acknowledged",
		"mentions", len(mentionIDs), "content", len(contentIDs))
}

// handleContentSync processes a content_sync item (text or attachment).
func (p *HorumPoller) handleContentSync(ctx context.Context, m polledMention) error {
	contentType, _ := m.Payload["content_type"].(string)
	switch contentType {
	case "text":
		return p.ingestText(ctx, m)
	case "attachment":
		return p.ingestAttachment(ctx, m)
	default:
		return fmt.Errorf("unknown content_type: %s", contentType)
	}
}

// ingestText directly ingests forum text content into the RAG database.
// Deduplicates by source: deletes any existing document for this node before inserting.
func (p *HorumPoller) ingestText(ctx context.Context, m polledMention) error {
	title, _ := m.Payload["title"].(string)
	content, _ := m.Payload["content_markdown"].(string)
	nodeID, _ := m.Payload["node_id"].(string)

	if content == "" {
		return nil // Skip empty content
	}

	textChunks := storage.ChunkBySentences(content, 200)
	if len(textChunks) == 0 {
		return nil
	}

	source := "horum:" + nodeID

	// Delete previous document for this source (dedup).
	// FTS5 virtual tables don't support CASCADE, so clean them manually.
	_, _ = p.svc.db.ExecContext(ctx, `DELETE FROM chunks_fts WHERE chunk_id IN
		(SELECT chunk_id FROM chunks WHERE document_id IN
			(SELECT document_id FROM documents WHERE source = ?))`, source)
	// embeddings cascade from chunks; chunks don't cascade from documents — delete explicitly.
	_, _ = p.svc.db.ExecContext(ctx, `DELETE FROM embeddings WHERE document_id IN
		(SELECT document_id FROM documents WHERE source = ?)`, source)
	_, _ = p.svc.db.ExecContext(ctx, `DELETE FROM chunks WHERE document_id IN
		(SELECT document_id FROM documents WHERE source = ?)`, source)
	_, _ = p.svc.db.ExecContext(ctx, `DELETE FROM documents WHERE source = ?`, source)

	doc := &storage.Document{
		ID:          data.NewUUID(),
		Title:       title,
		Source:      source,
		ContentType: "text/markdown",
		Metadata:    map[string]interface{}{"node_id": nodeID, "origin": "forum"},
		CreatedAt:   time.Now(),
	}

	chunks := make([]storage.Chunk, len(textChunks))
	for i, t := range textChunks {
		chunks[i] = storage.Chunk{
			ID:         data.NewUUID(),
			DocumentID: doc.ID,
			ChunkIndex: i,
			ChunkText:  t,
			WordCount:  len(strings.Fields(t)),
			CreatedAt:  time.Now(),
		}
	}

	if err := storage.SaveDocument(p.svc.db, doc, chunks); err != nil {
		return fmt.Errorf("save document: %w", err)
	}

	p.logger.Info("HorumPoller: text ingested",
		"node_id", nodeID, "chunks", len(chunks))
	return nil
}

// ingestAttachment routes an attachment to the auto_ingest workflow for processing.
// Repackages the flat content_sync payload into the "attachments" array format
// expected by HandleFetchAndIngest.
func (p *HorumPoller) ingestAttachment(ctx context.Context, m polledMention) error {
	// HandleFetchAndIngest expects payload["attachments"] as an array.
	// Content sync sends flat fields; repackage them.
	payload := make(map[string]any, len(m.Payload))
	for k, v := range m.Payload {
		payload[k] = v
	}
	attID, _ := m.Payload["attachment_id"].(string)
	if attID != "" {
		payload["attachments"] = []map[string]any{{
			"attachment_id": attID,
			"filename":      m.Payload["filename"],
			"content_type":  m.Payload["attachment_content_type"],
			"size_bytes":    m.Payload["size_bytes"],
		}}
	}

	submitReq := SubmitRequest{
		OriginMentionID: m.MentionID,
		OriginNodeID:    m.NodeID,
		OriginUserID:    m.MentionedByID,
		AgentName:       "auto_ingest",
		Payload:         payload,
		Provenance:      m.Provenance,
	}

	envelopeID, err := p.svc.CreateEnvelopeFromRequest(submitReq)
	if err != nil {
		return fmt.Errorf("create envelope: %w", err)
	}

	p.logger.Info("HorumPoller: attachment envelope created",
		"queue_id", m.MentionID,
		"envelope_id", envelopeID.String())
	return nil
}
