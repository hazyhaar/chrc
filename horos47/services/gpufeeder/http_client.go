package gpufeeder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// VLLMHTTPClient client HTTP pour communiquer avec serveurs vLLM persistants
type VLLMHTTPClient struct {
	client *http.Client
	logger *slog.Logger
}

// NewVLLMHTTPClient crée nouveau client HTTP vLLM
func NewVLLMHTTPClient(logger *slog.Logger) *VLLMHTTPClient {
	return &VLLMHTTPClient{
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		logger: logger,
	}
}

// VLLMRequest représente requête OpenAI Chat Completions
type VLLMRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float32       `json:"temperature"`
}

// ChatMessage représente message avec contenu texte/image
type ChatMessage struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// VLLMResponse représente réponse OpenAI format
type VLLMResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []VLLMChoice `json:"choices"`
	Usage   VLLMUsage    `json:"usage"`
}

// VLLMChoice représente choix de réponse
type VLLMChoice struct {
	Index        int         `json:"index"`
	Message      VLLMMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// VLLMMessage représente message de réponse
type VLLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// VLLMUsage représente usage tokens
type VLLMUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// SendRequest envoie requête HTTP POST vers serveur vLLM
func (c *VLLMHTTPClient) SendRequest(ctx context.Context, serverURL string, req VLLMRequest) (*VLLMResponse, error) {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", serverURL+"/v1/chat/completions", bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	c.logger.Debug("Sending vLLM HTTP request",
		"url", serverURL,
		"payload_size", len(reqJSON))

	startTime := time.Now()
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Error("vLLM HTTP error",
			"status", resp.StatusCode,
			"body", string(body),
			"duration", duration)
		return nil, fmt.Errorf("vLLM returned status %d: %s", resp.StatusCode, string(body))
	}

	var vllmResp VLLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&vllmResp); err != nil {
		return nil, fmt.Errorf("decode vLLM response: %w", err)
	}

	c.logger.Debug("vLLM response received",
		"duration", duration,
		"tokens", vllmResp.Usage.TotalTokens,
		"finish_reason", vllmResp.Choices[0].FinishReason)

	return &vllmResp, nil
}

// EmbeddingRequest represents an OpenAI embeddings API request.
type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// EmbeddingResponse represents an OpenAI embeddings API response.
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  EmbeddingUsage  `json:"usage"`
}

// EmbeddingData holds a single embedding result.
type EmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// EmbeddingUsage represents token usage for embeddings.
type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// SendEmbeddingRequest sends an embedding request to the vLLM /v1/embeddings endpoint.
func (c *VLLMHTTPClient) SendEmbeddingRequest(ctx context.Context, serverURL string, req EmbeddingRequest) (*EmbeddingResponse, error) {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", serverURL+"/v1/embeddings", bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("create embedding http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	c.logger.Debug("Sending embedding request",
		"url", serverURL,
		"input_count", len(req.Input))

	startTime := time.Now()
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embedding http request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Error("Embedding HTTP error",
			"status", resp.StatusCode,
			"body", string(body),
			"duration", duration)
		return nil, fmt.Errorf("embedding server returned status %d: %s", resp.StatusCode, string(body))
	}

	var embedResp EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	c.logger.Debug("Embedding response received",
		"duration", duration,
		"embeddings", len(embedResp.Data),
		"tokens", embedResp.Usage.TotalTokens)

	return &embedResp, nil
}
