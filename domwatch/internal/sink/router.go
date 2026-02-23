package sink

import (
	"context"
	"log/slog"

	"github.com/hazyhaar/chrc/domwatch/mutation"
)

// Router fans out mutations to all configured sinks. One sink error
// does not block the others — errors are logged and the first
// encountered is returned.
type Router struct {
	sinks  []Sink
	logger *slog.Logger
}

// NewRouter creates a fan-out router delivering to all sinks.
func NewRouter(logger *slog.Logger, sinks ...Sink) *Router {
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{sinks: sinks, logger: logger}
}

func (r *Router) Send(ctx context.Context, batch mutation.Batch) error {
	var firstErr error
	for _, s := range r.sinks {
		if err := s.Send(ctx, batch); err != nil {
			r.logger.Warn("sink: send batch failed", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (r *Router) SendSnapshot(ctx context.Context, snap mutation.Snapshot) error {
	var firstErr error
	for _, s := range r.sinks {
		if err := s.SendSnapshot(ctx, snap); err != nil {
			r.logger.Warn("sink: send snapshot failed", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (r *Router) SendProfile(ctx context.Context, prof mutation.Profile) error {
	var firstErr error
	for _, s := range r.sinks {
		if err := s.SendProfile(ctx, prof); err != nil {
			r.logger.Warn("sink: send profile failed", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (r *Router) Close() error {
	var firstErr error
	for _, s := range r.sinks {
		if err := s.Close(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
