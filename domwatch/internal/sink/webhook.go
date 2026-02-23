package sink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/hazyhaar/pkg/domwatch/mutation"
)

// Webhook POSTs JSON to a URL with retry and exponential backoff.
type Webhook struct {
	url        string
	client     *http.Client
	maxRetries int
	logger     *slog.Logger
}

// WebhookOption configures a Webhook sink.
type WebhookOption func(*Webhook)

// WithWebhookRetries sets the maximum number of retries. Default: 3.
func WithWebhookRetries(n int) WebhookOption {
	return func(w *Webhook) { w.maxRetries = n }
}

// WithWebhookLogger sets a custom logger.
func WithWebhookLogger(l *slog.Logger) WebhookOption {
	return func(w *Webhook) { w.logger = l }
}

// NewWebhook creates a Webhook sink targeting the given URL.
func NewWebhook(url string, opts ...WebhookOption) *Webhook {
	w := &Webhook{
		url:        url,
		client:     &http.Client{Timeout: 10 * time.Second},
		maxRetries: 3,
		logger:     slog.Default(),
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

func (w *Webhook) Send(ctx context.Context, batch mutation.Batch) error {
	return w.post(ctx, "batch", batch)
}

func (w *Webhook) SendSnapshot(ctx context.Context, snap mutation.Snapshot) error {
	return w.post(ctx, "snapshot", snap)
}

func (w *Webhook) SendProfile(ctx context.Context, prof mutation.Profile) error {
	return w.post(ctx, "profile", prof)
}

func (w *Webhook) Close() error { return nil }

func (w *Webhook) post(ctx context.Context, typ string, data any) error {
	body, err := json.Marshal(envelope{Type: typ, Data: data})
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("webhook: new request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := w.client.Do(req)
		if err != nil {
			lastErr = err
			w.logger.Warn("webhook: request failed", "attempt", attempt+1, "error", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("webhook: status %d", resp.StatusCode)
		w.logger.Warn("webhook: bad status", "attempt", attempt+1, "status", resp.StatusCode)
	}
	return fmt.Errorf("webhook: all retries exhausted: %w", lastErr)
}
