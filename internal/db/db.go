package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"sodapop/ent"
)

// DB bundles the raw handle (for FTS and analytical SQL) with the Ent client.
type DB struct {
	SQL    *sql.DB
	Client *ent.Client
	Path   string
}

// Init opens the SQLite database, applies the Ent schema, then runs the
// versioned migration registry for constructs Ent cannot express.
func Init(path string) (*DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	dsn := dsnFor(path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(4)

	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	// Ent's Schema.Create rebuilds tables by creating a new table and renaming
	// it into place. SQLite refuses that rename while a view references the old
	// table, so capture and drop existing views first, then restore them once
	// the schema and migrations have settled. Migrations recreate their own
	// views with CREATE VIEW IF NOT EXISTS, so restoring is idempotent.
	views, err := captureViews(ctx, sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("capture views: %w", err)
	}
	if err := dropViews(ctx, sqlDB, views); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("drop views: %w", err)
	}

	drv := entsql.OpenDB(dialect.SQLite, sqlDB)
	client := ent.NewClient(ent.Driver(&tracingDriver{Driver: drv}))
	if err := client.Schema.Create(ctx); err != nil {
		_ = restoreViews(ctx, sqlDB, views)
		_ = client.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := Migrate(sqlDB); err != nil {
		_ = restoreViews(ctx, sqlDB, views)
		_ = client.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	if err := restoreViews(ctx, sqlDB, views); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("restore views: %w", err)
	}
	return &DB{SQL: sqlDB, Client: client, Path: path}, nil
}

// dbView is a user-defined view captured before a schema rebuild.
type dbView struct {
	name string
	sql  string
}

// captureViews snapshots every non-empty view definition in the database.
func captureViews(ctx context.Context, db *sql.DB) ([]dbView, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, sql FROM sqlite_master WHERE type = 'view' AND sql IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}
	defer rows.Close()

	var views []dbView
	for rows.Next() {
		var v dbView
		if err := rows.Scan(&v.name, &v.sql); err != nil {
			return nil, fmt.Errorf("scan view: %w", err)
		}
		views = append(views, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate views: %w", err)
	}
	return views, nil
}

// dropViews removes captured views so referencing tables can be rebuilt.
func dropViews(ctx context.Context, db *sql.DB, views []dbView) error {
	for _, v := range views {
		if _, err := db.ExecContext(ctx, `DROP VIEW IF EXISTS `+quoteIdent(v.name)); err != nil {
			return fmt.Errorf("drop view %q: %w", v.name, err)
		}
	}
	return nil
}

// restoreViews recreates captured views from their original definitions.
func restoreViews(ctx context.Context, db *sql.DB, views []dbView) error {
	for _, v := range views {
		if _, err := db.ExecContext(ctx, v.sql); err != nil {
			return fmt.Errorf("restore view %q: %w", v.name, err)
		}
	}
	return nil
}

// quoteIdent quotes a SQLite identifier, escaping embedded double quotes.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func dsnFor(path string) string {
	pragmas := "_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(268435456)"
	if path == ":memory:" {
		return "file:sodapop?mode=memory&cache=shared&" + pragmas
	}
	return "file:" + path + "?" + pragmas
}

// Close closes the Ent client and the underlying database.
func (d *DB) Close() error {
	if d == nil {
		return nil
	}
	if d.Client != nil {
		_ = d.Client.Close()
	}
	if d.SQL != nil {
		return d.SQL.Close()
	}
	return nil
}

// Now returns the project-wide RFC3339 UTC timestamp.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
