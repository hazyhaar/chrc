package handlers

import (
	"context"
	"fmt"
)

// HandleAnalyzeQuality analyzes content quality using the Think LLM.
func (h *Handlers) HandleAnalyzeQuality(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("analyze_quality: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)
	content := ExtractEnrichedContent(payload)

	if h.GPUSubmitter == nil {
		result := map[string]interface{}{"status": "gpu_unavailable", "handler": "analyze_quality"}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("analyze_quality: submit next step: %w", err)
		}
		return result, nil
	}

	resp, err := h.GPUSubmitter.Generate(ctx, PromptAnalyzeQuality, content, 600)
	if err != nil {
		h.GW.FailEnvelope(envelopeID, err.Error())
		return nil, fmt.Errorf("analyze_quality: gpu: %w", err)
	}

	result := map[string]interface{}{
		"status":      "generated",
		"text":        resp.Text,
		"model":       resp.Model,
		"tokens_used": resp.TokensUsed,
	}
	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("analyze_quality: submit next step: %w", err)
	}
	return result, nil
}
