// CLAUDE:SUMMARY Defines the SourceHandler interface for source-type-specific pipeline handlers.
package pipeline

import (
	"context"

	"github.com/hazyhaar/chrc/veille/internal/store"
)

// SourceHandler handles the fetch-extract-store cycle for a specific source type.
type SourceHandler interface {
	Handle(ctx context.Context, s *store.Store, src *store.Source, p *Pipeline) error
}
