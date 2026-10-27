package saves_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/checkinsave"
	"sodapop/internal/checkins"
	"sodapop/internal/db"
	"sodapop/internal/saves"
	"sodapop/internal/social"
)

type fixture struct {
	ctx   context.Context
	db    *db.DB
	svc   *saves.Service
	alice *ent.User
	bob   *ent.User
	cola  *ent.Drink
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	mk := func(handle, name string) *ent.User {
		u, err := database.Client.User.Create().
			SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(name).Save(ctx)
		if err != nil {
			t.Fatalf("user %s: %v", handle, err)
		}
		return u
	}
	drink, err := database.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	checkinSvc := checkins.New(database, social.New(database))
	return &fixture{ctx: ctx, db: database, svc: saves.New(database, checkinSvc),
		alice: mk("alice", "Alice"), bob: mk("bob", "Bob"), cola: drink}
}

func (f *fixture) checkin(t *testing.T, owner *ent.User, visibility string) *ent.Checkin {
	t.Helper()
	c, err := f.db.Client.Checkin.Create().
		SetUserID(owner.ID).
		SetDrinkID(f.cola.ID).
		SetVisibility(checkin.Visibility(visibility)).
		Save(f.ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

// saveAt saves a check-in and pins created_at so ordering is deterministic
// even when several rows are written inside one second.
func (f *fixture) saveAt(t *testing.T, user *ent.User, checkinID, createdAt string) string {
	t.Helper()
	if err := f.svc.Save(f.ctx, user.ID, checkinID); err != nil {
		t.Fatalf("save: %v", err)
	}
	row, err := f.db.Client.CheckinSave.Query().
		Where(checkinsave.UserID(user.ID), checkinsave.CheckinID(checkinID)).Only(f.ctx)
	if err != nil {
		t.Fatalf("load save: %v", err)
	}
	if _, err := f.db.SQL.ExecContext(f.ctx,
		`UPDATE checkin_saves SET created_at = ? WHERE id = ?`, createdAt, row.ID); err != nil {
		t.Fatalf("pin created_at: %v", err)
	}
	return row.ID
}

func (f *fixture) saveCount(t *testing.T, userID, checkinID string) int {
	t.Helper()
	n, err := f.db.Client.CheckinSave.Query().
		Where(checkinsave.UserID(userID), checkinsave.CheckinID(checkinID)).Count(f.ctx)
	if err != nil {
		t.Fatalf("count saves: %v", err)
	}
	return n
}

func TestSaveAndUnsaveAreIdempotent(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, f.alice, "public")

	for i := 0; i < 2; i++ {
		if err := f.svc.Save(f.ctx, f.bob.ID, c.ID); err != nil {
			t.Fatalf("save #%d: %v", i+1, err)
		}
	}
	if n := f.saveCount(t, f.bob.ID, c.ID); n != 1 {
		t.Fatalf("saves after double save = %d, want 1", n)
	}
	saved, err := f.svc.IsSaved(f.ctx, f.bob.ID, c.ID)
	if err != nil || !saved {
		t.Fatalf("IsSaved = %v, %v; want true, nil", saved, err)
	}

	for i := 0; i < 2; i++ {
		if err := f.svc.Unsave(f.ctx, f.bob.ID, c.ID); err != nil {
			t.Fatalf("unsave #%d: %v", i+1, err)
		}
	}
	if n := f.saveCount(t, f.bob.ID, c.ID); n != 0 {
		t.Fatalf("saves after double unsave = %d, want 0", n)
	}
	saved, err = f.svc.IsSaved(f.ctx, f.bob.ID, c.ID)
	if err != nil || saved {
		t.Fatalf("IsSaved = %v, %v; want false, nil", saved, err)
	}
}

func TestSaveAllowsSelfSave(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, f.alice, "private")

	if err := f.svc.Save(f.ctx, f.alice.ID, c.ID); err != nil {
		t.Fatalf("self save: %v", err)
	}
	if n := f.saveCount(t, f.alice.ID, c.ID); n != 1 {
		t.Fatalf("saves = %d, want 1", n)
	}
}

func TestSaveRejectsInvisibleCheckin(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, f.alice, "private")

	if err := f.svc.Save(f.ctx, f.bob.ID, c.ID); !errors.Is(err, saves.ErrNotFound) {
		t.Fatalf("save private check-in: got %v, want ErrNotFound", err)
	}
	if n := f.saveCount(t, f.bob.ID, c.ID); n != 0 {
		t.Fatalf("saves = %d, want 0", n)
	}
}

func TestSaveRejectsEmptyInput(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.Save(f.ctx, "", "x"); !errors.Is(err, saves.ErrInvalid) {
		t.Fatalf("empty user: got %v, want ErrInvalid", err)
	}
	if err := f.svc.Save(f.ctx, f.bob.ID, " "); !errors.Is(err, saves.ErrInvalid) {
		t.Fatalf("empty check-in: got %v, want ErrInvalid", err)
	}
}

func TestListIDsSkipsInvisibleAndAdvancesCursor(t *testing.T) {
	f := newFixture(t)
	oldest := f.checkin(t, f.alice, "public")
	middle := f.checkin(t, f.alice, "public")
	newest := f.checkin(t, f.alice, "public")

	f.saveAt(t, f.bob, oldest.ID, "2026-01-01T00:00:01Z")
	f.saveAt(t, f.bob, middle.ID, "2026-01-01T00:00:02Z")
	f.saveAt(t, f.bob, newest.ID, "2026-01-01T00:00:03Z")
	// The middle save is now invisible: it must be skipped, but the cursor
	// must still walk past it.
	if _, err := f.db.SQL.ExecContext(f.ctx,
		`UPDATE checkins SET deleted_at = ? WHERE id = ?`, "2026-01-02T00:00:00Z", middle.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	ids, hasMore, nextBefore, err := f.svc.ListIDs(f.ctx, f.bob.ID, 10, "")
	if err != nil {
		t.Fatalf("ListIDs: %v", err)
	}
	if hasMore {
		t.Fatal("hasMore = true, want false")
	}
	if nextBefore == "" {
		t.Fatal("nextBefore empty")
	}
	if len(ids) != 2 || ids[0] != newest.ID || ids[1] != oldest.ID {
		t.Fatalf("ids = %v, want [%s %s]", ids, newest.ID, oldest.ID)
	}

	// Paging one at a time must terminate even when a whole page is invisible.
	var seen []string
	cursor := ""
	for page := 0; page < 5; page++ {
		pageIDs, more, next, err := f.svc.ListIDs(f.ctx, f.bob.ID, 1, cursor)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		seen = append(seen, pageIDs...)
		if !more {
			break
		}
		if next == cursor || next == "" {
			t.Fatalf("page %d: cursor did not advance (%q -> %q)", page, cursor, next)
		}
		cursor = next
	}
	if len(seen) != 2 || seen[0] != newest.ID || seen[1] != oldest.ID {
		t.Fatalf("paged ids = %v, want [%s %s]", seen, newest.ID, oldest.ID)
	}
}

func TestListIDsRejectsUnknownCursor(t *testing.T) {
	f := newFixture(t)
	if _, _, _, err := f.svc.ListIDs(f.ctx, f.bob.ID, 20, "no-such-save"); !errors.Is(err, saves.ErrInvalid) {
		t.Fatalf("unknown cursor: got %v, want ErrInvalid", err)
	}
}

func TestSavedIDsIsBatchAndEmptySafe(t *testing.T) {
	f := newFixture(t)
	a := f.checkin(t, f.alice, "public")
	b := f.checkin(t, f.alice, "public")
	other := f.checkin(t, f.alice, "public")
	if err := f.svc.Save(f.ctx, f.bob.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Save(f.ctx, f.bob.ID, b.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.SavedIDs(f.ctx, f.bob.ID, []string{a.ID, b.ID, other.ID})
	if err != nil {
		t.Fatalf("SavedIDs: %v", err)
	}
	if !got[a.ID] || !got[b.ID] || got[other.ID] {
		t.Fatalf("SavedIDs = %v, want only a and b", got)
	}

	empty, err := f.svc.SavedIDs(f.ctx, f.bob.ID, nil)
	if err != nil {
		t.Fatalf("empty SavedIDs: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty SavedIDs = %v, want empty", empty)
	}
}
