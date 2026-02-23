package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"horos47/core/data"
	"horos47/services/gateway"
)

// HandleClarifyIntent is the first step (step 0) of ALL agent workflows.
// It detects uncertainties and either generates clarification questions or passes through.
func (h *Handlers) HandleClarifyIntent(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("clarify_intent: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)

	content := ExtractContent(payload)
	if content == "" {
		return h.passThroughClarify(envelopeID, payload, chain)
	}

	// Check if this is a re-entry after clarification answers
	if answersJSON, ok := payload["clarification_answers"].(string); ok && answersJSON != "" {
		enriched := content + "\n\n[Clarification utilisateur: " + answersJSON + "]"
		result := map[string]interface{}{
			"intent":             "clarified",
			"original_content":   content,
			"enriched_content":   enriched,
			"clarification_used": true,
		}
		return h.submitNextAndReturn(envelopeID, result, chain)
	}

	// Deterministic uncertainty detection
	detector := &gateway.UncertaintyDetector{}
	needsClarification, uncertainties := detector.Analyze(content)

	if !needsClarification || len(uncertainties) == 0 {
		return h.passThroughClarify(envelopeID, payload, chain)
	}

	questions := gateway.GenerateQuestions(uncertainties)
	if len(questions) == 0 {
		return h.passThroughClarify(envelopeID, payload, chain)
	}

	// Store clarification request
	requestID := data.NewUUID()
	uncertaintiesJSON, _ := json.Marshal(uncertainties)
	questionsJSON, _ := json.Marshal(questions)
	expiresAt := time.Now().Add(24 * time.Hour).Unix()

	_, err = data.ExecWithRetry(h.DB, `
		INSERT INTO clarification_requests
			(request_id, envelope_id, detected_uncertainties, questions, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, 'pending', unixepoch(), ?)
	`, requestID, envelopeID, string(uncertaintiesJSON), string(questionsJSON), expiresAt)
	if err != nil {
		h.Logger.Error("Failed to insert clarification request", "error", err)
		return h.passThroughClarify(envelopeID, payload, chain)
	}

	_, _ = data.ExecWithRetry(h.DB, `
		UPDATE task_envelopes SET status = 'awaiting_clarification' WHERE envelope_id = ?
	`, envelopeID)

	h.Logger.Info("Clarification requested",
		"envelope_id", envelopeID.String(),
		"request_id", requestID.String(),
		"uncertainties", len(uncertainties))

	return map[string]interface{}{
		"status":     "awaiting_clarification",
		"request_id": requestID.String(),
	}, nil
}

func (h *Handlers) passThroughClarify(envelopeID data.UUID, payload map[string]interface{}, chain string) (map[string]interface{}, error) {
	content := ExtractContent(payload)
	result := map[string]interface{}{
		"intent":             "clear",
		"original_content":   content,
		"enriched_content":   content,
		"clarification_used": false,
	}
	return h.submitNextAndReturn(envelopeID, result, chain)
}

func (h *Handlers) submitNextAndReturn(envelopeID data.UUID, result map[string]interface{}, chain string) (map[string]interface{}, error) {
	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("submit next step: %w", err)
	}
	return result, nil
}
