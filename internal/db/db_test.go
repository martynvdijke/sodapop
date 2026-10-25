package db

import (
	"context"
	"path/filepath"
	"testing"

	"sodapop/ent/migrate"
)

// TestSchemaConsistency is the drift guard: every table that exists at runtime
// must be either Ent-managed or explicitly declared as raw-only SQL.
func TestSchemaConsistency(t *testing.T) {
	database, err := Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	defer database.Close()

	rows, err := database.SQL.QueryContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()

	known := map[string]bool{}
	for _, table := range migrate.Tables {
		known[table.Name] = true
	}
	rawOnly := map[string]bool{
		"schema_migrations":  true,
		"drinks_fts":         true,
		"drinks_fts_data":    true,
		"drinks_fts_idx":     true,
		"drinks_fts_content": true,
		"drinks_fts_docsize": true,
		"drinks_fts_config":  true,
	}
	seen := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen++
		if !known[name] && !rawOnly[name] {
			t.Errorf("table %q is neither Ent-managed nor an allowed raw-only table", name)
		}
	}
	if seen == 0 {
		t.Fatal("no tables found; migrations did not run")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	var fts int
	if err := database.SQL.QueryRowContext(context.Background(),
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'drinks_fts'`).Scan(&fts); err != nil {
		t.Fatalf("check fts: %v", err)
	}
	if fts != 1 {
		t.Fatal("drinks_fts is missing")
	}
}
