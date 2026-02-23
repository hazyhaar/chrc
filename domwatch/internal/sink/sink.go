// Package sink defines output backends for domwatch mutations.
package sink

import (
	"context"

	"github.com/hazyhaar/pkg/domwatch/mutation"
)

// Sink is the output interface. Implementations deliver mutations to
// different backends (stdout, webhook, NATS, in-process callback).
type Sink interface {
	Send(ctx context.Context, batch mutation.Batch) error
	SendSnapshot(ctx context.Context, snap mutation.Snapshot) error
	SendProfile(ctx context.Context, prof mutation.Profile) error
	Close() error
}
