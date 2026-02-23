package gateway

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"horos47/core/data"
	"horos47/core/jobs"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Service est le gateway unifié (sas IN/OUT) pour flux HORUM ↔ Edge
type Service struct {
	db         *sql.DB
	logger     *slog.Logger
	queue      *jobs.Queue
	httpClient *http.Client // shared persistent HTTP client for callbacks + polling
}

// New crée nouveau service gateway
func New(db *sql.DB, logger *slog.Logger) *Service {
	svc := &Service{
		db:     db,
		logger: logger,
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     90 * time.Second,
			},
			Timeout: 30 * time.Second,
		},
	}

	// Initialiser schéma gateway
	if err := svc.initSchema(); err != nil {
		logger.Error("Failed to initialize gateway schema", "error", err)
	}

	// Réutiliser la job queue existante
	queue, err := jobs.NewQueue(db)
	if err != nil {
		logger.Error("Failed to initialize job queue", "error", err)
	}
	svc.queue = queue

	return svc
}

// RegisterHTTP enregistre endpoints HTTP sur le router Chi
func (s *Service) RegisterHTTP(r chi.Router) {
	r.Post("/api/v1/sas/in", s.handleSubmit)
	r.Get("/api/v1/sas/status/{envelope_id}", s.handleStatus)

	// Clarification endpoints
	r.Post("/api/v1/sas/clarify/{envelope_id}", s.handleClarifyAnswer)

	// Chunked payload endpoints
	r.Post("/api/v1/sas/chunks/init", s.handleChunksInit)
	r.Post("/api/v1/sas/chunks/{payload_id}/chunk", s.handleChunkReceive)
}

// RegisterMCP enregistre tools MCP
func (s *Service) RegisterMCP(server *mcp.Server) error {
	// TODO: Expose gateway tools via MCP
	return nil
}

// Close arrête le service proprement
func (s *Service) Close() error {
	s.logger.Info("Gateway service closing")
	return nil
}

// initSchema crée tables gateway dans la DB partagée
func (s *Service) initSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS config_params (
			param_key TEXT PRIMARY KEY,
			param_value TEXT NOT NULL,
			description TEXT,
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		);

		INSERT OR IGNORE INTO config_params (param_key, param_value, description) VALUES
		('data_dir',             '/inference/horos47/data',            'Root data directory'),
		('staging_dir',          '/inference/agents/sources/staging',  'Temp staging for sources file downloads'),
		('agents_base_dir',      '/inference/agents',                  'Base directory for per-agent workspaces'),
		('horum_callback_url',   '',                                   'HORUM result callback URL'),
		('gateway_listen_addr',  ':8443',                              'QUIC/HTTP3 listen address'),
		('horum_pull_url',       '',                                   'HORUM base URL for pull mode (e.g. https://forum.docbusinessia.fr)');

		CREATE TABLE IF NOT EXISTS task_envelopes (
			envelope_id BLOB PRIMARY KEY,
			origin_mention_id TEXT NOT NULL,
			origin_node_id TEXT,
			origin_user_id TEXT NOT NULL,
			agent_name TEXT NOT NULL,
			workflow_name TEXT,
			payload_json TEXT NOT NULL DEFAULT '{}',
			provenance_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL CHECK(status IN (
				'received','routing','awaiting_clarification','processing','completed','failed','dispatched'
			)) DEFAULT 'received',
			result_json TEXT,
			error_message TEXT,
			priority INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (unixepoch()),
			started_at INTEGER,
			completed_at INTEGER
		);

		CREATE INDEX IF NOT EXISTS idx_envelopes_status ON task_envelopes(status, created_at)
			WHERE status IN ('received','routing','awaiting_clarification','processing');
		CREATE INDEX IF NOT EXISTS idx_envelopes_agent ON task_envelopes(agent_name, status);

		CREATE TABLE IF NOT EXISTS clarification_requests (
			request_id BLOB PRIMARY KEY,
			envelope_id BLOB NOT NULL REFERENCES task_envelopes(envelope_id),
			detected_uncertainties TEXT NOT NULL,
			questions TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('pending','answered','expired','cancelled'))
				DEFAULT 'pending',
			answers TEXT,
			created_at INTEGER NOT NULL DEFAULT (unixepoch()),
			expires_at INTEGER NOT NULL,
			answered_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_clarif_envelope ON clarification_requests(envelope_id);
		CREATE INDEX IF NOT EXISTS idx_clarif_pending ON clarification_requests(status, expires_at)
			WHERE status = 'pending';

		CREATE TABLE IF NOT EXISTS chunked_payloads (
			payload_id BLOB PRIMARY KEY,
			envelope_id BLOB NOT NULL REFERENCES task_envelopes(envelope_id),
			total_chunks INTEGER NOT NULL,
			received_chunks INTEGER NOT NULL DEFAULT 0,
			file_size INTEGER NOT NULL,
			file_sha256 TEXT NOT NULL,
			storage_dir TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('receiving','complete','failed'))
				DEFAULT 'receiving',
			created_at INTEGER NOT NULL DEFAULT (unixepoch()),
			completed_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_chunked_envelope ON chunked_payloads(envelope_id);

		-- Workflow definitions: maps agent_name → steps_chain
		-- DROP + CREATE because old table had 'name' instead of 'agent_name'
		DROP TABLE IF EXISTS workflow_definitions;
		CREATE TABLE IF NOT EXISTS workflow_definitions (
			id TEXT PRIMARY KEY,
			agent_name TEXT UNIQUE NOT NULL,
			description TEXT,
			steps_chain TEXT NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (unixepoch())
		);

		-- Ingest trackers: track multi-page document completion per envelope
		CREATE TABLE IF NOT EXISTS ingest_trackers (
			tracker_id BLOB PRIMARY KEY,
			envelope_id BLOB NOT NULL REFERENCES task_envelopes(envelope_id),
			document_id BLOB NOT NULL,
			file_path TEXT NOT NULL,
			file_format TEXT NOT NULL,
			total_pages INTEGER NOT NULL DEFAULT 0,
			completed_pages INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL CHECK(status IN ('pending','processing','completed','failed'))
				DEFAULT 'pending',
			created_at INTEGER NOT NULL DEFAULT (unixepoch()),
			completed_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_ingest_tracker_envelope ON ingest_trackers(envelope_id);
		CREATE INDEX IF NOT EXISTS idx_ingest_tracker_doc ON ingest_trackers(document_id);

		INSERT OR IGNORE INTO workflow_definitions (id, agent_name, description, steps_chain) VALUES
			('wf_sources',      'sources',      'Ingestion document: fetch PDF → pipeline OCR',  '["clarify_intent","fetch_and_ingest","detect_format"]'),
			('wf_syntheses',    'syntheses',    'Synthese de discussion',                        '["clarify_intent","generate_synthesis"]'),
			('wf_lexique',      'lexique',      'Extraction glossaire',                          '["clarify_intent","extract_glossary"]'),
			('wf_supervision',  'supervision',  'Monitoring qualite',                            '["analyze_quality"]'),
			('wf_assistance',   'assistance',   'Aide utilisateur: RAG + LLM',                   '["clarify_intent","rag_retrieve","generate_answer"]'),
			('wf_faq',          'faq',          'Generation FAQ',                                '["clarify_intent","generate_faq"]'),
			('wf_benchmarks',   'benchmarks',   'Comparaison technique',                         '["clarify_intent","generate_benchmark"]'),
			('wf_search',       'search',       'Recherche web',                                 '["clarify_intent","web_search","summarize_results"]'),
			('wf_auto_ingest',  'auto_ingest',  'Auto-ingest uploaded files from forum',         '["fetch_and_ingest","detect_format"]'),
			('wf_rag',          'rag',          'RAG retrieve + generate answer',                '["rag_retrieve","generate_answer"]');
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("init gateway schema: %w", err)
	}

	return nil
}

// GetConfigParam lit un paramètre depuis config_params
func (s *Service) GetConfigParam(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT param_value FROM config_params WHERE param_key = ?`, key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("config param %q: %w", key, err)
	}
	return value, nil
}

// SetConfigParam écrit un paramètre dans config_params
func (s *Service) SetConfigParam(key, value string) error {
	_, err := data.ExecWithRetry(s.db, `
		INSERT INTO config_params (param_key, param_value, updated_at)
		VALUES (?, ?, unixepoch())
		ON CONFLICT(param_key) DO UPDATE SET param_value = excluded.param_value, updated_at = excluded.updated_at
	`, key, value)
	return err
}

// SubmitRequest est le payload JSON pour POST /api/v1/sas/in
type SubmitRequest struct {
	OriginMentionID string                 `json:"origin_mention_id"`
	OriginNodeID    string                 `json:"origin_node_id,omitempty"`
	OriginUserID    string                 `json:"origin_user_id"`
	AgentName       string                 `json:"agent_name"`
	Payload         map[string]interface{} `json:"payload"`
	Provenance      map[string]interface{} `json:"provenance"`
	Priority        int                    `json:"priority,omitempty"`
}

// SubmitResponse est la réponse pour POST /api/v1/sas/in
type SubmitResponse struct {
	EnvelopeID string `json:"envelope_id"`
	Status     string `json:"status"`
}

// CreateEnvelopeFromRequest validates and inserts a task_envelope from a SubmitRequest.
// Used by both the HTTP handler and the pull-mode poller.
func (s *Service) CreateEnvelopeFromRequest(req SubmitRequest) (data.UUID, error) {
	if req.OriginMentionID == "" || req.OriginUserID == "" || req.AgentName == "" {
		return data.UUID{}, fmt.Errorf("origin_mention_id, origin_user_id, agent_name required")
	}

	envelopeID := data.NewUUID()

	payloadJSON, err := json.Marshal(req.Payload)
	if err != nil {
		return data.UUID{}, fmt.Errorf("marshal payload: %w", err)
	}

	provenanceJSON, err := json.Marshal(req.Provenance)
	if err != nil {
		return data.UUID{}, fmt.Errorf("marshal provenance: %w", err)
	}

	_, err = data.ExecWithRetry(s.db, `
		INSERT INTO task_envelopes (envelope_id, origin_mention_id, origin_node_id, origin_user_id,
			agent_name, payload_json, provenance_json, status, priority, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'received', ?, unixepoch())
	`, envelopeID, req.OriginMentionID, req.OriginNodeID, req.OriginUserID,
		req.AgentName, string(payloadJSON), string(provenanceJSON), req.Priority)

	if err != nil {
		return data.UUID{}, fmt.Errorf("insert envelope: %w", err)
	}

	s.logger.Info("Envelope received",
		"envelope_id", envelopeID.String(),
		"agent", req.AgentName,
		"mention", req.OriginMentionID)

	return envelopeID, nil
}

// handleSubmit est le sas IN : reçoit TaskEnvelope depuis HORUM
func (s *Service) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var req SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	envelopeID, err := s.CreateEnvelopeFromRequest(req)
	if err != nil {
		s.logger.Error("Failed to create envelope", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := SubmitResponse{
		EnvelopeID: envelopeID.String(),
		Status:     "received",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// handleStatus retourne le statut d'un envelope
func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	envelopeIDStr := chi.URLParam(r, "envelope_id")
	envelopeID, err := data.ParseUUID(envelopeIDStr)
	if err != nil {
		http.Error(w, "Invalid envelope_id", http.StatusBadRequest)
		return
	}

	var status, agentName, workflowName sql.NullString
	var resultJSON, errorMsg sql.NullString
	var createdAt, startedAt, completedAt sql.NullInt64

	err = s.db.QueryRow(`
		SELECT status, agent_name, workflow_name, result_json, error_message,
			created_at, started_at, completed_at
		FROM task_envelopes WHERE envelope_id = ?
	`, envelopeID).Scan(&status, &agentName, &workflowName, &resultJSON, &errorMsg,
		&createdAt, &startedAt, &completedAt)

	if err == sql.ErrNoRows {
		http.Error(w, "Envelope not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Error("Failed to query envelope", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"envelope_id": envelopeIDStr,
		"status":      status.String,
		"agent_name":  agentName.String,
	}
	if workflowName.Valid {
		resp["workflow_name"] = workflowName.String
	}
	if resultJSON.Valid {
		resp["result_json"] = resultJSON.String
	}
	if errorMsg.Valid {
		resp["error_message"] = errorMsg.String
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
