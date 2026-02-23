package handlers

import (
	"context"
	"fmt"
)

// HandleGenerateFAQ generates a FAQ from content using the Think LLM.
func (h *Handlers) HandleGenerateFAQ(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("generate_faq: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)
	content := ExtractEnrichedContent(payload)

	if h.GPUSubmitter == nil {
		result := map[string]interface{}{"status": "gpu_unavailable", "handler": "generate_faq"}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("generate_faq: submit next step: %w", err)
		}
		return result, nil
	}

	resp, err := h.GPUSubmitter.Generate(ctx, PromptGenerateFAQ, content, 800)
	if err != nil {
		h.GW.FailEnvelope(envelopeID, err.Error())
		return nil, fmt.Errorf("generate_faq: gpu: %w", err)
	}

	result := map[string]interface{}{
		"status":      "generated",
		"text":        resp.Text,
		"model":       resp.Model,
		"tokens_used": resp.TokensUsed,
	}
	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("generate_faq: submit next step: %w", err)
	}
	return result, nil
}
