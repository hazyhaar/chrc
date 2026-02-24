package vecbridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/hazyhaar/horosvec"
	"github.com/hazyhaar/pkg/connectivity"
	"github.com/hazyhaar/pkg/dbopen"

	_ "modernc.org/sqlite"
)

func testServiceConn(t *testing.T) (*Service, *connectivity.Router) {
	t.Helper()
	db := dbopen.OpenMemory(t)
	svc, err := NewFromDB(db, horosvec.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	router := connectivity.New()
	svc.RegisterConnectivity(router)
	return svc, router
}

func TestConn_Stats(t *testing.T) {
	_, router := testServiceConn(t)

	resp, err := router.Call(context.Background(), "horosvec_stats", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var stats struct {
		Count        int  `json:"count"`
		NeedsRebuild bool `json:"needs_rebuild"`
	}
	json.Unmarshal(resp, &stats)
	if stats.Count != 0 {
		t.Errorf("Count = %d, want 0", stats.Count)
	}
}

func TestConn_InsertAndSearch(t *testing.T) {
	svc, router := testServiceConn(t)
	ctx := context.Background()

	// Must build index first with seed data.
	buildTestIndex(t, svc, 4, 10)

	insertPayload, _ := json.Marshal(map[string]any{
		"ids":     []string{hex.EncodeToString([]byte{0xAA}), hex.EncodeToString([]byte{0xBB})},
		"vectors": [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}},
	})
	resp, err := router.Call(ctx, "horosvec_insert", insertPayload)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	var insertResult struct {
		Inserted int `json:"inserted"`
		Count    int `json:"count"`
	}
	json.Unmarshal(resp, &insertResult)
	if insertResult.Inserted != 2 {
		t.Errorf("Inserted = %d, want 2", insertResult.Inserted)
	}

	// Search.
	searchPayload, _ := json.Marshal(map[string]any{
		"vector": []float32{1, 0, 0, 0}, "top_k": 2,
	})
	resp, err = router.Call(ctx, "horosvec_search", searchPayload)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var searchResult struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	json.Unmarshal(resp, &searchResult)
	if len(searchResult.Results) == 0 {
		t.Fatal("expected results")
	}
}

func TestConn_Search_InvalidJSON(t *testing.T) {
	_, router := testServiceConn(t)
	_, err := router.Call(context.Background(), "horosvec_search", []byte(`broken`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
