package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"horos47/core/data"
	"horos47/core/jobs"
	"horos47/services/gpufeeder"
)

// Handlers holds shared dependencies for all job handlers.
type Handlers struct {
	DB             *sql.DB
	Logger         *slog.Logger
	Queue          *jobs.Queue
	GW             EnvelopeManager
	GPUSubmitter   *gpufeeder.GPUSubmitter // Vision + Think jobs (gpu_jobs table)
	HTTPClient     *http.Client            // TCP HTTP/2 client (polling, lightweight requests)
	H3Client       *http.Client            // QUIC HTTP/3 client (large file downloads)
}

// EnvelopeManager provides envelope lifecycle operations.
// Implemented by gateway.Service.
type EnvelopeManager interface {
	SubmitNextStep(envelopeID data.UUID, result map[string]interface{}, chain string) error
	CompleteEnvelope(envelopeID data.UUID, resultJSON string) error
	FailEnvelope(envelopeID data.UUID, errMsg string) error
	DispatchResult(ctx context.Context, envelopeID data.UUID) error
	GetConfigParam(name string) (string, error)
}

// --- Payload utility functions ---

// EnvelopeIDFromPayload extracts envelope_id from a job payload.
func EnvelopeIDFromPayload(payload map[string]interface{}) (data.UUID, error) {
	idStr, ok := payload["envelope_id"].(string)
	if !ok {
		return data.UUID{}, fmt.Errorf("missing envelope_id in payload")
	}
	return data.ParseUUID(idStr)
}

// WorkflowChainFromPayload extracts _workflow.chain from a job payload.
func WorkflowChainFromPayload(payload map[string]interface{}) string {
	chain, _ := payload["_workflow.chain"].(string)
	return chain
}

// ExtractContent gets the message content from a job payload.
func ExtractContent(payload map[string]interface{}) string {
	if content, ok := payload["content"].(string); ok {
		return content
	}
	if payloadStr, ok := payload["payload"].(string); ok {
		var nested map[string]interface{}
		if err := json.Unmarshal([]byte(payloadStr), &nested); err == nil {
			if content, ok := nested["content"].(string); ok {
				return content
			}
			if content, ok := nested["text"].(string); ok {
				return content
			}
			if content, ok := nested["message"].(string); ok {
				return content
			}
		}
	}
	return ""
}

// ExtractEnrichedContent gets content with enrichment from previous steps.
// Falls back to ExtractContent if no enriched content is available.
func ExtractEnrichedContent(payload map[string]interface{}) string {
	prev := ExtractPreviousResult(payload)
	if prev != nil {
		if ec, ok := prev["enriched_content"].(string); ok && ec != "" {
			return ec
		}
	}
	return ExtractContent(payload)
}

// ExtractPreviousResult parses previous_result from a job payload.
func ExtractPreviousResult(payload map[string]interface{}) map[string]interface{} {
	if prev, ok := payload["previous_result"].(string); ok {
		var prevMap map[string]interface{}
		if json.Unmarshal([]byte(prev), &prevMap) == nil {
			return prevMap
		}
	}
	if prev, ok := payload["previous_result"].(map[string]interface{}); ok {
		return prev
	}
	return nil
}
