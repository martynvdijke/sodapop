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
		"share_links":        true,
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

// TestInitPreservesViewsAcrossReopen is the regression guard for the v1.27.0
// startup failure: Ent's schema rebuild could not rename a table because the
// drink_ratings view still referenced it. Init must drop views before the
// rebuild and restore them afterwards, including views users added themselves.
func TestInitPreservesViewsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	first, err := Init(path)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := first.SQL.ExecContext(context.Background(),
		`CREATE VIEW IF NOT EXISTS v_checkin_count AS SELECT COUNT(*) AS n FROM checkins`); err != nil {
		_ = first.Close()
		t.Fatalf("create view: %v", err)
	}
	// Force the next Init to rebuild checkins, as a schema-version upgrade
	// does. Without the view fix, Ent's rename fails here with
	// "error in view ...: no such table: main.checkins".
	if _, err := first.SQL.ExecContext(context.Background(),
		`ALTER TABLE checkins DROP COLUMN prep_note`); err != nil {
		_ = first.Close()
		t.Fatalf("drop column: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := Init(path)
	if err != nil {
		t.Fatalf("reinit: %v", err)
	}
	defer second.Close()

	var n int
	if err := second.SQL.QueryRowContext(context.Background(),
		`SELECT n FROM v_checkin_count`).Scan(&n); err != nil {
		t.Fatalf("query view: %v", err)
	}
	if n != 0 {
		t.Fatalf("v_checkin_count = %d, want 0", n)
	}

	var views int
	if err := second.SQL.QueryRowContext(context.Background(),
		`SELECT count(*) FROM sqlite_master WHERE type = 'view' AND name = 'drink_ratings'`).Scan(&views); err != nil {
		t.Fatalf("count views: %v", err)
	}
	if views != 1 {
		t.Fatalf("drink_ratings view count = %d, want 1", views)
	}

	var prepNote int
	if err := second.SQL.QueryRowContext(context.Background(),
		`SELECT count(*) FROM pragma_table_info('checkins') WHERE name = 'prep_note'`).Scan(&prepNote); err != nil {
		t.Fatalf("check checkins columns: %v", err)
	}
	if prepNote != 1 {
		t.Fatal("checkins was not rebuilt by the schema migration")
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
