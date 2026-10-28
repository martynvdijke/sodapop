package backup

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"sodapop/internal/db"
)

func testService(t *testing.T, keep int) (*Service, *db.DB) {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	svc := New(database, filepath.Join(t.TempDir(), "backups"), keep)
	svc.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	return svc, database
}

func TestCreateProducesVerifiedSnapshot(t *testing.T) {
	svc, database := testService(t, 0)
	if svc.Keep != defaultKeep {
		t.Fatalf("Keep = %d, want default %d", svc.Keep, defaultKeep)
	}
	if _, err := database.Client.User.Create().
		SetHandle("backup-user").SetEmail("backup@example.com").SetDisplayName("Backup").
		Save(context.Background()); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	file, err := svc.Create(context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if file.Size <= 0 {
		t.Fatalf("Size = %d, want positive", file.Size)
	}
	info, err := os.Stat(file.Path)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if info.Size() != file.Size {
		t.Fatalf("Size = %d, on disk = %d", file.Size, info.Size())
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}

	handle, err := sql.Open("sqlite", "file:"+file.Path+"?mode=ro")
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer handle.Close()
	var result string
	if err := handle.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		t.Fatalf("integrity query: %v", err)
	}
	if result != "ok" {
		t.Fatalf("integrity = %q, want ok", result)
	}
	var users int
	if err := handle.QueryRow("SELECT count(*) FROM users").Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 1 {
		t.Fatalf("users = %d, want 1", users)
	}
}

func TestCreateDoesNotOverwriteWithinSameSecond(t *testing.T) {
	svc, _ := testService(t, 0)
	first, err := svc.Create(context.Background())
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	second, err := svc.Create(context.Background())
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if first.Name == second.Name || first.Path == second.Path {
		t.Fatalf("second snapshot reused %q", first.Name)
	}
	for _, file := range []File{first, second} {
		if _, err := os.Stat(file.Path); err != nil {
			t.Fatalf("snapshot %s missing: %v", file.Name, err)
		}
	}
	files, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("List length = %d, want 2", len(files))
	}
}

func TestListNewestFirstAndPruneRetention(t *testing.T) {
	svc, _ := testService(t, 2)
	base := time.Now().Add(-time.Hour).UTC()
	var created []File
	for i := 0; i < 5; i++ {
		file, err := svc.Create(context.Background())
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		when := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(file.Path, when, when); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
		file.CreatedAt = when
		created = append(created, file)
	}

	files, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 5 {
		t.Fatalf("List length = %d, want 5", len(files))
	}
	for i := 1; i < len(files); i++ {
		if files[i-1].CreatedAt.Before(files[i].CreatedAt) {
			t.Fatalf("List not newest first at %d: %s before %s", i, files[i-1].CreatedAt, files[i].CreatedAt)
		}
	}

	removed, err := svc.Prune()
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	files, err = svc.List()
	if err != nil {
		t.Fatalf("List after prune: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("after prune length = %d, want 2", len(files))
	}
	want := map[string]bool{created[4].Name: true, created[3].Name: true}
	for _, file := range files {
		if !want[file.Name] {
			t.Fatalf("kept unexpected snapshot %s", file.Name)
		}
	}

	again, err := svc.Prune()
	if err != nil {
		t.Fatalf("second Prune: %v", err)
	}
	if again != 0 {
		t.Fatalf("second prune removed = %d, want 0", again)
	}
}

func TestLastRunToleratesMissingState(t *testing.T) {
	svc, database := testService(t, 0)
	last, err := svc.LastRun(context.Background())
	if err != nil {
		t.Fatalf("LastRun: %v", err)
	}
	if last != "" {
		t.Fatalf("LastRun = %q, want empty", last)
	}

	now := time.Now().UTC().Format(schedulerTimeFormat)
	if _, err := database.SQL.Exec(
		`INSERT INTO scheduler_state (name, last_run_at, lock_owner, updated_at) VALUES (?, ?, '', ?)`,
		backupName, now, now); err != nil {
		t.Fatalf("seed scheduler state: %v", err)
	}
	last, err = svc.LastRun(context.Background())
	if err != nil {
		t.Fatalf("LastRun seeded: %v", err)
	}
	if last != now {
		t.Fatalf("LastRun = %q, want %q", last, now)
	}
}

func TestLastRunMissingTableIsTolerated(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "nostate.db"))
	if err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.SQL.Exec(`DROP TABLE scheduler_state`); err != nil {
		t.Fatalf("drop scheduler_state: %v", err)
	}
	svc := New(database, t.TempDir(), 1)
	last, err := svc.LastRun(context.Background())
	if err != nil {
		t.Fatalf("LastRun: %v", err)
	}
	if last != "" {
		t.Fatalf("LastRun = %q, want empty", last)
	}
}
