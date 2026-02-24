// Package e2e tests cross-package integration chains through the connectivity router.
//
// These tests verify that chrc packages compose correctly when wired
// together on a shared connectivity.Router — the production integration pattern.
package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/hazyhaar/chrc/docpipe"
	"github.com/hazyhaar/chrc/domkeeper"
	"github.com/hazyhaar/chrc/domregistry"
	"github.com/hazyhaar/chrc/horosembed"
	"github.com/hazyhaar/chrc/vecbridge"
	"github.com/hazyhaar/horosvec"
	"github.com/hazyhaar/pkg/connectivity"
	"github.com/hazyhaar/pkg/dbopen"

	_ "modernc.org/sqlite"
)

// --- test helpers ---

// hashEmbedder produces deterministic non-zero vectors from text hash.
// Used instead of noopEmbedder which produces zero vectors (unusable in ANN search).
type hashEmbedder struct {
	dim int
}

func (h *hashEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	hash := sha256.Sum256([]byte(text))
	vec := make([]float32, h.dim)
	for i := range vec {
		offset := (i * 4) % len(hash)
		bits := binary.LittleEndian.Uint32(hash[offset:])
		vec[i] = float32(bits%1000)/500.0 - 1.0
	}
	// Normalise to unit length for cosine similarity.
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] = float32(float64(vec[i]) / norm)
		}
	}
	return vec, nil
}

func (h *hashEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := h.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (h *hashEmbedder) Dimension() int { return h.dim }
func (h *hashEmbedder) Model() string  { return "test-hash" }

func callConn(t *testing.T, router *connectivity.Router, service string, payload any) []byte {
	t.Helper()
	var data []byte
	if payload != nil {
		var err error
		data, err = json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload for %s: %v", service, err)
		}
	}
	resp, err := router.Call(context.Background(), service, data)
	if err != nil {
		t.Fatalf("router.Call(%s): %v", service, err)
	}
	return resp
}

// --- E2E: shared router with all services ---

func TestE2E_SharedRouter_AllStats(t *testing.T) {
	dir := t.TempDir()
	router := connectivity.New()

	// domkeeper
	dk, err := domkeeper.New(&domkeeper.Config{DBPath: filepath.Join(dir, "dk.db")}, nil)
	if err != nil {
		t.Fatalf("domkeeper.New: %v", err)
	}
	t.Cleanup(func() { dk.Close() })
	dk.RegisterConnectivity(router)

	// domregistry
	dr, err := domregistry.New(&domregistry.Config{DBPath: filepath.Join(dir, "dr.db")}, nil)
	if err != nil {
		t.Fatalf("domregistry.New: %v", err)
	}
	t.Cleanup(func() { dr.Close() })
	dr.RegisterConnectivity(router)

	// horosembed
	emb := horosembed.New(horosembed.Config{Dimension: 8, Model: "test-shared"})
	horosembed.RegisterConnectivity(router, emb)

	// vecbridge
	vecDB := dbopen.OpenMemory(t)
	vecSvc, err := vecbridge.NewFromDB(vecDB, horosvec.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("vecbridge.NewFromDB: %v", err)
	}
	vecSvc.RegisterConnectivity(router)

	// docpipe
	pipe := docpipe.New(docpipe.Config{})
	pipe.RegisterConnectivity(router)

	// Call stats/info from each service — all should succeed.
	checks := []struct {
		service string
		key     string
	}{
		{"domkeeper_stats", "rules"},
		{"domregistry_stats", "profiles"},
		{"horosvec_stats", "count"},
		{"docpipe_detect", "format"},
	}

	for _, c := range checks {
		var payload any
		if c.service == "docpipe_detect" {
			payload = map[string]any{"path": "test.md"}
		}
		resp := callConn(t, router, c.service, payload)
		var result map[string]any
		if err := json.Unmarshal(resp, &result); err != nil {
			t.Errorf("%s: unmarshal: %v (raw: %s)", c.service, err, string(resp))
			continue
		}
		if _, ok := result[c.key]; !ok {
			t.Errorf("%s: missing key %q in response: %v", c.service, c.key, result)
		}
	}
}

// --- E2E: horosembed → vecbridge (embed → insert → search) ---

func TestE2E_EmbedInsertSearch(t *testing.T) {
	const dim = 8
	router := connectivity.New()

	// Register hashEmbedder (produces non-zero deterministic vectors).
	emb := &hashEmbedder{dim: dim}
	horosembed.RegisterConnectivity(router, emb)

	// Create vecbridge with seed data so index is built.
	vecDB := dbopen.OpenMemory(t)
	vecSvc, err := vecbridge.NewFromDB(vecDB, horosvec.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}

	// Build seed index (required before insert/search).
	seedVecs := make([][]float32, 20)
	seedIDs := make([][]byte, 20)
	for i := range seedVecs {
		v := make([]float32, dim)
		for j := range v {
			v[j] = rand.Float32() - 0.5
		}
		seedVecs[i] = v
		seedIDs[i] = []byte{byte(i >> 8), byte(i)}
	}
	iter := &sliceIter{vecs: seedVecs, ids: seedIDs}
	if err := vecSvc.Index.Build(context.Background(), iter); err != nil {
		t.Fatal(err)
	}
	vecSvc.RegisterConnectivity(router)

	ctx := context.Background()

	// Step 1: Embed text via connectivity.
	embedPayload, _ := json.Marshal(map[string]any{"text": "Machine learning is transforming industries"})
	embedResp, err := router.Call(ctx, "horosembed_embed", embedPayload)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	var embedResult struct {
		Vector    []float32 `json:"vector"`
		Dimension int       `json:"dimension"`
	}
	json.Unmarshal(embedResp, &embedResult)
	if embedResult.Dimension != dim {
		t.Fatalf("embed dimension = %d, want %d", embedResult.Dimension, dim)
	}

	// Step 2: Insert the embedding into vecbridge via connectivity.
	docID := hex.EncodeToString([]byte("doc-ml-01"))
	insertPayload, _ := json.Marshal(map[string]any{
		"ids":     []string{docID},
		"vectors": [][]float32{embedResult.Vector},
	})
	insertResp, err := router.Call(ctx, "horosvec_insert", insertPayload)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	var insertResult struct {
		Inserted int `json:"inserted"`
	}
	json.Unmarshal(insertResp, &insertResult)
	if insertResult.Inserted != 1 {
		t.Errorf("inserted = %d, want 1", insertResult.Inserted)
	}

	// Step 3: Search with the same embedding — should find our document.
	searchPayload, _ := json.Marshal(map[string]any{
		"vector": embedResult.Vector,
		"top_k":  5,
	})
	searchResp, err := router.Call(ctx, "horosvec_search", searchPayload)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var searchResult struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	json.Unmarshal(searchResp, &searchResult)
	if len(searchResult.Results) == 0 {
		t.Fatal("expected search results")
	}

	// Verify our document appears in results.
	found := false
	for _, r := range searchResult.Results {
		if r.ID == docID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("document %q not found in search results: %v", docID, searchResult.Results)
	}
}

// --- E2E: domregistry full lifecycle (publish → report → correct → verify) ---

func TestE2E_RegistryLifecycle(t *testing.T) {
	dir := t.TempDir()
	router := connectivity.New()

	reg, err := domregistry.New(&domregistry.Config{
		DBPath:     filepath.Join(dir, "registry.db"),
		AutoAccept: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	reg.RegisterConnectivity(router)

	// Step 1: Publish a profile.
	publishResp := callConn(t, router, "domregistry_publish_profile", map[string]any{
		"url_pattern": "https://example.com/articles/*",
		"domain":      "example.com",
		"extractors":  `{"title":"h1","body":".article-body"}`,
		"dom_profile": `{"type":"blog"}`,
		"trust_level": "community",
	})
	var profile struct {
		ID     string `json:"id"`
		Domain string `json:"domain"`
	}
	json.Unmarshal(publishResp, &profile)
	if profile.ID == "" {
		t.Fatal("expected profile ID after publish")
	}
	if profile.Domain != "example.com" {
		t.Errorf("domain = %q, want example.com", profile.Domain)
	}

	// Step 2: Report a failure for the profile.
	callConn(t, router, "domregistry_report_failure", map[string]any{
		"profile_id":  profile.ID,
		"instance_id": "worker-01",
		"error_type":  "selector_broken",
		"message":     "h1 selector returned empty",
	})

	// Step 3: Submit a correction.
	corrResp := callConn(t, router, "domregistry_submit_correction", map[string]any{
		"profile_id":     profile.ID,
		"instance_id":    "worker-01",
		"new_extractors": `{"title":"h1.title","body":".content"}`,
		"reason":         "selector_broken",
	})
	var correction struct {
		ProfileID string `json:"profile_id"`
	}
	json.Unmarshal(corrResp, &correction)
	if correction.ProfileID != profile.ID {
		t.Errorf("correction profile_id = %q, want %q", correction.ProfileID, profile.ID)
	}

	// Step 4: Verify stats reflect all operations.
	statsResp := callConn(t, router, "domregistry_stats", nil)
	var stats struct {
		Profiles    int `json:"profiles"`
		Corrections int `json:"corrections"`
		Reports     int `json:"reports"`
	}
	json.Unmarshal(statsResp, &stats)
	if stats.Profiles != 1 {
		t.Errorf("profiles = %d, want 1", stats.Profiles)
	}
	if stats.Corrections != 1 {
		t.Errorf("corrections = %d, want 1", stats.Corrections)
	}
	if stats.Reports != 1 {
		t.Errorf("reports = %d, want 1", stats.Reports)
	}

	// Step 5: Search profiles by domain — should find the published profile.
	searchResp := callConn(t, router, "domregistry_search_profiles", map[string]any{
		"domain": "example.com",
	})
	var profiles []struct {
		ID     string `json:"id"`
		Domain string `json:"domain"`
	}
	json.Unmarshal(searchResp, &profiles)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].ID != profile.ID {
		t.Errorf("search returned wrong profile: %q, want %q", profiles[0].ID, profile.ID)
	}
}

// --- E2E: docpipe extract → horosembed embed (document-to-vector pipeline) ---

func TestE2E_ExtractAndEmbed(t *testing.T) {
	const dim = 16
	router := connectivity.New()

	// Register docpipe.
	pipe := docpipe.New(docpipe.Config{})
	pipe.RegisterConnectivity(router)

	// Register horosembed with hash embedder.
	emb := &hashEmbedder{dim: dim}
	horosembed.RegisterConnectivity(router, emb)

	// Create a temp text document.
	dir := t.TempDir()
	docPath := filepath.Join(dir, "knowledge.txt")
	content := "Photosynthesis converts sunlight into chemical energy in plants. " +
		"This process is fundamental to life on Earth, producing oxygen as a byproduct."
	os.WriteFile(docPath, []byte(content), 0644)

	ctx := context.Background()

	// Step 1: Extract document via docpipe.
	extractPayload, _ := json.Marshal(map[string]any{"path": docPath})
	extractResp, err := router.Call(ctx, "docpipe_extract", extractPayload)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	var doc struct {
		RawText string `json:"raw_text"`
		Format  string `json:"format"`
	}
	json.Unmarshal(extractResp, &doc)
	if doc.RawText == "" {
		t.Fatal("expected non-empty extracted text")
	}
	if doc.Format != "txt" {
		t.Errorf("format = %q, want txt", doc.Format)
	}

	// Step 2: Embed the extracted text.
	embedPayload, _ := json.Marshal(map[string]any{"text": doc.RawText})
	embedResp, err := router.Call(ctx, "horosembed_embed", embedPayload)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	var embedResult struct {
		Vector    []float32 `json:"vector"`
		Dimension int       `json:"dimension"`
	}
	json.Unmarshal(embedResp, &embedResult)
	if embedResult.Dimension != dim {
		t.Errorf("dimension = %d, want %d", embedResult.Dimension, dim)
	}
	if len(embedResult.Vector) != dim {
		t.Errorf("vector len = %d, want %d", len(embedResult.Vector), dim)
	}

	// Step 3: Embed the same text again — must be deterministic.
	embedResp2, _ := router.Call(ctx, "horosembed_embed", embedPayload)
	var embedResult2 struct {
		Vector []float32 `json:"vector"`
	}
	json.Unmarshal(embedResp2, &embedResult2)
	for i := range embedResult.Vector {
		if embedResult.Vector[i] != embedResult2.Vector[i] {
			t.Fatalf("embedding not deterministic at index %d: %f != %f",
				i, embedResult.Vector[i], embedResult2.Vector[i])
		}
	}
}

// --- E2E: domkeeper rule → search through connectivity ---

func TestE2E_KeeperRuleAndSearch(t *testing.T) {
	dir := t.TempDir()
	router := connectivity.New()

	dk, err := domkeeper.New(&domkeeper.Config{DBPath: filepath.Join(dir, "keeper.db")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dk.Close() })
	dk.RegisterConnectivity(router)

	// Step 1: Create rule via connectivity.
	ruleResp := callConn(t, router, "domkeeper_add_rule", map[string]any{
		"name":        "News articles",
		"url_pattern": "https://news.example.com/*",
	})
	var rule struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		ExtractMode string `json:"extract_mode"`
		TrustLevel  string `json:"trust_level"`
		Enabled     bool   `json:"enabled"`
	}
	json.Unmarshal(ruleResp, &rule)
	if rule.ID == "" {
		t.Fatal("expected rule ID")
	}
	if rule.ExtractMode != "auto" {
		t.Errorf("default extract_mode = %q, want auto", rule.ExtractMode)
	}
	if !rule.Enabled {
		t.Error("rule should be enabled by default")
	}

	// Step 2: List rules — should contain our rule.
	listResp := callConn(t, router, "domkeeper_list_rules", map[string]any{})
	var rules []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(listResp, &rules)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].ID != rule.ID {
		t.Errorf("listed rule ID = %q, want %q", rules[0].ID, rule.ID)
	}

	// Step 3: Stats should show 1 rule, 0 content.
	statsResp := callConn(t, router, "domkeeper_stats", nil)
	var stats struct {
		Rules   int `json:"rules"`
		Content int `json:"content"`
		Chunks  int `json:"chunks"`
	}
	json.Unmarshal(statsResp, &stats)
	if stats.Rules != 1 {
		t.Errorf("rules = %d, want 1", stats.Rules)
	}
	if stats.Content != 0 {
		t.Errorf("content = %d, want 0", stats.Content)
	}

	// Step 4: Delete rule via connectivity.
	delResp := callConn(t, router, "domkeeper_delete_rule", map[string]any{
		"rule_id": rule.ID,
	})
	var delResult struct {
		Status string `json:"status"`
	}
	json.Unmarshal(delResp, &delResult)
	if delResult.Status != "deleted" {
		t.Errorf("delete status = %q, want deleted", delResult.Status)
	}

	// Step 5: Stats should show 0 rules.
	statsResp = callConn(t, router, "domkeeper_stats", nil)
	json.Unmarshal(statsResp, &stats)
	if stats.Rules != 0 {
		t.Errorf("rules after delete = %d, want 0", stats.Rules)
	}
}

// --- E2E: horosembed batch → vecbridge bulk insert ---

func TestE2E_BatchEmbedBulkInsert(t *testing.T) {
	const dim = 8
	router := connectivity.New()

	emb := &hashEmbedder{dim: dim}
	horosembed.RegisterConnectivity(router, emb)

	vecDB := dbopen.OpenMemory(t)
	vecSvc, err := vecbridge.NewFromDB(vecDB, horosvec.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}

	// Build seed index.
	seedVecs := make([][]float32, 20)
	seedIDs := make([][]byte, 20)
	for i := range seedVecs {
		v := make([]float32, dim)
		for j := range v {
			v[j] = rand.Float32() - 0.5
		}
		seedVecs[i] = v
		seedIDs[i] = []byte{byte(i >> 8), byte(i)}
	}
	iter := &sliceIter{vecs: seedVecs, ids: seedIDs}
	if err := vecSvc.Index.Build(context.Background(), iter); err != nil {
		t.Fatal(err)
	}
	vecSvc.RegisterConnectivity(router)

	ctx := context.Background()

	// Step 1: Batch embed multiple texts.
	texts := []string{
		"Artificial intelligence and machine learning",
		"Quantum computing and cryptography",
		"Climate change and renewable energy",
	}
	batchPayload, _ := json.Marshal(map[string]any{"texts": texts})
	batchResp, err := router.Call(ctx, "horosembed_batch", batchPayload)
	if err != nil {
		t.Fatalf("batch embed: %v", err)
	}
	var batchResult struct {
		Vectors   [][]float32 `json:"vectors"`
		Count     int         `json:"count"`
		Dimension int         `json:"dimension"`
	}
	json.Unmarshal(batchResp, &batchResult)
	if batchResult.Count != 3 {
		t.Fatalf("batch count = %d, want 3", batchResult.Count)
	}
	if batchResult.Dimension != dim {
		t.Errorf("batch dimension = %d, want %d", batchResult.Dimension, dim)
	}

	// Step 2: Bulk insert all embeddings into vecbridge.
	ids := make([]string, len(texts))
	for i := range ids {
		ids[i] = hex.EncodeToString([]byte{0xF0, byte(i)})
	}
	insertPayload, _ := json.Marshal(map[string]any{
		"ids":     ids,
		"vectors": batchResult.Vectors,
	})
	insertResp, err := router.Call(ctx, "horosvec_insert", insertPayload)
	if err != nil {
		t.Fatalf("bulk insert: %v", err)
	}
	var insertResult struct {
		Inserted int `json:"inserted"`
	}
	json.Unmarshal(insertResp, &insertResult)
	if insertResult.Inserted != 3 {
		t.Errorf("inserted = %d, want 3", insertResult.Inserted)
	}

	// Step 3: Search for one of the texts — should find its vector.
	searchPayload, _ := json.Marshal(map[string]any{
		"vector": batchResult.Vectors[0],
		"top_k":  3,
	})
	searchResp, err := router.Call(ctx, "horosvec_search", searchPayload)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var searchResult struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	json.Unmarshal(searchResp, &searchResult)
	if len(searchResult.Results) == 0 {
		t.Fatal("expected search results")
	}
}

// --- E2E: docpipe format detection across multiple types ---

func TestE2E_DocpipeMultiFormat(t *testing.T) {
	router := connectivity.New()
	pipe := docpipe.New(docpipe.Config{})
	pipe.RegisterConnectivity(router)

	formats := []struct {
		path   string
		expect string
	}{
		{"report.pdf", "pdf"},
		{"notes.md", "md"},
		{"data.docx", "docx"},
		{"readme.txt", "txt"},
		{"page.html", "html"},
	}

	for _, f := range formats {
		resp := callConn(t, router, "docpipe_detect", map[string]any{"path": f.path})
		var result struct {
			Format string `json:"format"`
		}
		json.Unmarshal(resp, &result)
		if result.Format != f.expect {
			t.Errorf("detect(%q) = %q, want %q", f.path, result.Format, f.expect)
		}
	}
}

// sliceIter implements horosvec.VectorIterator for building test indices.
type sliceIter struct {
	vecs [][]float32
	ids  [][]byte
	pos  int
}

func (s *sliceIter) Next() ([]byte, []float32, bool) {
	if s.pos >= len(s.vecs) {
		return nil, nil, false
	}
	id := s.ids[s.pos]
	vec := s.vecs[s.pos]
	s.pos++
	return id, vec, true
}

func (s *sliceIter) Reset() error {
	s.pos = 0
	return nil
}
