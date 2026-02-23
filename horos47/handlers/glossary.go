package handlers

import (
	"context"
	"fmt"
)

// HandleExtractGlossary extracts technical terms and definitions using the Think LLM.
func (h *Handlers) HandleExtractGlossary(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("extract_glossary: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)
	content := ExtractEnrichedContent(payload)

	if h.GPUSubmitter == nil {
		result := map[string]interface{}{"status": "gpu_unavailable", "handler": "extract_glossary"}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("extract_glossary: submit next step: %w", err)
		}
		return result, nil
	}

	resp, err := h.GPUSubmitter.Generate(ctx, PromptExtractGlossary, content, 600)
	if err != nil {
		h.GW.FailEnvelope(envelopeID, err.Error())
		return nil, fmt.Errorf("extract_glossary: gpu: %w", err)
	}

	result := map[string]interface{}{
		"status":      "generated",
		"text":        resp.Text,
		"model":       resp.Model,
		"tokens_used": resp.TokensUsed,
	}
	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("extract_glossary: submit next step: %w", err)
	}
	return result, nil
}
