package veille

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// catalogDDL is the minimal shards table for testing listActiveShards.
const catalogDDL = `
CREATE TABLE IF NOT EXISTS shards (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    strategy   TEXT NOT NULL DEFAULT 'local',
    endpoint   TEXT NOT NULL DEFAULT '',
    config     TEXT NOT NULL DEFAULT '{}',
    status     TEXT NOT NULL DEFAULT 'active',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
`

func openCatalogDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	if _, err := db.Exec(catalogDDL); err != nil {
		t.Fatalf("catalog schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertShard(t *testing.T, db *sql.DB, dossierID, status string) {
	t.Helper()
	now := time.Now().UnixMilli()
	_, err := db.Exec(
		`INSERT INTO shards (id, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?)`, dossierID, status, now, now)
	if err != nil {
		t.Fatalf("insert shard: %v", err)
	}
}

func TestListActiveShards_ReturnsDossierIDs(t *testing.T) {
	// WHAT: listActiveShards returns active dossier IDs.
	// WHY: The scheduler needs these to poll DueSources across all tenants.
	catalogDB := openCatalogDB(t)
	insertShard(t, catalogDB, "dossier-a", "active")
	insertShard(t, catalogDB, "dossier-b", "active")
	insertShard(t, catalogDB, "dossier-c", "active")

	svc, _ := setupTestService(t)
	svc.catalogDB = catalogDB

	ctx := context.Background()
	shards, err := svc.listActiveShards(ctx)
	if err != nil {
		t.Fatalf("listActiveShards: %v", err)
	}
	if len(shards) != 3 {
		t.Fatalf("expected 3 shards, got %d", len(shards))
	}

	// Check all IDs are present.
	found := make(map[string]bool)
	for _, s := range shards {
		found[s] = true
	}
	expected := []string{"dossier-a", "dossier-b", "dossier-c"}
	for _, e := range expected {
		if !found[e] {
			t.Errorf("missing shard: %v", e)
		}
	}
}

func TestListActiveShards_FiltersNonActive(t *testing.T) {
	// WHAT: Deleted and archived shards are excluded.
	// WHY: Scheduler must not poll disabled tenants.
	catalogDB := openCatalogDB(t)
	insertShard(t, catalogDB, "active-dossier", "active")
	insertShard(t, catalogDB, "deleted-dossier", "deleted")
	insertShard(t, catalogDB, "archived-dossier", "archived")

	svc, _ := setupTestService(t)
	svc.catalogDB = catalogDB

	ctx := context.Background()
	shards, err := svc.listActiveShards(ctx)
	if err != nil {
		t.Fatalf("listActiveShards: %v", err)
	}
	if len(shards) != 1 {
		t.Fatalf("expected 1 active shard, got %d", len(shards))
	}
	if shards[0] != "active-dossier" {
		t.Errorf("unexpected shard: %v", shards[0])
	}
}

func TestListActiveShards_NoCatalog_ReturnsNil(t *testing.T) {
	// WHAT: Without a catalog DB, listActiveShards returns nil (no error).
	// WHY: catalogDB is optional — services without it should degrade gracefully.
	svc, _ := setupTestService(t)
	// svc.catalogDB is nil by default from setupTestService.

	ctx := context.Background()
	shards, err := svc.listActiveShards(ctx)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if shards != nil {
		t.Errorf("expected nil shards, got %v", shards)
	}
}

func TestListActiveShards_EmptyCatalog(t *testing.T) {
	// WHAT: Empty catalog returns empty slice (not error).
	// WHY: A fresh install with no spaces should not break the scheduler.
	catalogDB := openCatalogDB(t)

	svc, _ := setupTestService(t)
	svc.catalogDB = catalogDB

	ctx := context.Background()
	shards, err := svc.listActiveShards(ctx)
	if err != nil {
		t.Fatalf("listActiveShards: %v", err)
	}
	if len(shards) != 0 {
		t.Errorf("expected 0 shards, got %d", len(shards))
	}
}
