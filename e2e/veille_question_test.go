package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hazyhaar/chrc/veille"

	_ "modernc.org/sqlite"
)

func TestE2E_QuestionLifecycle(t *testing.T) {
	// WHAT: Full lifecycle: add question → run → extractions + .md + dedup.
	// WHY: Validates the complete question pipeline end-to-end.
	bufDir := filepath.Join(t.TempDir(), "pending")

	// Mock search API.
	apiResp := map[string]any{
		"web": map[string]any{
			"results": []map[string]any{
				{"title": "LLM Inference Benchmark 2026", "url": "https://example.com/llm-2026", "description": "Comprehensive benchmark of LLM inference performance across hardware."},
				{"title": "Efficient Transformers Survey", "url": "https://example.com/transformers", "description": "Survey of efficient transformer architectures for real-time inference."},
				{"title": "GPU vs TPU for LLM", "url": "https://example.com/gpu-tpu", "description": "Comparative analysis of GPU and TPU for large language model deployment."},
			},
		},
	}
	apiBody, _ := json.Marshal(apiResp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(apiBody)
	}))
	defer srv.Close()

	// Setup service.
	pool := newTestPool()
	defer pool.Close()
	cfg := &veille.Config{BufferDir: bufDir}
	svc, err := veille.New(pool, cfg, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	ctx := context.Background()
	dossierID := "dossier-q"

	// Seed a search engine via the store (resolve the DB and use it directly).
	db, _ := pool.Resolve(ctx, dossierID)
	_, err = db.ExecContext(ctx,
		`INSERT INTO search_engines (id, name, strategy, url_template, api_config, selectors, stealth_level, rate_limit_ms, max_pages, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"test_api", "Test API", "api", srv.URL+"?q={query}",
		`{"result_path":"web.results","fields":{"title":"title","text":"description","url":"url"}}`,
		"{}", 0, 1000, 1, 1,
		1709000000000, 1709000000000,
	)
	if err != nil {
		t.Fatalf("insert engine: %v", err)
	}

	// Add a tracked question.
	q := &veille.TrackedQuestion{
		Text:        "LLM inference performance 2026",
		Keywords:    "LLM inference benchmark",
		Channels:    `["test_api"]`,
		ScheduleMs:  86400000,
		MaxResults:  20,
		FollowLinks: false,
		Enabled:     true,
	}
	if err := svc.AddQuestion(ctx, dossierID, q); err != nil {
		t.Fatalf("add question: %v", err)
	}
	if q.ID == "" {
		t.Fatal("question ID should be set")
	}

	// Verify question exists.
	questions, err := svc.ListQuestions(ctx, dossierID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("questions: got %d, want 1", len(questions))
	}

	// Verify auto-source was created.
	sources, _ := svc.ListSources(ctx, dossierID)
	var foundSource bool
	for _, s := range sources {
		if s.SourceType == "question" && s.ID == q.ID {
			foundSource = true
			break
		}
	}
	if !foundSource {
		t.Fatal("auto-source with type 'question' not found")
	}

	// Run the question.
	count, err := svc.RunQuestionNow(ctx, dossierID, q.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if count != 3 {
		t.Errorf("new results: got %d, want 3", count)
	}

	// Verify extractions (sourceID = q.ID).
	exts, _ := svc.QuestionResults(ctx, dossierID, q.ID, 50)
	if len(exts) != 3 {
		t.Fatalf("extractions: got %d, want 3", len(exts))
	}

	// Verify metadata contains question_id.
	var meta map[string]string
	json.Unmarshal([]byte(exts[0].MetadataJSON), &meta)
	if meta["question_id"] != q.ID {
		t.Errorf("metadata question_id: got %q, want %q", meta["question_id"], q.ID)
	}

	// Verify .md files in buffer.
	entries, err := os.ReadDir(bufDir)
	if err != nil {
		t.Fatalf("read buffer dir: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("buffer files: got %d, want 3", len(entries))
	}
	if len(entries) > 0 {
		data, _ := os.ReadFile(filepath.Join(bufDir, entries[0].Name()))
		content := string(data)
		if !strings.Contains(content, "source_type: question") {
			t.Error("buffer .md missing source_type: question")
		}
	}

	// Second run — dedup.
	count2, err := svc.RunQuestionNow(ctx, dossierID, q.ID)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if count2 != 0 {
		t.Errorf("dedup: second run got %d, want 0", count2)
	}

	// Verify extraction count unchanged.
	exts2, _ := svc.QuestionResults(ctx, dossierID, q.ID, 50)
	if len(exts2) != 3 {
		t.Errorf("extractions after dedup: got %d, want 3", len(exts2))
	}

	// Verify question run stats.
	updatedQ, _ := svc.ListQuestions(ctx, dossierID)
	if len(updatedQ) == 1 {
		if updatedQ[0].TotalResults != 3 {
			t.Errorf("total_results: got %d, want 3", updatedQ[0].TotalResults)
		}
	}

	// Delete question.
	if err := svc.DeleteQuestion(ctx, dossierID, q.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify question is gone.
	afterQ, _ := svc.ListQuestions(ctx, dossierID)
	if len(afterQ) != 0 {
		t.Errorf("questions after delete: got %d", len(afterQ))
	}
}
