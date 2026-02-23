package handlers

import (
	"context"
	"fmt"
)

// HandleWebSearchStub is a placeholder until SearXNG is deployed on VPS.
func (h *Handlers) HandleWebSearchStub(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("web_search: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)

	h.Logger.Info("web_search stub called (SearXNG pending)", "envelope_id", envelopeID.String())

	result := map[string]interface{}{
		"status":  "not_available",
		"handler": "web_search",
		"reason":  "searxng_pending",
	}

	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("web_search: submit next step: %w", err)
	}
	return result, nil
}

// HandleSummarizeResultsStub is a placeholder until web_search provides results.
func (h *Handlers) HandleSummarizeResultsStub(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("summarize_results: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)

	h.Logger.Info("summarize_results stub called (web_search pending)", "envelope_id", envelopeID.String())

	result := map[string]interface{}{
		"status":  "not_available",
		"handler": "summarize_results",
		"reason":  "searxng_pending",
	}

	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("summarize_results: submit next step: %w", err)
	}
	return result, nil
}
