package veille

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/hazyhaar/chrc/veille/internal/store"
	"github.com/hazyhaar/pkg/audit"

	_ "modernc.org/sqlite"
)

func TestAudit_SourceCreate(t *testing.T) {
	// WHAT: AddSource emits an audit log entry.
	// WHY: All data-modifying operations must be auditable per architecture rules.
	svc, auditDB := setupAuditService(t)
	ctx := context.Background()

	src := &Source{Name: "Audited", URL: "https://example.com", SourceType: "web", Enabled: true}
	if err := svc.AddSource(ctx, "d1", src); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Drain the async logger.
	svc.audit.Close()

	var count int
	auditDB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'add_source'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 audit entry for add_source, got %d", count)
	}
}

func TestAudit_SourceDelete(t *testing.T) {
	// WHAT: DeleteSource emits an audit log entry.
	// WHY: Deletion must be traceable.
	svc, auditDB := setupAuditService(t)
	ctx := context.Background()

	src := &Source{Name: "ToDelete", URL: "https://example.com", SourceType: "web", Enabled: true}
	svc.AddSource(ctx, "d1", src)
	svc.DeleteSource(ctx, "d1", src.ID)

	svc.audit.Close()

	var count int
	auditDB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'delete_source'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 audit entry for delete_source, got %d", count)
	}
}

func TestAudit_NoAudit_NoError(t *testing.T) {
	// WHAT: Service without audit logger does not crash.
	// WHY: Audit is optional — services without it should work fine.
	svc, _ := setupTestService(t)
	ctx := context.Background()

	src := &Source{Name: "NoAudit", URL: "https://example.com", SourceType: "web", Enabled: true}
	if err := svc.AddSource(ctx, "d1", src); err != nil {
		t.Fatalf("add without audit should work: %v", err)
	}
}

// setupAuditService creates a test service with audit enabled.
func setupAuditService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()

	// Shard DB.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	if err := store.ApplySchema(db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Audit DB (separate).
	auditDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open audit db: %v", err)
	}
	auditDB.Exec("PRAGMA journal_mode=WAL")
	t.Cleanup(func() { auditDB.Close() })

	auditLogger := audit.NewSQLiteLogger(auditDB)
	if err := auditLogger.Init(); err != nil {
		t.Fatalf("audit init: %v", err)
	}

	pool := &testPool{db: db}
	svc, err := New(pool, nil, nil, WithAudit(auditLogger))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	// Give the audit goroutine time to start.
	time.Sleep(10 * time.Millisecond)

	return svc, auditDB
}
