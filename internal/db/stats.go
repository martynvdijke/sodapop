package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

// Stats is a point-in-time view of the SQLite file and its migration state.
type Stats struct {
	Path          string
	Size          int64
	WALSize       int64
	PageCount     int
	PageSize      int
	SchemaVersion int
	SchemaName    string
}

// Stats reads page accounting, the applied migration version and on-disk file
// sizes. A missing WAL file reports zero bytes.
func (d *DB) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{}
	if d == nil || d.SQL == nil {
		return stats, errors.New("db: database is not configured")
	}
	stats.Path = d.Path

	if err := d.SQL.QueryRowContext(ctx, "PRAGMA page_count").Scan(&stats.PageCount); err != nil {
		return stats, fmt.Errorf("db: read page_count: %w", err)
	}
	if err := d.SQL.QueryRowContext(ctx, "PRAGMA page_size").Scan(&stats.PageSize); err != nil {
		return stats, fmt.Errorf("db: read page_size: %w", err)
	}

	err := d.SQL.QueryRowContext(ctx,
		`SELECT version, name FROM schema_migrations ORDER BY version DESC LIMIT 1`).
		Scan(&stats.SchemaVersion, &stats.SchemaName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return stats, fmt.Errorf("db: read schema_migrations: %w", err)
	}

	if d.Path != "" && d.Path != ":memory:" {
		if info, err := os.Stat(d.Path); err == nil {
			stats.Size = info.Size()
		} else if !errors.Is(err, os.ErrNotExist) {
			return stats, fmt.Errorf("db: stat database: %w", err)
		}
		if info, err := os.Stat(d.Path + "-wal"); err == nil {
			stats.WALSize = info.Size()
		} else if !errors.Is(err, os.ErrNotExist) {
			return stats, fmt.Errorf("db: stat wal: %w", err)
		}
	}
	return stats, nil
}
