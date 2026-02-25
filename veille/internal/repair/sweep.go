// CLAUDE:SUMMARY Periodic sweeper that probes broken/error sources and resets those that recover.
// CLAUDE:DEPENDS repair, store
// CLAUDE:EXPORTS Sweeper, SweepResult
package repair

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/store"
)

// SweepResult reports the outcome of probing one source.
type SweepResult struct {
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name"`
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Recovered  bool   `json:"recovered"`
	Error      string `json:"error,omitempty"`
}

// PoolResolver abstracts usertenant shard resolution.
type PoolResolver interface {
	Resolve(ctx context.Context, dossierID string) (*sql.DB, error)
}

// ShardLister returns active dossier IDs.
type ShardLister func(ctx context.Context) ([]string, error)

// Sweeper periodically probes broken sources and resets those that recover.
type Sweeper struct {
	pool     PoolResolver
	list     ShardLister
	logger   *slog.Logger
	interval time.Duration
	timeout  time.Duration // per-probe timeout
}

// NewSweeper creates a Sweeper.
func NewSweeper(pool PoolResolver, list ShardLister, logger *slog.Logger, interval time.Duration) *Sweeper {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	return &Sweeper{
		pool:     pool,
		list:     list,
		logger:   logger,
		interval: interval,
		timeout:  10 * time.Second,
	}
}

// Run launches the periodic sweep. Blocks until ctx.Done().
func (sw *Sweeper) Run(ctx context.Context) {
	sw.logger.Info("sweeper: started", "interval", sw.interval)
	ticker := time.NewTicker(sw.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			sw.logger.Info("sweeper: stopped")
			return
		case <-ticker.C:
			results := sw.SweepOnce(ctx)
			recovered := 0
			for _, r := range results {
				if r.Recovered {
					recovered++
				}
			}
			if len(results) > 0 {
				sw.logger.Info("sweeper: cycle done", "probed", len(results), "recovered", recovered)
			}
		}
	}
}

// SweepOnce probes all broken/error sources across all shards.
// Returns results for sources that were probed.
func (sw *Sweeper) SweepOnce(ctx context.Context) []SweepResult {
	dossierIDs, err := sw.list(ctx)
	if err != nil {
		sw.logger.Warn("sweeper: list shards", "error", err)
		return nil
	}

	var results []SweepResult
	for _, dossierID := range dossierIDs {
		shardResults := sw.sweepShard(ctx, dossierID)
		results = append(results, shardResults...)
	}
	return results
}

func (sw *Sweeper) sweepShard(ctx context.Context, dossierID string) []SweepResult {
	db, err := sw.pool.Resolve(ctx, dossierID)
	if err != nil {
		sw.logger.Warn("sweeper: resolve shard", "dossier_id", dossierID, "error", err)
		return nil
	}
	st := store.NewStore(db)

	broken, err := st.ListBrokenSources(ctx)
	if err != nil {
		sw.logger.Warn("sweeper: list broken", "dossier_id", dossierID, "error", err)
		return nil
	}

	var results []SweepResult
	for _, src := range broken {
		// Skip non-HTTP sources (question://, file paths).
		if src.SourceType == "question" || src.SourceType == "document" {
			continue
		}

		r := sw.probeSource(ctx, st, src)
		results = append(results, r)
	}
	return results
}

func (sw *Sweeper) probeSource(ctx context.Context, st *store.Store, src *store.Source) SweepResult {
	result := SweepResult{
		SourceID:   src.ID,
		SourceName: src.Name,
		URL:        src.URL,
	}

	code, err := ProbeURL(ctx, src.URL, sw.timeout)
	result.StatusCode = code

	if err != nil {
		result.Error = err.Error()
		return result
	}

	// Successful probe — source has recovered.
	if code >= 200 && code < 400 {
		if err := st.ResetSource(ctx, src.ID); err != nil {
			sw.logger.Warn("sweeper: reset source", "source_id", src.ID, "error", err)
			result.Error = "reset failed: " + err.Error()
			return result
		}
		result.Recovered = true
		sw.logger.Info("sweeper: source recovered", "source_id", src.ID, "name", src.Name)
		return result
	}

	// Redirect — update URL and reset.
	if code == 301 || code == 302 || code == 307 || code == 308 {
		// ProbeURL follows redirects, so if we get here it was a redirect loop.
		result.Error = "redirect loop"
		return result
	}

	result.Error = "still failing"
	return result
}
