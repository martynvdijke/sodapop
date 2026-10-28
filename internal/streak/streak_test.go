package streak

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "streak.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE checkins (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			deleted_at TEXT
		)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func insertCheckin(t *testing.T, db *sql.DB, id, userID, created string, deleted bool) {
	t.Helper()
	var deletedAt any
	if deleted {
		deletedAt = created
	}
	if _, err := db.Exec(`
		INSERT INTO checkins (id, user_id, created_at, deleted_at) VALUES (?, ?, ?, ?)`,
		id, userID, created, deletedAt); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestCurrent(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		days []string // dates relative to now; each is an RFC3339 timestamp offset in days
		want int
	}{
		{name: "no checkins", days: nil, want: 0},
		{name: "today only", days: []string{"2026-10-28T09:00:00Z"}, want: 1},
		{name: "run ending today", days: []string{
			"2026-10-28T09:00:00Z", "2026-10-27T09:00:00Z", "2026-10-26T23:30:00Z",
		}, want: 3},
		{name: "run ending yesterday still counts", days: []string{
			"2026-10-27T09:00:00Z", "2026-10-26T09:00:00Z",
		}, want: 2},
		{name: "gap before yesterday resets", days: []string{
			"2026-10-27T09:00:00Z", "2026-10-25T09:00:00Z", "2026-10-28T09:00:00Z",
		}, want: 2},
		{name: "only old run is zero", days: []string{
			"2026-10-20T09:00:00Z", "2026-10-21T09:00:00Z",
		}, want: 0},
		{name: "multiple checkins same day count once", days: []string{
			"2026-10-28T01:00:00Z", "2026-10-28T20:00:00Z", "2026-10-27T05:00:00Z",
		}, want: 2},
		{name: "yesterday only", days: []string{"2026-10-27T09:00:00Z"}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openDB(t)
			for i, created := range tt.days {
				insertCheckin(t, db, "id-"+string(rune('a'+i)), "u1", created, false)
			}
			got, err := Current(context.Background(), db, "u1", now)
			if err != nil {
				t.Fatalf("Current: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Current = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCurrentIgnoresDeletedAndOtherUsers(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	db := openDB(t)
	insertCheckin(t, db, "a", "u1", "2026-10-28T09:00:00Z", false)
	insertCheckin(t, db, "b", "u1", "2026-10-27T09:00:00Z", true)
	insertCheckin(t, db, "c", "u2", "2026-10-27T09:00:00Z", false)

	got, err := Current(context.Background(), db, "u1", now)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got != 1 {
		t.Fatalf("Current = %d, want 1", got)
	}
}

func TestLongest(t *testing.T) {
	tests := []struct {
		name string
		days []string
		want int
	}{
		{name: "no checkins", days: nil, want: 0},
		{name: "single day", days: []string{"2026-10-28T09:00:00Z"}, want: 1},
		{name: "one run", days: []string{
			"2026-10-01T09:00:00Z", "2026-10-03T09:00:00Z", "2026-10-02T09:00:00Z",
		}, want: 3},
		{name: "longest is not current", days: []string{
			"2026-09-01T09:00:00Z", "2026-09-02T09:00:00Z", "2026-09-03T09:00:00Z",
			"2026-09-04T09:00:00Z", "2026-10-28T09:00:00Z",
		}, want: 4},
		{name: "duplicate days collapse", days: []string{
			"2026-10-27T09:00:00Z", "2026-10-27T22:00:00Z", "2026-10-28T09:00:00Z",
		}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openDB(t)
			for i, created := range tt.days {
				insertCheckin(t, db, "id-"+string(rune('a'+i)), "u1", created, false)
			}
			got, err := Longest(context.Background(), db, "u1")
			if err != nil {
				t.Fatalf("Longest: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Longest = %d, want %d", got, tt.want)
			}
		})
	}
}
