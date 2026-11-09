package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// addColumnStmt matches one `ALTER TABLE <table> ADD COLUMN <column> ...;`
// statement. Ent's Schema.Create runs before the registry and already adds
// columns declared on an Ent schema, so an ALTER for a column that exists (a
// fresh database, or an upgrade Ent already migrated) would otherwise abort the
// migration with "duplicate column name".
var addColumnStmt = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(\w+)\s+ADD\s+COLUMN\s+(\w+)[^;]*;`)

// stripExistingAddColumns removes ADD COLUMN statements whose target column is
// already present. Other statements are left untouched.
func stripExistingAddColumns(sqlDB *sql.DB, body string) (string, error) {
	var firstErr error
	out := addColumnStmt.ReplaceAllStringFunc(body, func(stmt string) string {
		if firstErr != nil {
			return stmt
		}
		m := addColumnStmt.FindStringSubmatch(stmt)
		exists, err := columnExists(sqlDB, m[1], m[2])
		if err != nil {
			firstErr = err
			return stmt
		}
		if exists {
			return ""
		}
		return stmt
	})
	return out, firstErr
}

func columnExists(sqlDB *sql.DB, table, column string) (bool, error) {
	rows, err := sqlDB.Query(`PRAGMA table_info("` + table + `")`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
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
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	body    string
}

// Migrate applies every pending versioned migration exactly once. Each
// migration runs in its own transaction and is recorded in schema_migrations.
func Migrate(sqlDB *sql.DB) error {
	if _, err := sqlDB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL
	);`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := sqlDB.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		body, err := stripExistingAddColumns(sqlDB, m.body)
		if err != nil {
			return fmt.Errorf("migration %04d %s: inspect columns: %w", m.version, m.name, err)
		}
		tx, err := sqlDB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(body); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %04d %s: %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			m.version, m.name, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		idx := strings.IndexByte(base, '_')
		if idx != 4 {
			return nil, fmt.Errorf("invalid migration name %q", e.Name())
		}
		v, err := strconv.Atoi(base[:idx])
		if err != nil {
			return nil, fmt.Errorf("invalid migration version %q: %w", e.Name(), err)
		}
		body, err := migrationFS.ReadFile(path.Join("migrations", e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: e.Name(), body: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}
