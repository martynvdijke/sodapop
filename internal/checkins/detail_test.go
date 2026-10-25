package checkins_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkinlike"
	"sodapop/ent/friendship"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

type testEnv struct {
	ctx   context.Context
	db    *db.DB
	svc   *checkins.Service
	alice *ent.User
	bob   *ent.User
	carol *ent.User
	drink *ent.Drink
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()

	alice := createUser(t, ctx, database.Client, "alice", "Alice")
	bob := createUser(t, ctx, database.Client, "bob", "Bob")
	carol := createUser(t, ctx, database.Client, "carol", "Carol")

	drink, err := database.Client.Drink.Create().
		SetName("Ginger Beer").SetSlug("ginger-beer").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	return &testEnv{ctx: ctx, db: database, svc: checkins.New(database, social.New(database)),
		alice: alice, bob: bob, carol: carol, drink: drink}
}

func createUser(t *testing.T, ctx context.Context, client *ent.Client, handle, name string) *ent.User {
	t.Helper()
	u, err := client.User.Create().
		SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(name).Save(ctx)
	if err != nil {
		t.Fatalf("user %s: %v", handle, err)
	}
	return u
}

func (e *testEnv) createCheckin(t *testing.T, owner *ent.User, visibility string) *ent.Checkin {
	t.Helper()
	c, _, err := e.svc.Create(e.ctx, owner, checkins.CreateInput{
		DrinkID: e.drink.ID, Visibility: visibility,
	})
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func (e *testEnv) befriend(t *testing.T, a, b *ent.User) {
	t.Helper()
	x, y := a.ID, b.ID
	if x > y {
		x, y = y, x
	}
	if _, err := e.db.Client.Friendship.Create().
		SetUserA(x).SetUserB(y).
		SetStatus(friendship.StatusAccepted).
		SetRequestedBy(a.ID).Save(e.ctx); err != nil {
		t.Fatalf("friendship: %v", err)
	}
}

func TestGetVisibility(t *testing.T) {
	e := newTestEnv(t)

	t.Run("owner sees private", func(t *testing.T) {
		c := e.createCheckin(t, e.alice, "private")
		got, err := e.svc.Get(e.ctx, e.alice.ID, c.ID)
		if err != nil {
			t.Fatalf("owner: %v", err)
		}
		if got.ID != c.ID || got.Visibility != "private" || got.UserHandle != "alice" {
			t.Fatalf("unexpected detail: %+v", got)
		}
		if got.DrinkSlug != "ginger-beer" || got.DrinkName != "Ginger Beer" {
			t.Fatalf("unexpected drink: %+v", got)
		}
	})

	t.Run("anonymous only sees public", func(t *testing.T) {
		pub := e.createCheckin(t, e.alice, "public")
		if _, err := e.svc.Get(e.ctx, "", pub.ID); err != nil {
			t.Fatalf("public: %v", err)
		}
		for _, v := range []string{"friends", "private"} {
			c := e.createCheckin(t, e.alice, v)
			if _, err := e.svc.Get(e.ctx, "", c.ID); !errors.Is(err, checkins.ErrNotFound) {
				t.Fatalf("%s anonymous: got %v, want ErrNotFound", v, err)
			}
		}
	})

	t.Run("friend sees friends-only", func(t *testing.T) {
		e.befriend(t, e.alice, e.bob)
		c := e.createCheckin(t, e.alice, "friends")
		if _, err := e.svc.Get(e.ctx, e.bob.ID, c.ID); err != nil {
			t.Fatalf("friend: %v", err)
		}
	})

	t.Run("stranger is refused", func(t *testing.T) {
		c := e.createCheckin(t, e.alice, "friends")
		if _, err := e.svc.Get(e.ctx, e.carol.ID, c.ID); !errors.Is(err, checkins.ErrNotFound) {
			t.Fatalf("stranger friends: got %v, want ErrNotFound", err)
		}
		p := e.createCheckin(t, e.alice, "private")
		if _, err := e.svc.Get(e.ctx, e.carol.ID, p.ID); !errors.Is(err, checkins.ErrNotFound) {
			t.Fatalf("stranger private: got %v, want ErrNotFound", err)
		}
	})

	t.Run("soft-deleted is not found", func(t *testing.T) {
		c := e.createCheckin(t, e.alice, "public")
		if err := e.db.Client.Checkin.UpdateOneID(c.ID).SetDeletedAt(db.Now()).Exec(e.ctx); err != nil {
			t.Fatalf("soft delete: %v", err)
		}
		if _, err := e.svc.Get(e.ctx, e.alice.ID, c.ID); !errors.Is(err, checkins.ErrNotFound) {
			t.Fatalf("deleted: got %v, want ErrNotFound", err)
		}
	})

	t.Run("missing row is not found", func(t *testing.T) {
		if _, err := e.svc.Get(e.ctx, e.alice.ID, "does-not-exist"); !errors.Is(err, checkins.ErrNotFound) {
			t.Fatalf("missing: got %v, want ErrNotFound", err)
		}
	})
}

func TestGetHidesDeletedAuthor(t *testing.T) {
	e := newTestEnv(t)
	c := e.createCheckin(t, e.alice, "public")
	if _, err := e.svc.Get(e.ctx, e.carol.ID, c.ID); err != nil {
		t.Fatalf("public read before deletion: %v", err)
	}
	if err := e.db.Client.User.UpdateOneID(e.alice.ID).
		SetStatus(user.StatusDeleted).
		SetDeletedAt(db.Now()).
		Exec(e.ctx); err != nil {
		t.Fatalf("soft delete author: %v", err)
	}
	if _, err := e.svc.Get(e.ctx, e.carol.ID, c.ID); !errors.Is(err, checkins.ErrNotFound) {
		t.Fatalf("deleted author: got %v, want ErrNotFound", err)
	}
}

func TestGetToasts(t *testing.T) {
	e := newTestEnv(t)
	e.befriend(t, e.alice, e.bob)
	c := e.createCheckin(t, e.alice, "friends")

	if _, err := e.db.Client.CheckinLike.Create().
		SetCheckinID(c.ID).SetUserID(e.bob.ID).Save(e.ctx); err != nil {
		t.Fatalf("like: %v", err)
	}

	owner, err := e.svc.Get(e.ctx, e.alice.ID, c.ID)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	if owner.Toasts != 1 || owner.Toasted {
		t.Fatalf("owner toasts: got count=%d toasted=%v", owner.Toasts, owner.Toasted)
	}

	viewer, err := e.svc.Get(e.ctx, e.bob.ID, c.ID)
	if err != nil {
		t.Fatalf("viewer: %v", err)
	}
	if viewer.Toasts != 1 || !viewer.Toasted {
		t.Fatalf("viewer toasts: got count=%d toasted=%v", viewer.Toasts, viewer.Toasted)
	}

	if _, err := e.db.Client.CheckinLike.Query().
		Where(checkinlike.CheckinID(c.ID), checkinlike.UserID(e.bob.ID)).Only(e.ctx); err != nil {
		t.Fatalf("like row: %v", err)
	}
}
