package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
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

// TestNoUserLocationColumns is the guard for the product promise that user
// locations are never stored. Nearby is computed from the request coordinates
// and venues, not from a persisted profile.
func TestNoUserLocationColumns(t *testing.T) {
	database, err := Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	defer database.Close()

	rows, err := database.SQL.QueryContext(context.Background(), `PRAGMA table_info(users)`)
	if err != nil {
		t.Fatalf("table_info users: %v", err)
	}
	defer rows.Close()

	banned := map[string]bool{
		"lat":       true,
		"lng":       true,
		"latitude":  true,
		"longitude": true,
		"location":  true,
		"geohash":   true,
	}
	found := 0
	for rows.Next() {
		var (
			cid       int
			name      string
			typ       string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if banned[strings.ToLower(name)] {
			t.Errorf("users table has location column %q; user locations must not be stored", name)
		}
		found++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if found == 0 {
		t.Fatal("users table has no columns; schema did not initialise")
	}
}
