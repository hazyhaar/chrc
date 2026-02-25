// CLAUDE:SUMMARY Connectivity bridge handler dispatching source fetches to external services via router.Call().
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hazyhaar/chrc/extract"
	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/store"
	"github.com/hazyhaar/pkg/connectivity"
)

// ConnectivityBridge implements SourceHandler by delegating to a connectivity.Router service.
//
// Convention: a service registered as "{type}_fetch" handles source_type="{type}".
// The bridge serializes the source, calls the remote service, and processes the extractions.
type ConnectivityBridge struct {
	router     *connectivity.Router
	service    string // e.g. "socmed_fetch"
	sourceType string // e.g. "socmed"
}

// NewConnectivityBridge creates a bridge for the given service name.
func NewConnectivityBridge(router *connectivity.Router, service, sourceType string) *ConnectivityBridge {
	return &ConnectivityBridge{
		router:     router,
		service:    service,
		sourceType: sourceType,
	}
}

// bridgeRequest is the payload sent to the remote service.
type bridgeRequest struct {
	SourceID   string          `json:"source_id"`
	URL        string          `json:"url"`
	Config     json.RawMessage `json:"config"`
	SourceType string          `json:"source_type"`
}

// bridgeResponse is the expected response from the remote service.
type bridgeResponse struct {
	Extractions []bridgeExtraction `json:"extractions"`
}

// bridgeExtraction is one extracted item from the remote service.
type bridgeExtraction struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	URL         string `json:"url"`
	ContentHash string `json:"content_hash"`
}

// Handle calls the remote service via the connectivity router, deduplicates,
// stores extractions and chunks, and optionally writes to the buffer.
func (b *ConnectivityBridge) Handle(ctx context.Context, s *store.Store, src *store.Source, p *Pipeline) error {
	log := p.logger.With("source_id", src.ID, "handler", "connectivity", "service", b.service)
	start := time.Now()

	// Build request payload.
	req := bridgeRequest{
		SourceID:   src.ID,
		URL:        src.URL,
		Config:     json.RawMessage(src.ConfigJSON),
		SourceType: src.SourceType,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("connectivity bridge: marshal: %w", err)
	}

	logEntry := &store.FetchLogEntry{
		ID:        p.newID(),
		SourceID:  src.ID,
		FetchedAt: time.Now().UnixMilli(),
	}

	// Call the remote service.
	respData, err := b.router.Call(ctx, b.service, payload)
	duration := time.Since(start).Milliseconds()
	logEntry.DurationMs = duration

	if err != nil {
		logEntry.Status = "error"
		logEntry.ErrorMessage = err.Error()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchError(ctx, src.ID, err.Error())
		log.Warn("connectivity: call failed", "error", err)
		return fmt.Errorf("connectivity call %s: %w", b.service, err)
	}

	// Parse response.
	var resp bridgeResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		logEntry.Status = "extract_error"
		logEntry.ErrorMessage = "parse response: " + err.Error()
		s.InsertFetchLog(ctx, logEntry)
		s.RecordFetchError(ctx, src.ID, logEntry.ErrorMessage)
		return fmt.Errorf("connectivity bridge: parse response: %w", err)
	}

	// Process extractions.
	var newCount int
	for _, ext := range resp.Extractions {
		contentHash := ext.ContentHash
		if contentHash == "" {
			contentHash = bridgeHash(ext.URL + "|" + ext.Title)
		}

		// Dedup.
		exists, err := s.ExtractionExists(ctx, src.ID, contentHash)
		if err != nil {
			log.Warn("connectivity: dedup check failed", "error", err)
			continue
		}
		if exists {
			continue
		}

		text := extract.CleanText(ext.Content)
		if text == "" {
			continue
		}

		now := time.Now().UnixMilli()
		extractionID := p.newID()

		url := ext.URL
		if url == "" {
			url = src.URL
		}

		extraction := &store.Extraction{
			ID:            extractionID,
			SourceID:      src.ID,
			ContentHash:   contentHash,
			Title:         ext.Title,
			ExtractedText: text,
			URL:           url,
			ExtractedAt:   now,
		}
		if err := s.InsertExtraction(ctx, extraction); err != nil {
			log.Warn("connectivity: insert extraction failed", "error", err)
			continue
		}

		// Buffer write.
		if p.buffer != nil && p.currentJob != nil {
			meta := buffer.Metadata{
				ID:          extractionID,
				SourceID:    src.ID,
				DossierID:   p.currentJob.DossierID,
				SourceURL:   url,
				SourceType:  b.sourceType,
				Title:       ext.Title,
				ContentHash: contentHash,
				ExtractedAt: time.Now().UTC(),
			}
			if _, err := p.buffer.Write(ctx, meta, text); err != nil {
				log.Warn("connectivity: buffer write failed", "error", err)
			}
		}

		newCount++
	}

	logEntry.Status = "ok"
	s.InsertFetchLog(ctx, logEntry)
	s.RecordFetchSuccess(ctx, src.ID, "")

	log.Info("connectivity: processed", "service", b.service, "new", newCount, "duration_ms", duration)
	return nil
}

// DiscoverHandlers scans the connectivity router for services matching the
// convention "{type}_fetch" and registers a ConnectivityBridge handler for each
// type not already covered by a built-in handler.
func DiscoverHandlers(p *Pipeline, router *connectivity.Router) {
	if router == nil {
		return
	}

	for si := range router.ListServices() {
		if !strings.HasSuffix(si.Name, "_fetch") {
			continue
		}
		sourceType := strings.TrimSuffix(si.Name, "_fetch")

		// Skip if a built-in handler already exists.
		if _, exists := p.handlers[sourceType]; exists {
			continue
		}

		bridge := NewConnectivityBridge(router, si.Name, sourceType)
		p.handlers[sourceType] = bridge
		p.logger.Info("connectivity: discovered handler",
			"service", si.Name, "source_type", sourceType)
	}
}

func bridgeHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
