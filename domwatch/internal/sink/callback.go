// CLAUDE:SUMMARY In-process callback sink delivering mutations via Go function calls with zero serialization.
package sink

import (
	"context"

	"github.com/hazyhaar/chrc/domwatch/mutation"
)

// BatchFunc is called for each batch (in-process, zero serialisation).
type BatchFunc func(ctx context.Context, batch mutation.Batch) error

// SnapshotFunc is called for each snapshot.
type SnapshotFunc func(ctx context.Context, snap mutation.Snapshot) error

// ProfileFunc is called for each profile.
type ProfileFunc func(ctx context.Context, prof mutation.Profile) error

// Callback delivers mutations via Go function calls. This is the
// connectivity "local" path — when domkeeper and domwatch live in the
// same binary, batches are delivered as in-memory function calls with
// zero serialisation overhead.
type Callback struct {
	onBatch    BatchFunc
	onSnapshot SnapshotFunc
	onProfile  ProfileFunc
}

// NewCallback creates a Callback sink. Any handler may be nil.
func NewCallback(onBatch BatchFunc, onSnapshot SnapshotFunc, onProfile ProfileFunc) *Callback {
	return &Callback{
		onBatch:    onBatch,
		onSnapshot: onSnapshot,
		onProfile:  onProfile,
	}
}

func (c *Callback) Send(ctx context.Context, batch mutation.Batch) error {
	if c.onBatch != nil {
		return c.onBatch(ctx, batch)
	}
	return nil
}

func (c *Callback) SendSnapshot(ctx context.Context, snap mutation.Snapshot) error {
	if c.onSnapshot != nil {
		return c.onSnapshot(ctx, snap)
	}
	return nil
}

func (c *Callback) SendProfile(ctx context.Context, prof mutation.Profile) error {
	if c.onProfile != nil {
		return c.onProfile(ctx, prof)
	}
	return nil
}

func (c *Callback) Close() error { return nil }
