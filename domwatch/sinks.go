package domwatch

import (
	"context"
	"io"
	"log/slog"

	"github.com/hazyhaar/pkg/domwatch/internal/sink"
	"github.com/hazyhaar/pkg/domwatch/mutation"
)

// Sink is the output interface for domwatch mutations.
type Sink = sink.Sink

// NewStdoutSink creates a stdout JSON-lines sink.
func NewStdoutSink(w io.Writer) Sink {
	return sink.NewStdout(w)
}

// NewWebhookSink creates a webhook POST sink with retry.
func NewWebhookSink(url string, logger *slog.Logger) Sink {
	return sink.NewWebhook(url, sink.WithWebhookLogger(logger))
}

// BatchFunc is called for each batch.
type BatchFunc = sink.BatchFunc

// SnapshotFunc is called for each snapshot.
type SnapshotFunc = sink.SnapshotFunc

// ProfileFunc is called for each profile.
type ProfileFunc = sink.ProfileFunc

// NewCallbackSink creates an in-process callback sink for the connectivity
// "local" path — zero serialisation.
func NewCallbackSink(
	onBatch func(ctx context.Context, batch mutation.Batch) error,
	onSnapshot func(ctx context.Context, snap mutation.Snapshot) error,
	onProfile func(ctx context.Context, prof mutation.Profile) error,
) Sink {
	return sink.NewCallback(onBatch, onSnapshot, onProfile)
}
