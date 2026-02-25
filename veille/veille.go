// CLAUDE:SUMMARY Main Service orchestrator: multi-tenant lifecycle, pipeline dispatch, scheduler, and all business methods.
package veille

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"net/url"
	"strings"

	"github.com/hazyhaar/chrc/veille/internal/buffer"
	"github.com/hazyhaar/chrc/veille/internal/fetch"
	"github.com/hazyhaar/chrc/veille/internal/pipeline"
	"github.com/hazyhaar/chrc/veille/internal/question"
	"github.com/hazyhaar/chrc/veille/internal/repair"
	"github.com/hazyhaar/chrc/veille/internal/scheduler"
	"github.com/hazyhaar/chrc/veille/internal/search"
	"github.com/hazyhaar/chrc/veille/internal/store"
	"github.com/hazyhaar/pkg/audit"
	"github.com/hazyhaar/pkg/connectivity"
	"github.com/hazyhaar/pkg/horosafe"
	"github.com/hazyhaar/pkg/idgen"
)

// PoolResolver abstracts usertenant.Pool.Resolve for testability.
type PoolResolver interface {
	Resolve(ctx context.Context, dossierID string) (*sql.DB, error)
}

// Service is the main veille orchestrator.
type Service struct {
	pool         PoolResolver
	fetcher      *fetch.Fetcher
	pipeline     *pipeline.Pipeline
	scheduler    *scheduler.Scheduler
	repairer     *repair.Repairer
	sweeper      *repair.Sweeper
	logger       *slog.Logger
	config       *Config
	newID        func() string
	sourceTypes  map[string]bool // allowed source types (built-in + discovered)
	router       *connectivity.Router // optional — enables ConnectivityBridge discovery
	catalogDB    *sql.DB              // optional — global engine/source catalog
	audit        audit.Logger          // optional — audit trail
	urlValidator func(string) error    // URL validation (default: horosafe.ValidateURL)
}

// New creates a veille Service.
// If router is non-nil, ConnectivityBridge handlers are auto-discovered.
func New(pool PoolResolver, cfg *Config, logger *slog.Logger, opts ...ServiceOption) (*Service, error) {
	if cfg == nil {
		cfg = defaultConfig()
	}
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}

	f := fetch.New(cfg.Fetch)
	p := pipeline.New(f, logger)

	// Configure buffer if dir is set.
	var buf *buffer.Writer
	if cfg.BufferDir != "" {
		buf = buffer.NewWriter(cfg.BufferDir)
		p.SetBuffer(buf)
	}

	// Start with built-in source types.
	types := make(map[string]bool, len(allowedSourceTypes))
	for k, v := range allowedSourceTypes {
		types[k] = v
	}

	svc := &Service{
		pool:         pool,
		fetcher:      f,
		pipeline:     p,
		repairer:     repair.NewRepairer(logger),
		logger:       logger,
		config:       cfg,
		newID:        idgen.New,
		urlValidator: horosafe.ValidateURL,
		sourceTypes:  types,
	}

	// Apply options.
	for _, opt := range opts {
		opt(svc)
	}

	// Wire question handler: the runner needs store access via a closure.
	engineLookup := func(ctx context.Context, id string) (*search.Engine, error) {
		return svc.lookupSearchEngine(ctx, id)
	}
	runner := question.NewRunner(question.Config{
		Engines: engineLookup,
		Fetcher: f,
		Buffer:  buf,
		Logger:  logger,
		NewID:   idgen.New,
	})
	p.RegisterHandler("question", pipeline.NewQuestionHandler(runner))

	// Discover connectivity bridge handlers if router is set.
	if svc.router != nil {
		pipeline.DiscoverHandlers(p, svc.router)
	}

	// Sync all registered pipeline types into the validation set.
	for _, t := range p.RegisteredTypes() {
		svc.sourceTypes[t] = true
	}

	// Create scheduler with shard resolution wired to pool.
	resolve := func(ctx context.Context, dossierID string) (*sql.DB, error) {
		return pool.Resolve(ctx, dossierID)
	}
	list := func(ctx context.Context) ([]string, error) {
		return svc.listActiveShards(ctx)
	}
	sink := func(ctx context.Context, job *scheduler.Job) error {
		return svc.processJob(ctx, job)
	}
	svc.scheduler = scheduler.New(resolve, list, sink, cfg.Scheduler, logger)

	// Create sweeper for periodic probe of broken sources.
	svc.sweeper = repair.NewSweeper(pool, func(ctx context.Context) ([]string, error) {
		return svc.listActiveShards(ctx)
	}, logger, cfg.SweepInterval)

	return svc, nil
}

// ServiceOption configures a Service during creation.
type ServiceOption func(*Service)

// WithRouter sets the connectivity router for auto-discovery of external handlers.
func WithRouter(r *connectivity.Router) ServiceOption {
	return func(svc *Service) { svc.router = r }
}

// WithCatalogDB sets the global catalog database for admin-managed engines and sources.
func WithCatalogDB(db *sql.DB) ServiceOption {
	return func(svc *Service) { svc.catalogDB = db }
}

// WithAudit sets the audit logger for data-modifying operations.
func WithAudit(a audit.Logger) ServiceOption {
	return func(svc *Service) { svc.audit = a }
}

// WithURLValidator overrides the URL validation function (default: horosafe.ValidateURL).
// Use in tests with httptest servers that listen on loopback addresses.
func WithURLValidator(fn func(string) error) ServiceOption {
	return func(svc *Service) { svc.urlValidator = fn }
}

// CatalogDB returns the catalog database for admin operations.
func (svc *Service) CatalogDB() *sql.DB {
	return svc.catalogDB
}

// lookupSearchEngine loads a search.Engine from the first available shard.
// In practice, search engines are per-shard, so the caller provides a resolved store.
// This is used as a fallback when the runner doesn't have direct store access.
func (svc *Service) lookupSearchEngine(ctx context.Context, id string) (*search.Engine, error) {
	// This is a minimal implementation — the runner gets its engine lookup
	// from the store it receives, but this serves as a default.
	// In practice, the QuestionHandler re-wires this per-shard.
	return nil, fmt.Errorf("engine lookup requires shard context (engine %q)", id)
}

// Start launches the background scheduler and sweeper. Non-blocking.
func (svc *Service) Start(ctx context.Context) {
	go svc.scheduler.Run(ctx)
	if svc.sweeper != nil {
		go svc.sweeper.Run(ctx)
	}
	svc.logger.Info("veille: started")
}

// Close shuts down the service.
func (svc *Service) Close() error {
	svc.logger.Info("veille: closed")
	return nil
}

// resolveStore resolves a shard and wraps it in a Store.
func (svc *Service) resolveStore(ctx context.Context, dossierID string) (*store.Store, error) {
	db, err := svc.pool.Resolve(ctx, dossierID)
	if err != nil {
		return nil, fmt.Errorf("resolve shard: %w", err)
	}
	return store.NewStore(db), nil
}

// auditLog emits an async audit entry if an audit logger is configured.
func (svc *Service) auditLog(dossierID, action, params string) {
	if svc.audit == nil {
		return
	}
	svc.audit.LogAsync(&audit.Entry{
		Action:     action,
		UserID:     dossierID,
		Parameters: params,
	})
}

// --- Sources ---

// validateSourceURL validates the URL of a source before insert or update.
// Internal source types (question) use synthetic URLs that bypass SSRF checks.
// Document sources are validated for path traversal.
// All other sources are validated against SSRF.
func (svc *Service) validateSourceURL(s *Source) error {
	if s.URL == "" {
		return nil
	}

	// Internal source types use synthetic URLs (question://id) — skip SSRF checks.
	if s.SourceType == "question" {
		return nil
	}

	// Document sources: validate against path traversal.
	if s.SourceType == "document" {
		// Decode percent-encoded sequences before checking.
		decoded, err := url.PathUnescape(s.URL)
		if err != nil {
			return fmt.Errorf("invalid document path: %w", err)
		}
		if strings.Contains(decoded, "..") {
			return horosafe.ErrPathTraversal
		}
		return nil
	}

	// HTTP sources: validate against SSRF (private IPs, non-HTTP schemes).
	return svc.urlValidator(s.URL)
}

// AddSource adds a new monitored source to a dossier.
func (svc *Service) AddSource(ctx context.Context, dossierID string, s *Source) error {
	if s.ID == "" {
		s.ID = svc.newID()
	}

	// Apply defaults before validation (source_type and fetch_interval).
	if s.SourceType == "" {
		s.SourceType = "web"
	}
	if s.FetchInterval == 0 {
		s.FetchInterval = 3600000
	}

	// Validate input fields.
	if err := validateSourceInput(s, svc.sourceTypes); err != nil {
		return err
	}

	// Normalize URL for consistent dedup.
	normalized, err := NormalizeSourceURL(s.URL)
	if err != nil {
		return err
	}
	s.URL = normalized

	// SSRF / path traversal validation.
	if err := svc.validateSourceURL(s); err != nil {
		return err
	}

	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return err
	}

	// Quota check.
	count, err := st.CountSources(ctx)
	if err != nil {
		return fmt.Errorf("count sources: %w", err)
	}
	if count >= MaxSourcesPerSpace {
		return fmt.Errorf("%w: maximum %d sources per space", ErrQuotaExceeded, MaxSourcesPerSpace)
	}

	// Dedup check.
	existing, _ := st.GetSourceByURL(ctx, s.URL)
	if existing != nil {
		return fmt.Errorf("%w: %s", ErrDuplicateSource, s.URL)
	}

	if err := st.InsertSource(ctx, s); err != nil {
		return err
	}
	svc.auditLog(dossierID, "add_source", fmt.Sprintf(`{"dossier_id":%q,"source_id":%q,"url":%q,"type":%q}`, dossierID, s.ID, s.URL, s.SourceType))
	return nil
}

// ListSources returns all sources in a dossier.
func (svc *Service) ListSources(ctx context.Context, dossierID string) ([]*Source, error) {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return nil, err
	}
	return st.ListSources(ctx)
}

// UpdateSource updates a source's mutable fields.
func (svc *Service) UpdateSource(ctx context.Context, dossierID string, s *Source) error {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return err
	}

	// Load existing source to fill in missing fields for validation.
	existing, err := st.GetSource(ctx, s.ID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("source not found: %s", s.ID)
	}

	// Merge: use existing values for unset fields so validation passes.
	if s.Name == "" {
		s.Name = existing.Name
	}
	if s.SourceType == "" {
		s.SourceType = existing.SourceType
	}
	if s.FetchInterval == 0 {
		s.FetchInterval = existing.FetchInterval
	}
	if s.URL == "" {
		s.URL = existing.URL
	}

	// Validate merged input.
	if err := validateSourceInput(s, svc.sourceTypes); err != nil {
		return err
	}

	// Normalize URL.
	normalized, err := NormalizeSourceURL(s.URL)
	if err != nil {
		return err
	}
	s.URL = normalized

	// SSRF / path traversal validation.
	if err := svc.validateSourceURL(s); err != nil {
		return err
	}

	// Dedup check: if URL changed, ensure no other source has this URL.
	if s.URL != existing.URL {
		other, _ := st.GetSourceByURL(ctx, s.URL)
		if other != nil && other.ID != s.ID {
			return fmt.Errorf("%w: %s", ErrDuplicateSource, s.URL)
		}
	}

	if err := st.UpdateSource(ctx, s); err != nil {
		return err
	}
	svc.auditLog(dossierID, "update_source", fmt.Sprintf(`{"dossier_id":%q,"source_id":%q}`, dossierID, s.ID))
	return nil
}

// DeleteSource removes a source and all its content.
func (svc *Service) DeleteSource(ctx context.Context, dossierID, sourceID string) error {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return err
	}
	if err := st.DeleteSource(ctx, sourceID); err != nil {
		return err
	}
	svc.auditLog(dossierID, "delete_source", fmt.Sprintf(`{"dossier_id":%q,"source_id":%q}`, dossierID, sourceID))
	return nil
}

// FetchNow triggers an immediate fetch for a source.
func (svc *Service) FetchNow(ctx context.Context, dossierID, sourceID string) error {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return err
	}
	src, err := st.GetSource(ctx, sourceID)
	if err != nil {
		return err
	}
	if src == nil {
		return fmt.Errorf("source not found: %s", sourceID)
	}
	svc.auditLog(dossierID, "fetch_now", fmt.Sprintf(`{"dossier_id":%q,"source_id":%q}`, dossierID, sourceID))
	return svc.pipeline.HandleJob(ctx, st, &pipeline.Job{
		DossierID: dossierID,
		SourceID:  sourceID,
		URL:       src.URL,
	})
}

// --- Questions ---

// AddQuestion adds a tracked question and creates its backing source.
func (svc *Service) AddQuestion(ctx context.Context, dossierID string, q *TrackedQuestion) error {
	if q.ID == "" {
		q.ID = svc.newID()
	}
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return err
	}

	// Insert the question.
	if err := st.InsertQuestion(ctx, q); err != nil {
		return fmt.Errorf("insert question: %w", err)
	}

	// Create the auto-source (source_type="question", sourceID=q.ID).
	name := "Q: " + q.Text
	if len(name) > 83 {
		name = name[:80] + "..."
	}
	configJSON, _ := json.Marshal(map[string]string{"question_id": q.ID})
	src := &Source{
		ID:            q.ID,
		Name:          name,
		URL:           "question://" + q.ID,
		SourceType:    "question",
		FetchInterval: q.ScheduleMs,
		Enabled:       q.Enabled,
		ConfigJSON:    string(configJSON),
	}
	if err := st.InsertSource(ctx, src); err != nil {
		return err
	}
	svc.auditLog(dossierID, "add_question", fmt.Sprintf(`{"dossier_id":%q,"question_id":%q}`, dossierID, q.ID))
	return nil
}

// ListQuestions returns all tracked questions in a dossier.
func (svc *Service) ListQuestions(ctx context.Context, dossierID string) ([]*TrackedQuestion, error) {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return nil, err
	}
	return st.ListQuestions(ctx)
}

// UpdateQuestion updates a tracked question and syncs the backing source.
func (svc *Service) UpdateQuestion(ctx context.Context, dossierID string, q *TrackedQuestion) error {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return err
	}
	if err := st.UpdateQuestion(ctx, q); err != nil {
		return err
	}
	// Sync the auto-source.
	src, err := st.GetSource(ctx, q.ID)
	if err != nil {
		return err
	}
	if src != nil {
		src.FetchInterval = q.ScheduleMs
		src.Enabled = q.Enabled
		return st.UpdateSource(ctx, src)
	}
	return nil
}

// DeleteQuestion removes a tracked question and its backing source.
func (svc *Service) DeleteQuestion(ctx context.Context, dossierID, questionID string) error {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return err
	}
	if err := st.DeleteQuestion(ctx, questionID); err != nil {
		return err
	}
	if err := st.DeleteSource(ctx, questionID); err != nil {
		return err
	}
	svc.auditLog(dossierID, "delete_question", fmt.Sprintf(`{"dossier_id":%q,"question_id":%q}`, dossierID, questionID))
	return nil
}

// RunQuestionNow triggers an immediate question run.
func (svc *Service) RunQuestionNow(ctx context.Context, dossierID, questionID string) (int, error) {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return 0, err
	}
	q, err := st.GetQuestion(ctx, questionID)
	if err != nil {
		return 0, err
	}
	if q == nil {
		return 0, fmt.Errorf("question not found: %s", questionID)
	}

	// Build runner with global→per-shard engine lookup chain.
	engineLookup := func(ctx context.Context, id string) (*search.Engine, error) {
		// 1. Global catalog DB (admin-managed).
		if svc.catalogDB != nil {
			e, err := lookupGlobalEngine(ctx, svc.catalogDB, id)
			if err == nil && e != nil {
				return e, nil
			}
		}
		// 2. Per-shard fallback.
		se, err := st.GetSearchEngine(ctx, id)
		if err != nil {
			return nil, err
		}
		if se == nil {
			return nil, nil
		}
		return storeEngineToSearch(se), nil
	}

	var buf *buffer.Writer
	if svc.config.BufferDir != "" {
		buf = buffer.NewWriter(svc.config.BufferDir)
	}

	runner := question.NewRunner(question.Config{
		Engines: engineLookup,
		Fetcher: svc.fetcher,
		Buffer:  buf,
		Logger:  svc.logger,
		NewID:   idgen.New,
	})
	return runner.Run(ctx, st, q, dossierID)
}

// QuestionResults returns extractions for a question (sourceID = questionID).
func (svc *Service) QuestionResults(ctx context.Context, dossierID, questionID string, limit int) ([]*Extraction, error) {
	return svc.ListExtractions(ctx, dossierID, questionID, limit)
}

// storeEngineToSearch converts a store.SearchEngine to a search.Engine.
func storeEngineToSearch(se *store.SearchEngine) *search.Engine {
	e := &search.Engine{
		ID:           se.ID,
		Name:         se.Name,
		Strategy:     se.Strategy,
		URLTemplate:  se.URLTemplate,
		StealthLevel: se.StealthLevel,
		RateLimitMs:  se.RateLimitMs,
		MaxPages:     se.MaxPages,
		Enabled:      se.Enabled,
		CreatedAt:    se.CreatedAt,
		UpdatedAt:    se.UpdatedAt,
	}
	if se.APIConfigJSON != "" && se.APIConfigJSON != "{}" {
		json.Unmarshal([]byte(se.APIConfigJSON), &e.APIConfig)
	}
	if se.SelectorsJSON != "" && se.SelectorsJSON != "{}" {
		json.Unmarshal([]byte(se.SelectorsJSON), &e.Selectors)
	}
	return e
}

// --- Read operations ---

// Search performs FTS5 search on extractions.
func (svc *Service) Search(ctx context.Context, dossierID, query string, limit int) ([]*SearchResult, error) {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return nil, err
	}
	return st.Search(ctx, query, limit)
}

// ListExtractions returns extractions for a source.
func (svc *Service) ListExtractions(ctx context.Context, dossierID, sourceID string, limit int) ([]*Extraction, error) {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return nil, err
	}
	return st.ListExtractions(ctx, sourceID, limit)
}

// Stats returns aggregate counters for a dossier.
func (svc *Service) Stats(ctx context.Context, dossierID string) (*SpaceStats, error) {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return nil, err
	}
	return st.Stats(ctx)
}

// FetchHistory returns fetch log entries for a source.
func (svc *Service) FetchHistory(ctx context.Context, dossierID, sourceID string, limit int) ([]*FetchLogEntry, error) {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return nil, err
	}
	return st.FetchHistory(ctx, sourceID, limit)
}

// SearchLog returns recent search log entries for a dossier.
func (svc *Service) SearchLog(ctx context.Context, dossierID string, limit int) ([]SearchLogEntry, error) {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return nil, err
	}
	return st.ListSearchLog(ctx, limit)
}

// ApplySchema applies the veille schema to a database.
// It first normalizes existing URLs and removes duplicates (idempotent),
// then applies the full schema including the UNIQUE index on sources(url).
// Exported for use by usertenant factories and migration scripts.
func ApplySchema(db *sql.DB) error {
	// Normalize URLs and remove duplicates BEFORE the UNIQUE index.
	if err := MigrateNormalizeURLs(db); err != nil {
		return fmt.Errorf("migrate normalize URLs: %w", err)
	}
	return store.ApplySchema(db)
}

// lookupGlobalEngine queries the global catalog for a search engine by ID.
func lookupGlobalEngine(ctx context.Context, catalogDB *sql.DB, id string) (*search.Engine, error) {
	var name, strategy, urlTemplate, apiConfigJSON, selectorsJSON string
	var rateLimitMs int64
	var maxPages, enabled int
	err := catalogDB.QueryRowContext(ctx,
		`SELECT name, strategy, url_template, api_config, selectors,
		rate_limit_ms, max_pages, enabled
		FROM global_search_engines WHERE id = ? AND enabled = 1`, id).
		Scan(&name, &strategy, &urlTemplate, &apiConfigJSON, &selectorsJSON,
			&rateLimitMs, &maxPages, &enabled)
	if err != nil {
		return nil, err
	}
	e := &search.Engine{
		ID:          id,
		Name:        name,
		Strategy:    strategy,
		URLTemplate: urlTemplate,
		RateLimitMs: rateLimitMs,
		MaxPages:    maxPages,
		Enabled:     enabled != 0,
	}
	if apiConfigJSON != "" && apiConfigJSON != "{}" {
		json.Unmarshal([]byte(apiConfigJSON), &e.APIConfig)
	}
	if selectorsJSON != "" && selectorsJSON != "{}" {
		json.Unmarshal([]byte(selectorsJSON), &e.Selectors)
	}
	return e, nil
}

// --- Admin: source health ---

// SourceHealth is a broken source with its dossier context.
type SourceHealth struct {
	DossierID string  `json:"dossier_id"`
	Source    *Source `json:"source"`
}

// ListSourceHealth returns all broken/error sources across all dossiers.
func (svc *Service) ListSourceHealth(ctx context.Context) ([]SourceHealth, error) {
	dossierIDs, err := svc.listActiveShards(ctx)
	if err != nil {
		return nil, err
	}

	var results []SourceHealth
	for _, dossierID := range dossierIDs {
		st, err := svc.resolveStore(ctx, dossierID)
		if err != nil {
			continue
		}
		broken, err := st.ListBrokenSources(ctx)
		if err != nil {
			continue
		}
		for _, src := range broken {
			results = append(results, SourceHealth{DossierID: dossierID, Source: src})
		}
	}
	return results, nil
}

// SweepNow triggers a manual sweep of all broken sources.
func (svc *Service) SweepNow(ctx context.Context) []repair.SweepResult {
	if svc.sweeper == nil {
		return nil
	}
	return svc.sweeper.SweepOnce(ctx)
}

// ProbeURL probes a URL and returns the HTTP status code.
func (svc *Service) ProbeURL(ctx context.Context, url string) (int, error) {
	return repair.ProbeURL(ctx, url, 0)
}

// ResetSource resets a source's error state so the scheduler picks it up again.
func (svc *Service) ResetSource(ctx context.Context, dossierID, sourceID string) error {
	st, err := svc.resolveStore(ctx, dossierID)
	if err != nil {
		return err
	}
	if err := st.ResetSource(ctx, sourceID); err != nil {
		return err
	}
	svc.auditLog(dossierID, "reset_source", fmt.Sprintf(`{"dossier_id":%q,"source_id":%q}`, dossierID, sourceID))
	return nil
}

// --- Internal ---

func (svc *Service) processJob(ctx context.Context, job *scheduler.Job) error {
	st, err := svc.resolveStore(ctx, job.DossierID)
	if err != nil {
		return err
	}
	pipeErr := svc.pipeline.HandleJob(ctx, st, &pipeline.Job{
		DossierID: job.DossierID,
		SourceID:  job.SourceID,
		URL:       job.URL,
	})
	if pipeErr != nil && svc.repairer != nil {
		// Attempt auto-repair after fetch failure.
		src, getErr := st.GetSource(ctx, job.SourceID)
		if getErr == nil && src != nil {
			statusCode := repair.ExtractStatusCode(pipeErr.Error())
			action := svc.repairer.TryRepair(ctx, st, src, statusCode, pipeErr)
			if action != repair.ActionNone {
				svc.logger.Info("auto-repair applied",
					"source_id", job.SourceID, "action", action)
			}
		}
	}
	return pipeErr
}

func (svc *Service) listActiveShards(ctx context.Context) ([]string, error) {
	if svc.catalogDB == nil {
		return nil, nil
	}
	rows, err := svc.catalogDB.QueryContext(ctx,
		`SELECT id FROM shards WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var dossierID string
		if err := rows.Scan(&dossierID); err != nil {
			return nil, err
		}
		result = append(result, dossierID)
	}
	return result, rows.Err()
}
