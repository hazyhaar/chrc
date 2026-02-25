// CLAUDE:SUMMARY Repairer applies auto-repair actions (redirect, backoff, UA rotation, mark broken) after fetch errors.
// CLAUDE:DEPENDS repair/classify, store, fetch
// CLAUDE:EXPORTS Repairer, TryRepair
package repair

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/store"
)

// MaxBackoffMs is the maximum fetch interval during backoff (24h).
const MaxBackoffMs int64 = 86400000

// alternateUserAgents is a list of common browser User-Agents for rotation.
var alternateUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0",
}

// Repairer attempts auto-repair of fetch errors.
type Repairer struct {
	logger *slog.Logger
}

// NewRepairer creates a Repairer.
func NewRepairer(logger *slog.Logger) *Repairer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Repairer{logger: logger}
}

// TryRepair attempts to auto-repair a source after a fetch failure.
// Returns the action taken (ActionNone if no repair was possible).
func (rep *Repairer) TryRepair(ctx context.Context, st *store.Store, src *store.Source, statusCode int, fetchErr error) Action {
	errMsg := ""
	if fetchErr != nil {
		errMsg = fetchErr.Error()
	}

	cls, action := Classify(src.SourceType, statusCode, errMsg)
	log := rep.logger.With("source_id", src.ID, "class", cls, "action", action)

	switch action {
	case ActionFollowRedirect:
		newURL := extractRedirectURL(statusCode, errMsg)
		if newURL != "" {
			if err := st.UpdateSourceURL(ctx, src.ID, newURL); err != nil {
				log.Warn("repair: failed to update URL", "error", err)
				return ActionNone
			}
			log.Info("repair: followed redirect", "old_url", src.URL, "new_url", newURL)
			return ActionFollowRedirect
		}
		log.Debug("repair: redirect but no Location URL found")
		return ActionNone

	case ActionBackoff:
		if err := st.SetSourceBackoff(ctx, src.ID, MaxBackoffMs); err != nil {
			log.Warn("repair: failed to set backoff", "error", err)
			return ActionNone
		}
		log.Info("repair: applied backoff", "source", src.Name)
		return ActionBackoff

	case ActionIncreaseRate:
		// Double the rate limit (applies to search engines via source config).
		if err := st.SetSourceBackoff(ctx, src.ID, MaxBackoffMs); err != nil {
			log.Warn("repair: failed to increase rate", "error", err)
			return ActionNone
		}
		log.Info("repair: increased rate limit", "source", src.Name)
		return ActionIncreaseRate

	case ActionRotateUA:
		newUA := pickAlternateUA(src.ConfigJSON)
		if newUA == "" {
			// All UAs exhausted, mark broken.
			st.SetSourceStatus(ctx, src.ID, "broken")
			log.Info("repair: all UAs exhausted, marked broken", "source", src.Name)
			return ActionMarkBroken
		}
		if err := setConfigUA(ctx, st, src.ID, src.ConfigJSON, newUA); err != nil {
			log.Warn("repair: failed to set UA", "error", err)
			return ActionNone
		}
		log.Info("repair: rotated user-agent", "source", src.Name, "ua", newUA[:40])
		return ActionRotateUA

	case ActionMarkBroken:
		if err := st.SetSourceStatus(ctx, src.ID, "broken"); err != nil {
			log.Warn("repair: failed to mark broken", "error", err)
			return ActionNone
		}
		log.Info("repair: marked broken", "source", src.Name, "reason", cls)
		return ActionMarkBroken

	default:
		return ActionNone
	}
}

// ProbeURL performs a lightweight HEAD request to check if a URL is reachable.
// Returns the HTTP status code (0 on network error) and any error.
func ProbeURL(ctx context.Context, url string, timeout time.Duration) (int, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", "chrc-veille-probe/1.0")

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// extractRedirectURL attempts to get a redirect URL from the error message.
// In practice, the HTTP client follows redirects and the fetcher gets the final URL.
// This is a fallback for when redirects are not followed automatically.
func extractRedirectURL(_ int, _ string) string {
	// The fetcher doesn't expose Location headers currently.
	// For 301/302 with redirect following disabled, the Location would be in the response.
	// This is a placeholder — the sweep probe will handle actual redirect following.
	return ""
}

// pickAlternateUA returns a UA not already tried (tracked in config_json).
func pickAlternateUA(configJSON string) string {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		cfg = map[string]any{}
	}

	currentUA, _ := cfg["user_agent"].(string)
	triedRaw, _ := cfg["tried_uas"].([]any)
	tried := make(map[string]bool, len(triedRaw))
	for _, v := range triedRaw {
		if s, ok := v.(string); ok {
			tried[s] = true
		}
	}
	if currentUA != "" {
		tried[currentUA] = true
	}

	for _, ua := range alternateUserAgents {
		if !tried[ua] {
			return ua
		}
	}
	return "" // all exhausted
}

// setConfigUA updates config_json with a new user_agent and appends to tried_uas.
func setConfigUA(ctx context.Context, st *store.Store, sourceID, configJSON, newUA string) error {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		cfg = map[string]any{}
	}

	// Track previously tried UAs.
	currentUA, _ := cfg["user_agent"].(string)
	triedRaw, _ := cfg["tried_uas"].([]any)
	if currentUA != "" {
		triedRaw = append(triedRaw, currentUA)
	}
	cfg["tried_uas"] = triedRaw
	cfg["user_agent"] = newUA

	updated, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return st.UpdateSourceConfig(ctx, sourceID, string(updated))
}
