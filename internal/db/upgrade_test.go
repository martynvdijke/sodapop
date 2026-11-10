package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpgradeFromOlderSchema exercises the versioned migration ledger the way a
// real release upgrade does. It builds a current database, rewinds it to the
// schema the previous release shipped (schema version 14: the newest migrations
// are removed from schema_migrations and the columns they added are dropped),
// then runs Init again and checks that the ledger completes exactly once, the
// columns and the drink_ratings view come back, and existing rows survive.
//
// Columns are discovered from each migration's own SQL (ADD COLUMN statements)
// rather than hardcoded, so this test follows the registry as it grows.
func TestUpgradeFromOlderSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")

	// upgradeFromVersion is the first migration missing from the simulated old
	// deployment. Bump it only if the simulated baseline should move forward.
	const upgradeFromVersion = 15

	const (
		userID    = "user_upgrade_test"
		drinkID   = 9001
		checkinID = "checkin_upgrade_test"
		rating    = 16 // 4.0 stars, stored as quarter-stars
		ts        = "2026-01-02T03:04:05Z"
	)

	first, err := Init(path)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	closed := false
	closeFirst := func() {
		if !closed {
			closed = true
			_ = first.Close()
		}
	}
	defer closeFirst()

	// Representative data that any upgrade must preserve.
	for _, stmt := range []struct {
		name string
		args []any
	}{
		{
			name: `INSERT INTO users (id, handle, email, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			args: []any{userID, "upgrade-test", "upgrade@example.com", ts, ts},
		},
		{
			name: `INSERT INTO drinks (id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			args: []any{drinkID, "Upgrade Cola", "upgrade-cola", ts, ts},
		},
		{
			name: `INSERT INTO checkins (id, rating, created_at, updated_at, drink_id, user_id) VALUES (?, ?, ?, ?, ?, ?)`,
			args: []any{checkinID, rating, ts, ts, drinkID, userID},
		},
	} {
		if _, err := first.SQL.ExecContext(ctx, stmt.name, stmt.args...); err != nil {
			closeFirst()
			t.Fatalf("seed data: %v", err)
		}
	}

	all, err := loadMigrations()
	if err != nil {
		closeFirst()
		t.Fatalf("load migrations: %v", err)
	}
	if len(all) == 0 {
		closeFirst()
		t.Fatal("no embedded migrations")
	}

	// Rewind the newest migrations: drop the columns they added, then forget
	// they ran so Init has to apply them again.
	type columnRef struct{ table, column string }
	var (
		dropped        []columnRef
		droppedIndexes []string
	)
	for _, m := range all {
		if m.version < upgradeFromVersion {
			continue
		}
		for _, match := range addColumnStmt.FindAllStringSubmatch(m.body, -1) {
			table, column := match[1], match[2]
			droppedIndexes = append(droppedIndexes, dropIndexesOnColumn(t, first.SQL, table, column)...)
			if _, err := first.SQL.ExecContext(ctx,
				`ALTER TABLE `+quoteIdent(table)+` DROP COLUMN `+quoteIdent(column)); err != nil {
				closeFirst()
				t.Fatalf("drop column %s.%s: %v", table, column, err)
			}
			dropped = append(dropped, columnRef{table: table, column: column})
		}
	}
	if len(dropped) == 0 {
		closeFirst()
		t.Fatalf("no ADD COLUMN statements found in migrations >= %d; upgrade simulation is vacuous", upgradeFromVersion)
	}
	t.Logf("rewound %d columns from migrations >= %d: %v", len(dropped), upgradeFromVersion, dropped)
	// The rewind has to be real, or the assertions below prove nothing.
	for _, c := range dropped {
		if tableColumnSet(t, first.SQL, c.table)[c.column] {
			closeFirst()
			t.Fatalf("column %s.%s still present after drop", c.table, c.column)
		}
	}
	for _, index := range droppedIndexes {
		if objectExists(t, first.SQL, "index", index) {
			closeFirst()
			t.Fatalf("index %q still present after rewind", index)
		}
	}
	// Drop the @home venue migration 0016 seeds; only a replayed migration can
	// recreate it, since Init itself never inserts venues.
	if _, err := first.SQL.ExecContext(ctx, `DELETE FROM venues WHERE slug = 'at-home'`); err != nil {
		closeFirst()
		t.Fatalf("remove seeded venue: %v", err)
	}

	if _, err := first.SQL.ExecContext(ctx,
		`DELETE FROM schema_migrations WHERE version >= ?`, upgradeFromVersion); err != nil {
		closeFirst()
		t.Fatalf("rewind schema_migrations: %v", err)
	}
	var remaining int
	if err := first.SQL.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations WHERE version >= ?`, upgradeFromVersion).Scan(&remaining); err != nil {
		closeFirst()
		t.Fatalf("verify ledger rewind: %v", err)
	}
	if remaining != 0 {
		closeFirst()
		t.Fatalf("%d migration rows >= %d still recorded after rewind", remaining, upgradeFromVersion)
	}

	closeFirst()

	// Upgrade: Init must rebuild what Ent needs, then replay the pending
	// migrations and restore the captured views.
	second, err := Init(path)
	if err != nil {
		t.Fatalf("reinit (upgrade): %v", err)
	}
	defer second.Close()

	// 1. Ledger: every embedded version recorded exactly once, nothing extra,
	//    and the ledger reaches the newest embedded version.
	applied := map[int]int{}
	rows, err := second.SQL.QueryContext(ctx, `SELECT version, count(*) FROM schema_migrations GROUP BY version`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	for rows.Next() {
		var version, count int
		if err := rows.Scan(&version, &count); err != nil {
			rows.Close()
			t.Fatalf("scan schema_migrations: %v", err)
		}
		applied[version] = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema_migrations: %v", err)
	}

	var maxEmbedded int
	for _, m := range all {
		if applied[m.version] != 1 {
			t.Errorf("migration %04d %s recorded %d times, want exactly 1", m.version, m.name, applied[m.version])
		}
		if m.version > maxEmbedded {
			maxEmbedded = m.version
		}
	}
	if len(applied) != len(all) {
		t.Errorf("schema_migrations has %d distinct versions, want %d", len(applied), len(all))
	}
	var total int
	if err := second.SQL.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&total); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if total != len(all) {
		t.Errorf("schema_migrations has %d rows, want %d", total, len(all))
	}
	// Guard against a future migration that this test would silently skip: the
	// ledger must reach the newest embedded version.
	var maxApplied int
	if err := second.SQL.QueryRowContext(ctx, `SELECT COALESCE(max(version), 0) FROM schema_migrations`).Scan(&maxApplied); err != nil {
		t.Fatalf("max applied version: %v", err)
	}
	if maxApplied != maxEmbedded {
		t.Fatalf("max applied version = %d, newest embedded = %d; a new migration was added and this upgrade test must cover it", maxApplied, maxEmbedded)
	}

	// 2. Columns dropped for the simulation exist again.
	for _, c := range dropped {
		if !tableColumnSet(t, second.SQL, c.table)[c.column] {
			t.Errorf("column %s.%s was not restored by the upgrade", c.table, c.column)
		}
	}
	// Raw indexes are not Ent-managed, so their return proves the migration
	// bodies actually replayed rather than only Ent re-adding columns.
	for _, index := range droppedIndexes {
		if !objectExists(t, second.SQL, "index", index) {
			t.Errorf("index %q was not recreated by the replayed migrations", index)
		}
	}

	// 3. The view survives and reflects the preserved check-in.
	var viewCount int
	if err := second.SQL.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'view' AND name = 'drink_ratings'`).Scan(&viewCount); err != nil {
		t.Fatalf("check drink_ratings view: %v", err)
	}
	if viewCount != 1 {
		t.Errorf("drink_ratings view count = %d, want 1", viewCount)
	}

	var (
		checkinCount int
		raterCount   int
		avgRating    float64
	)
	if err := second.SQL.QueryRowContext(ctx,
		`SELECT checkin_count, rater_count, avg_rating FROM drink_ratings WHERE drink_id = ?`, drinkID).
		Scan(&checkinCount, &raterCount, &avgRating); err != nil {
		t.Fatalf("query drink_ratings: %v", err)
	}
	if checkinCount != 1 || raterCount != 1 {
		t.Errorf("drink_ratings for drink %d = (%d check-ins, %d raters), want (1, 1)", drinkID, checkinCount, raterCount)
	}
	if avgRating != rating {
		t.Errorf("drink_ratings avg_rating = %v, want %v", avgRating, float64(rating))
	}

	// The @home venue deleted during the rewind only comes back if migration
	// 0016's seed statement replayed.
	var homeVenues int
	if err := second.SQL.QueryRowContext(ctx,
		`SELECT count(*) FROM venues WHERE slug = 'at-home'`).Scan(&homeVenues); err != nil {
		t.Fatalf("count home venue: %v", err)
	}
	if homeVenues != 1 {
		t.Errorf("@home venue count = %d, want 1 (migration seed did not replay)", homeVenues)
	}

	// 4. No data loss: the rows seeded before the simulated downgrade survive.
	for _, check := range []struct {
		query string
		args  []any
		want  int
	}{
		{`SELECT count(*) FROM users WHERE id = ?`, []any{userID}, 1},
		{`SELECT count(*) FROM drinks WHERE id = ?`, []any{drinkID}, 1},
		{`SELECT count(*) FROM checkins WHERE id = ? AND rating = ?`, []any{checkinID, rating}, 1},
	} {
		var got int
		if err := second.SQL.QueryRowContext(ctx, check.query, check.args...).Scan(&got); err != nil {
			t.Fatalf("data loss check %q: %v", check.query, err)
		}
		if got != check.want {
			t.Errorf("data loss check %q = %d, want %d", check.query, got, check.want)
		}
	}
}

// tableColumnSet returns the column names currently present on table.
func tableColumnSet(t *testing.T, sqlDB *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := sqlDB.Query(`PRAGMA table_info(` + quoteIdent(table) + `)`)
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer rows.Close()

	cols := map[string]bool{}
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
			t.Fatalf("scan table_info %s: %v", table, err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info %s: %v", table, err)
	}
	return cols
}

// dropIndexesOnColumn drops the non-automatic indexes that reference table's
// column, because SQLite refuses to drop an indexed column. Migrations recreate
// their indexes with IF NOT EXISTS, so this is part of the downgrade simulation.
// It returns the names of the indexes it dropped.
func dropIndexesOnColumn(t *testing.T, sqlDB *sql.DB, table, column string) []string {
	t.Helper()

	rows, err := sqlDB.Query(`PRAGMA index_list(` + quoteIdent(table) + `)`)
	if err != nil {
		t.Fatalf("index_list %s: %v", table, err)
	}
	var indexes []string
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			t.Fatalf("scan index_list %s: %v", table, err)
		}
		// sqlite_autoindex_* backs a UNIQUE constraint and cannot be dropped.
		if strings.HasPrefix(name, "sqlite_autoindex") {
			continue
		}
		indexes = append(indexes, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate index_list %s: %v", table, err)
	}

	var dropped []string
	for _, index := range indexes {
		if !indexUsesColumn(t, sqlDB, index, column) {
			continue
		}
		if _, err := sqlDB.Exec(`DROP INDEX IF EXISTS ` + quoteIdent(index)); err != nil {
			t.Fatalf("drop index %q on %s: %v", index, table, err)
		}
		dropped = append(dropped, index)
	}
	return dropped
}

// indexUsesColumn reports whether index includes column in its key.
func indexUsesColumn(t *testing.T, sqlDB *sql.DB, index, column string) bool {
	t.Helper()
	rows, err := sqlDB.Query(`PRAGMA index_info(` + quoteIdent(index) + `)`)
	if err != nil {
		t.Fatalf("index_info %s: %v", index, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			seqno int
			cid   int
			name  sql.NullString
		)
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			t.Fatalf("scan index_info %s: %v", index, err)
		}
		if name.Valid && name.String == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate index_info %s: %v", index, err)
	}
	return false
}

// objectExists reports whether sqlite_master holds a named object of kind
// ("view", "index", "table", ...).
func objectExists(t *testing.T, sqlDB *sql.DB, kind, name string) bool {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = ? AND name = ?`, kind, name).Scan(&n); err != nil {
		t.Fatalf("check %s %q: %v", kind, name, err)
	}
	return n == 1
}
