package veille

import (
	"database/sql"
	"testing"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/store"

	_ "modernc.org/sqlite"
)

// openTestDBBaseSchema creates a DB with only the base schema (no UNIQUE index).
// This simulates a pre-migration shard that may contain duplicate URLs.
func openTestDBBaseSchema(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	if _, err := db.Exec(store.Schema); err != nil {
		t.Fatalf("apply base schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertTestSource(t *testing.T, db *sql.DB, id, name, url string, createdAt int64) {
	t.Helper()
	now := time.Now().UnixMilli()
	if createdAt == 0 {
		createdAt = now
	}
	_, err := db.Exec(
		`INSERT INTO sources (id, name, url, source_type, fetch_interval, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, 'rss', 3600000, 1, ?, ?)`,
		id, name, url, createdAt, now)
	if err != nil {
		t.Fatalf("insert source %s: %v", id, err)
	}
}

func TestMigrateNormalizeURLs_DeduplicatesSameURL(t *testing.T) {
	// WHAT: Duplicate exact URLs are merged into one (oldest kept).
	// WHY: Duplicate sources waste fetch cycles and produce duplicate results.
	db := openTestDBBaseSchema(t)
	now := time.Now().UnixMilli()

	insertTestSource(t, db, "old", "Old", "https://example.com/feed", now-10000)
	insertTestSource(t, db, "new", "New", "https://example.com/feed", now)

	if err := MigrateNormalizeURLs(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM sources").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 source after dedup, got %d", count)
	}

	// The oldest should be kept.
	var id string
	db.QueryRow("SELECT id FROM sources").Scan(&id)
	if id != "old" {
		t.Errorf("expected oldest source 'old' to be kept, got %q", id)
	}
}

func TestMigrateNormalizeURLs_NormalizedVariants(t *testing.T) {
	// WHAT: URL variants that normalize to the same canonical form are merged.
	// WHY: Case, trailing slash, and fragment differences must not bypass dedup.
	db := openTestDBBaseSchema(t)
	now := time.Now().UnixMilli()

	insertTestSource(t, db, "s1", "First", "https://example.com/feed", now-20000)
	insertTestSource(t, db, "s2", "Second", "HTTPS://Example.COM/feed/", now-10000)
	insertTestSource(t, db, "s3", "Third", "https://example.com/feed#section", now)

	if err := MigrateNormalizeURLs(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM sources").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 source after variant dedup, got %d", count)
	}

	// Check the URL is normalized.
	var url string
	db.QueryRow("SELECT url FROM sources").Scan(&url)
	if url != "https://example.com/feed" {
		t.Errorf("expected normalized URL, got %q", url)
	}
}

func TestMigrateNormalizeURLs_KeepsOldest(t *testing.T) {
	// WHAT: Among duplicates, the source with the earliest created_at survives.
	// WHY: Oldest source has the longest fetch history (extractions, fetch_log).
	db := openTestDBBaseSchema(t)

	insertTestSource(t, db, "oldest", "Oldest", "https://example.com", 1000)
	insertTestSource(t, db, "middle", "Middle", "https://example.com", 2000)
	insertTestSource(t, db, "newest", "Newest", "https://example.com", 3000)

	if err := MigrateNormalizeURLs(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var id string
	db.QueryRow("SELECT id FROM sources").Scan(&id)
	if id != "oldest" {
		t.Errorf("expected 'oldest' to survive, got %q", id)
	}
}

func TestMigrateNormalizeURLs_EmptyDB(t *testing.T) {
	// WHAT: Migration on an empty DB is a no-op.
	// WHY: New shards have no sources — migration must not error.
	db := openTestDBBaseSchema(t)

	if err := MigrateNormalizeURLs(db); err != nil {
		t.Errorf("migration on empty DB should not error: %v", err)
	}
}

func TestMigrateNormalizeURLs_NoSourcesTable(t *testing.T) {
	// WHAT: Migration on a DB without sources table is a no-op.
	// WHY: Newly created DBs may not have the schema applied yet.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := MigrateNormalizeURLs(db); err != nil {
		t.Errorf("migration on empty schema should not error: %v", err)
	}
}

func TestMigrateNormalizeURLs_Idempotent(t *testing.T) {
	// WHAT: Running migration twice produces the same result.
	// WHY: Migration may be called at every startup.
	db := openTestDBBaseSchema(t)
	now := time.Now().UnixMilli()

	insertTestSource(t, db, "s1", "A", "https://example.com/feed", now-10000)
	insertTestSource(t, db, "s2", "B", "HTTPS://Example.COM/feed/", now)

	if err := MigrateNormalizeURLs(db); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := MigrateNormalizeURLs(db); err != nil {
		t.Fatalf("second run: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM sources").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 source after idempotent run, got %d", count)
	}
}

func TestMigrateNormalizeURLs_NormalizesWithoutDuplicates(t *testing.T) {
	// WHAT: URLs are normalized even when there are no duplicates.
	// WHY: All stored URLs must be in canonical form for the UNIQUE index to work.
	db := openTestDBBaseSchema(t)

	insertTestSource(t, db, "s1", "A", "HTTPS://Example.COM/rss", 1000)
	insertTestSource(t, db, "s2", "B", "https://other.com/feed/", 2000)

	if err := MigrateNormalizeURLs(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM sources").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 sources (no dedup needed), got %d", count)
	}

	var url1, url2 string
	db.QueryRow("SELECT url FROM sources WHERE id = 's1'").Scan(&url1)
	db.QueryRow("SELECT url FROM sources WHERE id = 's2'").Scan(&url2)
	if url1 != "https://example.com/rss" {
		t.Errorf("s1 URL not normalized: %q", url1)
	}
	if url2 != "https://other.com/feed" {
		t.Errorf("s2 URL not normalized: %q", url2)
	}
}

func TestMigrateNormalizeURLs_UniqueIndexSucceedsAfter(t *testing.T) {
	// WHAT: After migration, the UNIQUE index can be created without error.
	// WHY: This is the whole point — migration must clean data for the index.
	db := openTestDBBaseSchema(t)
	now := time.Now().UnixMilli()

	insertTestSource(t, db, "s1", "A", "https://example.com/feed", now-10000)
	insertTestSource(t, db, "s2", "B", "https://example.com/feed", now)
	insertTestSource(t, db, "s3", "C", "HTTPS://Example.COM/feed/", now-5000)

	if err := MigrateNormalizeURLs(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Now apply the UNIQUE index — should succeed.
	if _, err := db.Exec(store.Migration001UniqueURL); err != nil {
		t.Fatalf("UNIQUE index failed after migration: %v", err)
	}

	// Verify it's enforced.
	_, err := db.Exec(
		`INSERT INTO sources (id, name, url, source_type, created_at, updated_at)
		 VALUES ('dup', 'Dup', 'https://example.com/feed', 'rss', ?, ?)`, now, now)
	if err == nil {
		t.Error("UNIQUE index should prevent duplicate insert")
	}
}

func TestMigrateNormalizeURLs_PreservesExtractions(t *testing.T) {
	// WHAT: Extractions from the surviving source are preserved.
	// WHY: We want to keep existing content, not lose it during migration.
	db := openTestDBBaseSchema(t)

	insertTestSource(t, db, "keep", "Keep", "https://example.com", 1000)
	insertTestSource(t, db, "drop", "Drop", "https://example.com", 2000)

	// Add extraction to the source we keep.
	now := time.Now().UnixMilli()
	db.Exec(`INSERT INTO extractions (id, source_id, content_hash, extracted_text, url, extracted_at)
		VALUES ('ext-1', 'keep', 'h1', 'content', 'https://example.com', ?)`, now)

	if err := MigrateNormalizeURLs(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var extCount int
	db.QueryRow("SELECT COUNT(*) FROM extractions WHERE source_id = 'keep'").Scan(&extCount)
	if extCount != 1 {
		t.Errorf("expected 1 extraction preserved, got %d", extCount)
	}
}
