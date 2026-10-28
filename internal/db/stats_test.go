package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStatsReportsFileAndMigrationState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.db")
	database, err := Init(path)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	stats, err := database.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Path != path {
		t.Fatalf("Path = %q, want %q", stats.Path, path)
	}
	if stats.PageCount <= 0 || stats.PageSize <= 0 {
		t.Fatalf("page accounting = %d pages of %d bytes, want positive", stats.PageCount, stats.PageSize)
	}
	if stats.Size != int64(stats.PageCount*stats.PageSize) && stats.WALSize == 0 {
		// WAL mode means the main file can lag the page total; still require
		// that some bytes are visible either in the file or the WAL.
		t.Fatalf("Size = %d with no WAL for %d pages", stats.Size, stats.PageCount)
	}
	if stats.SchemaVersion < 8 {
		t.Fatalf("SchemaVersion = %d, want at least 8", stats.SchemaVersion)
	}
	if stats.SchemaName == "" {
		t.Fatal("SchemaName is empty")
	}
	if stats.WALSize < 0 {
		t.Fatalf("WALSize = %d, want non-negative", stats.WALSize)
	}
}

func TestStatsOnMemoryDatabase(t *testing.T) {
	database, err := Init(":memory:")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	stats, err := database.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.PageCount <= 0 || stats.PageSize <= 0 {
		t.Fatalf("page accounting = %d pages of %d bytes, want positive", stats.PageCount, stats.PageSize)
	}
	if stats.Size != 0 || stats.WALSize != 0 {
		t.Fatalf("file sizes = %d/%d, want zero for in-memory", stats.Size, stats.WALSize)
	}
}
