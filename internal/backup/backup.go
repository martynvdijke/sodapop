// Package backup creates, lists and prunes consistent SQLite snapshots.
package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"sodapop/internal/db"
)

const (
	defaultKeep = 14
	backupName  = "backup"
	// schedulerTimeFormat keeps nanosecond precision so lexical comparison of
	// the RFC 3339 timestamps in SQLite stays correct.
	schedulerTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"
	lockTTL             = 10 * time.Minute
	minInterval         = 24 * time.Hour
	tickInterval        = time.Hour
)

// Service manages the backup directory for one database.
type Service struct {
	DB   *db.DB
	Dir  string
	Keep int
	Log  *slog.Logger
}

// File describes one snapshot on disk.
type File struct {
	Name      string
	Path      string
	Size      int64
	CreatedAt time.Time
}

// New returns a backup service. A non-positive keep falls back to 14.
func New(database *db.DB, dir string, keep int) *Service {
	if keep <= 0 {
		keep = defaultKeep
	}
	return &Service{DB: database, Dir: dir, Keep: keep, Log: slog.Default()}
}

func (s *Service) logger() *slog.Logger {
	if s != nil && s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Create writes a consistent snapshot with VACUUM INTO, verifies it, and
// returns its metadata. The destination is never overwritten.
func (s *Service) Create(ctx context.Context) (File, error) {
	if s == nil || s.DB == nil || s.DB.SQL == nil {
		return File{}, errors.New("backup: database is not configured")
	}
	if s.Dir == "" {
		return File{}, errors.New("backup: directory is not configured")
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return File{}, fmt.Errorf("backup: create directory: %w", err)
	}

	path, err := s.nextPath(time.Now().UTC())
	if err != nil {
		return File{}, err
	}

	quoted := strings.ReplaceAll(path, "'", "''")
	if _, err := s.DB.SQL.ExecContext(ctx, "VACUUM INTO '"+quoted+"'"); err != nil {
		return File{}, fmt.Errorf("backup: vacuum into snapshot: %w", err)
	}
	// Snapshots contain the full database; keep them owner-only regardless of
	// the process umask.
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return File{}, fmt.Errorf("backup: restrict snapshot permissions: %w", err)
	}

	if err := verifyFile(ctx, path); err != nil {
		_ = os.Remove(path)
		return File{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return File{}, fmt.Errorf("backup: stat snapshot: %w", err)
	}
	return File{Name: filepath.Base(path), Path: path, Size: info.Size(), CreatedAt: info.ModTime().UTC()}, nil
}

// nextPath returns a free path for the given second, adding a numeric suffix
// when a snapshot from the same second already exists.
func (s *Service) nextPath(now time.Time) (string, error) {
	base := "sodapop-" + now.Format("20060102-150405")
	for i := 0; i < 1000; i++ {
		name := base + ".db"
		if i > 0 {
			name = fmt.Sprintf("%s-%d.db", base, i)
		}
		path := filepath.Join(s.Dir, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", fmt.Errorf("backup: inspect target: %w", err)
		}
	}
	return "", fmt.Errorf("backup: no free snapshot name for %s", base)
}

// verifyFile opens the snapshot read-only and requires a clean integrity check.
func verifyFile(ctx context.Context, path string) error {
	handle, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("backup: open snapshot: %w", err)
	}
	defer handle.Close()

	var result string
	if err := handle.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("backup: integrity check: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(result), "ok") {
		return fmt.Errorf("backup: integrity check failed: %s", result)
	}
	return nil
}

// List returns snapshots newest first.
func (s *Service) List() ([]File, error) {
	if s == nil || s.Dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("backup: list directory: %w", err)
	}
	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		path := filepath.Join(s.Dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("backup: stat %s: %w", entry.Name(), err)
		}
		files = append(files, File{Name: entry.Name(), Path: path, Size: info.Size(), CreatedAt: info.ModTime().UTC()})
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].CreatedAt.Equal(files[j].CreatedAt) {
			return files[i].CreatedAt.After(files[j].CreatedAt)
		}
		return files[i].Name > files[j].Name
	})
	return files, nil
}

// Prune keeps the newest Keep snapshots and removes the rest. It returns the
// number of files removed.
func (s *Service) Prune() (int, error) {
	files, err := s.List()
	if err != nil {
		return 0, err
	}
	if s.Keep <= 0 || len(files) <= s.Keep {
		return 0, nil
	}
	removed := 0
	for _, file := range files[s.Keep:] {
		if err := os.Remove(file.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("backup: remove %s: %w", file.Name, err)
		}
		removed++
	}
	return removed, nil
}

// LastRun returns the last successful snapshot timestamp recorded in
// scheduler_state, or an empty string when none has run. A missing table or
// row is treated as "never".
func (s *Service) LastRun(ctx context.Context) (string, error) {
	if s == nil || s.DB == nil || s.DB.SQL == nil {
		return "", nil
	}
	var raw sql.NullString
	err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT last_run_at FROM scheduler_state WHERE name = ?`, backupName).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err != nil && strings.Contains(err.Error(), "no such table")) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("backup: read last run: %w", err)
	}
	if !raw.Valid {
		return "", nil
	}
	return raw.String, nil
}

// Run takes a snapshot at startup and then at most once every 24 hours until
// the context is cancelled. Failures are logged and never stop the server.
func (s *Service) Run(ctx context.Context) {
	s.runOnce(ctx)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Service) runOnce(ctx context.Context) {
	if s == nil || s.DB == nil {
		return
	}
	last, err := s.LastRun(ctx)
	if err != nil {
		s.logger().Warn("backup: last run unavailable", "err", err)
	}
	if raw := strings.TrimSpace(last); raw != "" {
		when, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			s.logger().Warn("backup: invalid last run timestamp", "value", raw, "err", err)
		} else if time.Since(when.UTC()) < minInterval {
			return
		}
	}

	now := time.Now().UTC()
	owner := newOwner()
	acquired, err := s.acquire(ctx, owner, now)
	if err != nil {
		s.logger().Warn("backup: lease unavailable", "err", err)
		return
	}
	if !acquired {
		return
	}

	file, createErr := s.Create(ctx)
	if createErr == nil {
		if removed, err := s.Prune(); err != nil {
			s.logger().Warn("backup: prune failed", "err", err)
		} else if removed > 0 {
			s.logger().Info("backup: pruned old snapshots", "removed", removed)
		}
	}

	if err := s.release(ctx, owner, now, createErr == nil); err != nil {
		s.logger().Warn("backup: lease release failed", "err", err)
	}
	if createErr != nil {
		s.logger().Error("backup: snapshot failed", "err", createErr)
		return
	}
	s.logger().Info("backup: snapshot created", "file", file.Name, "size", file.Size)
}

func (s *Service) acquire(ctx context.Context, owner string, now time.Time) (bool, error) {
	res, err := s.DB.SQL.ExecContext(ctx, `
		INSERT INTO scheduler_state (name, lock_owner, locked_until, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			lock_owner = excluded.lock_owner,
			locked_until = excluded.locked_until,
			updated_at = excluded.updated_at
		WHERE scheduler_state.locked_until IS NULL OR scheduler_state.locked_until < ?`,
		backupName, owner, schedulerTime(now.Add(lockTTL)), schedulerTime(now), schedulerTime(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Service) release(ctx context.Context, owner string, now time.Time, success bool) error {
	relCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if success {
		_, err := s.DB.SQL.ExecContext(relCtx, `
			UPDATE scheduler_state
			SET last_run_at = ?, locked_until = NULL, lock_owner = '', updated_at = ?
			WHERE name = ? AND lock_owner = ?`,
			schedulerTime(now), schedulerTime(now), backupName, owner)
		return err
	}
	_, err := s.DB.SQL.ExecContext(relCtx, `
		UPDATE scheduler_state
		SET locked_until = NULL, lock_owner = '', updated_at = ?
		WHERE name = ? AND lock_owner = ?`,
		schedulerTime(now), backupName, owner)
	return err
}

func schedulerTime(t time.Time) string {
	return t.UTC().Format(schedulerTimeFormat)
}

func newOwner() string {
	return fmt.Sprintf("backup-%d", time.Now().UnixNano())
}
