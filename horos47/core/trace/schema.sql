-- Table de traçabilité workflow (pattern HOROS 47)
CREATE TABLE IF NOT EXISTS workflow_execution_trace (
    trace_id TEXT PRIMARY KEY,
    workflow_name TEXT NOT NULL,
    workflow_run_id TEXT NOT NULL,
    step_name TEXT NOT NULL,
    step_index INTEGER NOT NULL,
    step_status TEXT NOT NULL CHECK(step_status IN ('started', 'completed', 'failed')),

    -- Input/Output tracking
    input_file_path TEXT,
    input_sha256 TEXT,
    output_file_path TEXT,
    output_sha256 TEXT,
    artifact_paths TEXT,  -- JSON array

    -- Worker metadata
    machine_name TEXT NOT NULL,
    worker_pid INTEGER NOT NULL,
    step_metadata TEXT DEFAULT '{}',  -- JSON extensible

    -- Timing
    started_at INTEGER NOT NULL,
    completed_at INTEGER,
    duration_ms INTEGER,

    created_at INTEGER DEFAULT (unixepoch())
);

-- Index pour idempotence (check duplicate par hash)
CREATE INDEX IF NOT EXISTS idx_workflow_trace_duplicate
    ON workflow_execution_trace(workflow_name, input_sha256, step_status);

-- Index pour requêtes par workflow run
CREATE INDEX IF NOT EXISTS idx_workflow_trace_run
    ON workflow_execution_trace(workflow_run_id, step_index);

-- Index pour recherche par step
CREATE INDEX IF NOT EXISTS idx_workflow_trace_step
    ON workflow_execution_trace(workflow_name, step_name, step_status);

-- Table des workflows disponibles (Livre de Recettes)
CREATE TABLE IF NOT EXISTS workflow_definitions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    agent_name TEXT,            -- links workflow to HORUM agent (NULL = system workflow)
    description TEXT,
    steps_chain TEXT NOT NULL,  -- JSON array: ["step1", "step2"]
    created_at INTEGER DEFAULT (unixepoch())
);

-- Workflow par défaut: Vision PDF OCR (system workflow, no agent)
INSERT OR IGNORE INTO workflow_definitions (id, name, agent_name, description, steps_chain) VALUES
('vision_pdf_ocr', 'Ingestion PDF avec OCR Vision', NULL,
    'Pipeline complet: PDF → Images → OCR → Database → Chunks',
    '["image_to_ocr", "ocr_to_database"]');

-- Agent workflows (one per HORUM agent)
-- All workflows start with clarify_intent (step 0) for uncertainty detection
INSERT OR IGNORE INTO workflow_definitions (id, name, agent_name, description, steps_chain) VALUES
('agent_sources',     'Sources',     'sources',     'RAG search + citations',      '["clarify_intent","rag_search","format_citations"]'),
('agent_syntheses',   'Syntheses',   'syntheses',   'Thread summarization',        '["clarify_intent","collect_context","llm_summarize"]'),
('agent_lexique',     'Lexique',     'lexique',     'Terminology extraction',      '["clarify_intent","extract_terms","llm_define"]'),
('agent_supervision', 'Supervision', 'supervision', 'Quality check',              '["clarify_intent","analyze_quality","generate_report"]'),
('agent_assistance',  'Assistance',  'assistance',  'User help',                  '["clarify_intent","understand_query","llm_respond"]'),
('agent_faq',         'FAQ',         'faq',         'FAQ generation',             '["clarify_intent","collect_questions","llm_faq"]'),
('agent_benchmarks',  'Benchmarks',  'benchmarks',  'Technical comparison',       '["clarify_intent","collect_data","llm_compare"]'),
('agent_search',      'Search',      'search',      'Web search',                '["clarify_intent","web_search","format_results"]');
