package social_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/user"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

// newToastFixture builds two users and a drink for toast visibility tests.
func newToastFixture(t *testing.T) (context.Context, *db.DB, *social.Service, *ent.User, *ent.User, *ent.Drink) {
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
	drink, err := database.Client.Drink.Create().
		SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	return ctx, database, social.New(database), mk("alice", "Alice"), mk("bob", "Bob"), drink
}

func mustCheckin(t *testing.T, ctx context.Context, database *db.DB, owner *ent.User, drink *ent.Drink, visibility string) *ent.Checkin {
	t.Helper()
	c, err := database.Client.Checkin.Create().
		SetUserID(owner.ID).
		SetDrinkID(drink.ID).
		SetVisibility(checkin.Visibility(visibility)).
		Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func TestToastRejectsPrivateCheckinFromStranger(t *testing.T) {
	ctx, database, svc, alice, bob, drink := newToastFixture(t)
	c := mustCheckin(t, ctx, database, alice, drink, "private")

	if err := svc.Toast(ctx, bob.ID, c.ID); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("stranger toast: got %v, want ErrNotFound", err)
	}
	n, err := database.Client.CheckinLike.Query().Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("likes = %d, want 0", n)
	}
}

func TestToastRejectsCheckinFromDeletedAuthor(t *testing.T) {
	ctx, database, svc, alice, bob, drink := newToastFixture(t)
	c := mustCheckin(t, ctx, database, alice, drink, "public")

	if err := database.Client.User.UpdateOneID(alice.ID).
		SetStatus(user.StatusDeleted).
		SetDeletedAt(db.Now()).
		Exec(ctx); err != nil {
		t.Fatalf("delete author: %v", err)
	}
	if err := svc.Toast(ctx, bob.ID, c.ID); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("deleted author toast: got %v, want ErrNotFound", err)
	}
}

func TestToastAllowsVisiblePublicCheckin(t *testing.T) {
	ctx, database, svc, alice, bob, drink := newToastFixture(t)
	c := mustCheckin(t, ctx, database, alice, drink, "public")

	if err := svc.Toast(ctx, bob.ID, c.ID); err != nil {
		t.Fatalf("public toast: %v", err)
	}
	// The notification still reaches the author.
	rows, err := database.Client.Notification.Query().All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UserID != alice.ID {
		t.Fatalf("notifications = %+v, want one for alice", rows)
	}
}
