package db

import (
	"context"
	"path/filepath"
	"testing"
)

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
