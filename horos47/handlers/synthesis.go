package handlers

import (
	"context"
	"fmt"
)

// HandleGenerateSynthesis generates a structured summary using the Think LLM.
func (h *Handlers) HandleGenerateSynthesis(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("generate_synthesis: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)
	content := ExtractEnrichedContent(payload)

	if h.GPUSubmitter == nil {
		result := map[string]interface{}{"status": "gpu_unavailable", "handler": "generate_synthesis"}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("generate_synthesis: submit next step: %w", err)
		}
		return result, nil
	}

	resp, err := h.GPUSubmitter.Generate(ctx, PromptGenerateSynthesis, content, 800)
	if err != nil {
		h.GW.FailEnvelope(envelopeID, err.Error())
		return nil, fmt.Errorf("generate_synthesis: gpu: %w", err)
	}

	result := map[string]interface{}{
		"status":      "generated",
		"text":        resp.Text,
		"model":       resp.Model,
		"tokens_used": resp.TokensUsed,
	}
	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("generate_synthesis: submit next step: %w", err)
	}
	return result, nil
}
