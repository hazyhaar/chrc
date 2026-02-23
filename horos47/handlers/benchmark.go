package handlers

import (
	"context"
	"fmt"
)

// HandleGenerateBenchmark generates evaluation questions using the Think LLM.
func (h *Handlers) HandleGenerateBenchmark(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("generate_benchmark: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)
	content := ExtractEnrichedContent(payload)

	if h.GPUSubmitter == nil {
		result := map[string]interface{}{"status": "gpu_unavailable", "handler": "generate_benchmark"}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("generate_benchmark: submit next step: %w", err)
		}
		return result, nil
	}

	resp, err := h.GPUSubmitter.Generate(ctx, PromptGenerateBenchmark, content, 800)
	if err != nil {
		h.GW.FailEnvelope(envelopeID, err.Error())
		return nil, fmt.Errorf("generate_benchmark: gpu: %w", err)
	}

	result := map[string]interface{}{
		"status":      "generated",
		"text":        resp.Text,
		"model":       resp.Model,
		"tokens_used": resp.TokensUsed,
	}
	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("generate_benchmark: submit next step: %w", err)
	}
	return result, nil
}
