package social_test

import (
	"testing"

	"sodapop/internal/db"
)

func TestByIDsPreservesInputOrderAndSkipsDeleted(t *testing.T) {
	ctx, database, svc, alice, _, drink := newToastFixture(t)
	first := mustCheckin(t, ctx, database, alice, drink, "public")
	second := mustCheckin(t, ctx, database, alice, drink, "public")
	third := mustCheckin(t, ctx, database, alice, drink, "public")

	if err := database.Client.Checkin.UpdateOneID(second.ID).
		SetDeletedAt(db.Now()).Exec(ctx); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// Deliberately unordered and containing a deleted row.
	got, err := svc.ByIDs(ctx, alice.ID, []string{third.ID, second.ID, first.ID})
	if err != nil {
		t.Fatalf("ByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != third.ID || got[1].ID != first.ID {
		t.Fatalf("ids = [%s %s], want [%s %s]", got[0].ID, got[1].ID, third.ID, first.ID)
	}
}

func TestByIDsEmptyReturnsEmpty(t *testing.T) {
	ctx, _, svc, alice, _, _ := newToastFixture(t)
	got, err := svc.ByIDs(ctx, alice.ID, nil)
	if err != nil {
		t.Fatalf("ByIDs: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %v, want empty non-nil slice", got)
	}
}
