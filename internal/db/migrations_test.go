package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeliveryOutboxMigration guards the append-only 0008 migration: the due
// poll index is partial and the per-user history index exists.
func TestDeliveryOutboxMigration(t *testing.T) {
	database, err := Init(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	defer database.Close()
	ctx := context.Background()

	var version int
	if err := database.SQL.QueryRowContext(ctx,
		`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version < 8 {
		t.Fatalf("schema_migrations max = %d, want at least 8", version)
	}

	var dueSQL string
	if err := database.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'index' AND name = ?`,
		"idx_delivery_outbox_due").Scan(&dueSQL); err != nil {
		t.Fatalf("due index: %v", err)
	}
	if !strings.Contains(dueSQL, "delivered_at IS NULL") {
		t.Errorf("idx_delivery_outbox_due is not partial: %q", dueSQL)
	}

	var userCreated int
	if err := database.SQL.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'delivery_outbox' AND name = ?`,
		"idx_delivery_outbox_user_created").Scan(&userCreated); err != nil {
		t.Fatalf("user/created index: %v", err)
	}
	if userCreated != 1 {
		t.Error("idx_delivery_outbox_user_created is missing")
	}
}

// TestAuditIndexesMigration guards the append-only 0009 migration: both audit
// indexes exist and the recorded schema version reached it.
func TestAuditIndexesMigration(t *testing.T) {
	database, err := Init(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	defer database.Close()
	ctx := context.Background()

	var version int
	if err := database.SQL.QueryRowContext(ctx,
		`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version < 9 {
		t.Fatalf("schema_migrations max = %d, want at least 9", version)
	}

	for _, index := range []struct{ table, name string }{
		{"catalog_revisions", "idx_catalog_revisions_entity_created"},
		{"admin_notes", "idx_admin_notes_entity_created"},
	} {
		var n int
		if err := database.SQL.QueryRowContext(ctx,
			`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND name = ?`,
			index.table, index.name).Scan(&n); err != nil {
			t.Fatalf("check %s: %v", index.name, err)
		}
		if n != 1 {
			t.Errorf("index %s on %s is missing", index.name, index.table)
		}
	}

	var loginIndexes int
	if err := database.SQL.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_index_list('login_attempts')
		 WHERE name IN (SELECT name FROM pragma_index_list('login_attempts'))`).Scan(&loginIndexes); err != nil {
		t.Fatalf("login attempts indexes: %v", err)
	}
	if loginIndexes == 0 {
		t.Error("login_attempts has no index")
	}
}
