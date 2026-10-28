// Command restore replaces a Sodapop database with a verified snapshot.
//
// It is intentionally offline: stop the server before running it. There is no
// HTTP restore endpoint.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var requiredTables = []string{"users", "checkins", "drinks"}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "restore failed:", err)
		os.Exit(1)
	}
}

func run() error {
	from := flag.String("from", "", "snapshot file to restore from")
	to := flag.String("db", "", "target database path")
	yes := flag.Bool("yes", false, "confirm replacing the target database")
	flag.Parse()

	if strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
		return errors.New("-from and -db are required")
	}
	if !*yes {
		return errors.New("refusing to overwrite the database without -yes; stop the server first")
	}
	if _, err := os.Stat(*from); err != nil {
		return fmt.Errorf("source snapshot: %w", err)
	}
	if err := verifySnapshot(*from); err != nil {
		return err
	}

	fmt.Println("WARNING: stop the Sodapop server before restoring. Writing while it runs")
	fmt.Println("can corrupt the database or serve stale pages.")
	fmt.Printf("Restoring %s -> %s\n", *from, *to)

	if err := atomicCopy(*from, *to); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(*to + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale %s: %w", suffix, err)
		}
	}
	fmt.Println("restore complete; start the server again.")
	return nil
}

func verifySnapshot(path string) error {
	handle, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open snapshot: %w", err)
	}
	defer handle.Close()

	ctx := context.Background()
	var result string
	if err := handle.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(result), "ok") {
		return fmt.Errorf("snapshot failed integrity check: %s", result)
	}

	for _, table := range requiredTables {
		var count int
		if err := handle.QueryRowContext(ctx,
			`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			return fmt.Errorf("inspect snapshot tables: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("snapshot is missing required table %q", table)
		}
	}
	return nil
}

// atomicCopy writes a temporary file in the target directory, flushes it, then
// renames it over the destination so a partial copy never becomes the live DB.
func atomicCopy(from, to string) error {
	dir := filepath.Dir(to)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}
	tmp := to + ".restore-tmp"
	src, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary copy: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy snapshot: %w", err)
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("flush copy: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close copy: %w", err)
	}

	if err := os.Rename(tmp, to); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace target: %w", err)
	}

	// Flush the directory entry so the rename survives a crash.
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
	return nil
}
