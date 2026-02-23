package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

// HandleGenerateAnswer generates an answer using the Think LLM via GPU Feeder V3.
// The previous step (rag_retrieve) provides context chunks in previous_result.results.
func (h *Handlers) HandleGenerateAnswer(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	envelopeID, err := EnvelopeIDFromPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("generate_answer: %w", err)
	}
	chain := WorkflowChainFromPayload(payload)
	content := ExtractEnrichedContent(payload)

	if h.GPUSubmitter == nil {
		result := map[string]interface{}{"status": "gpu_unavailable", "handler": "generate_answer"}
		if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
			return nil, fmt.Errorf("generate_answer: submit next step: %w", err)
		}
		return result, nil
	}

	// Build user prompt with RAG context from previous step
	userPrompt := content
	prevResult := ExtractPreviousResult(payload)
	if prevResult != nil {
		if results, ok := prevResult["results"].([]interface{}); ok && len(results) > 0 {
			var ragContext strings.Builder
			for _, r := range results {
				if m, ok := r.(map[string]interface{}); ok {
					if chunkText, ok := m["chunk_text"].(string); ok {
						ragContext.WriteString(chunkText)
						ragContext.WriteString("\n---\n")
					}
				}
			}
			if ragContext.Len() > 0 {
				userPrompt = fmt.Sprintf("Contexte:\n%s\nQuestion: %s", ragContext.String(), content)
			}
		}
	}

	resp, err := h.GPUSubmitter.Generate(ctx, PromptGenerateAnswer, userPrompt, 800)
	if err != nil {
		h.GW.FailEnvelope(envelopeID, err.Error())
		return nil, fmt.Errorf("generate_answer: gpu: %w", err)
	}

	// Strip <think>...</think> blocks from Qwen3 reasoning output
	cleanText := strings.TrimSpace(thinkRe.ReplaceAllString(resp.Text, ""))
	if cleanText == "" {
		cleanText = resp.Text // fallback: keep raw if stripping removed everything
	}

	result := map[string]interface{}{
		"status":      "generated",
		"text":        cleanText,
		"model":       resp.Model,
		"tokens_used": resp.TokensUsed,
	}
	if err := h.GW.SubmitNextStep(envelopeID, result, chain); err != nil {
		return nil, fmt.Errorf("generate_answer: submit next step: %w", err)
	}
	return result, nil
}
