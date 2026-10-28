package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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

	drv := entsql.OpenDB(dialect.SQLite, sqlDB)
	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := Migrate(sqlDB); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return &DB{SQL: sqlDB, Client: client, Path: path}, nil
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
