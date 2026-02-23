package domkeeper

import (
	"github.com/hazyhaar/pkg/domwatch"
)

// Sink returns a domwatch.Sink that feeds into this Keeper's ingestion pipeline.
// Wire this into domwatch.New() to connect observation → extraction.
//
// Usage:
//
//	k, _ := domkeeper.New(cfg, logger)
//	sink := k.Sink()
//	w := domwatch.New(dwCfg, logger, sink)
func (k *Keeper) Sink() domwatch.Sink {
	return domwatch.NewCallbackSink(
		k.HandleBatch,
		k.HandleSnapshot,
		k.HandleProfile,
	)
}
