package social_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/notification"
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

func TestUntoastChangesCountAndIsIdempotent(t *testing.T) {
	ctx, database, svc, alice, bob, drink := newToastFixture(t)
	c := mustCheckin(t, ctx, database, alice, drink, "public")

	if err := svc.Toast(ctx, bob.ID, c.ID); err != nil {
		t.Fatalf("toast: %v", err)
	}
	count := func() int {
		n, err := database.Client.CheckinLike.Query().Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := count(); got != 1 {
		t.Fatalf("likes after toast = %d, want 1", got)
	}
	if err := svc.Untoast(ctx, bob.ID, c.ID); err != nil {
		t.Fatalf("untoast: %v", err)
	}
	if got := count(); got != 0 {
		t.Fatalf("likes after untoast = %d, want 0", got)
	}
	if err := svc.Untoast(ctx, bob.ID, c.ID); err != nil {
		t.Fatalf("second untoast: %v", err)
	}
	if got := count(); got != 0 {
		t.Fatalf("likes after repeated untoast = %d, want 0", got)
	}
}

func TestUntoastRemovesToastNotification(t *testing.T) {
	ctx, database, svc, alice, bob, drink := newToastFixture(t)
	c := mustCheckin(t, ctx, database, alice, drink, "public")

	count := func() int {
		n, err := database.Client.Notification.Query().
			Where(notification.KindEQ(notification.KindToast)).Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := svc.Toast(ctx, bob.ID, c.ID); err != nil {
		t.Fatalf("toast: %v", err)
	}
	if got := count(); got != 1 {
		t.Fatalf("toast notifications = %d, want 1", got)
	}
	if err := svc.Untoast(ctx, bob.ID, c.ID); err != nil {
		t.Fatalf("untoast: %v", err)
	}
	if got := count(); got != 0 {
		t.Fatalf("toast notifications after untoast = %d, want 0", got)
	}
	if err := svc.Untoast(ctx, bob.ID, c.ID); err != nil {
		t.Fatalf("second untoast: %v", err)
	}
	if got := count(); got != 0 {
		t.Fatalf("toast notifications after repeated untoast = %d, want 0", got)
	}
}

func TestUntoastRejectsPrivateCheckinFromStranger(t *testing.T) {
	ctx, database, svc, alice, bob, drink := newToastFixture(t)
	c := mustCheckin(t, ctx, database, alice, drink, "private")

	if err := svc.Untoast(ctx, bob.ID, c.ID); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("stranger untoast: got %v, want ErrNotFound", err)
	}
}

func TestFeedVisibilityForFriendsAndFollows(t *testing.T) {
	ctx, database, svc, alice, bob, drink := newToastFixture(t)
	carol, err := database.Client.User.Create().
		SetHandle("carol").SetEmail("carol@example.com").SetDisplayName("Carol").Save(ctx)
	if err != nil {
		t.Fatalf("carol: %v", err)
	}
	a, b := alice.ID, bob.ID
	if a > b {
		a, b = b, a
	}
	if _, err := database.Client.Friendship.Create().
		SetUserA(a).SetUserB(b).SetRequestedBy(alice.ID).SetStatus("accepted").Save(ctx); err != nil {
		t.Fatalf("friendship: %v", err)
	}
	if err := svc.Follow(ctx, bob.ID, "carol"); err != nil {
		t.Fatalf("follow carol: %v", err)
	}

	bobOwn := mustCheckin(t, ctx, database, bob, drink, "private")
	alicePublic := mustCheckin(t, ctx, database, alice, drink, "public")
	aliceFriends := mustCheckin(t, ctx, database, alice, drink, "friends")
	mustCheckin(t, ctx, database, alice, drink, "private")
	carolPublic := mustCheckin(t, ctx, database, carol, drink, "public")
	mustCheckin(t, ctx, database, carol, drink, "friends")

	ids := func(items []social.FeedItem) map[string]bool {
		out := map[string]bool{}
		for _, item := range items {
			if out[item.ID] {
				t.Fatalf("duplicate feed item %s", item.ID)
			}
			out[item.ID] = true
		}
		return out
	}

	feed, err := svc.Feed(ctx, bob.ID, 20, "")
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	got := ids(feed)
	for _, want := range []string{bobOwn.ID, alicePublic.ID, aliceFriends.ID, carolPublic.ID} {
		if !got[want] {
			t.Errorf("feed missing %s", want)
		}
	}
	if len(got) != 4 {
		t.Errorf("feed has %d items, want 4", len(got))
	}

	following, err := svc.FeedFollowing(ctx, bob.ID, 20, "")
	if err != nil {
		t.Fatalf("following feed: %v", err)
	}
	got = ids(following)
	if !got[bobOwn.ID] || !got[carolPublic.ID] {
		t.Errorf("following feed missing own or followed public check-in: %v", got)
	}
	if got[alicePublic.ID] || got[aliceFriends.ID] {
		t.Errorf("following feed leaked a non-followed friend's check-ins: %v", got)
	}
	if len(got) != 2 {
		t.Errorf("following feed has %d items, want 2", len(got))
	}
}

func TestFeedIncludesFollowedUsersPublicCheckins(t *testing.T) {
	ctx, database, svc, alice, bob, drink := newToastFixture(t)
	if err := svc.Follow(ctx, bob.ID, "alice"); err != nil {
		t.Fatalf("follow: %v", err)
	}
	public := mustCheckin(t, ctx, database, alice, drink, "public")
	mustCheckin(t, ctx, database, alice, drink, "private")
	mustCheckin(t, ctx, database, alice, drink, "friends")
	deleted := mustCheckin(t, ctx, database, alice, drink, "public")
	if _, err := database.Client.Checkin.UpdateOneID(deleted.ID).SetDeletedAt("2026-01-01T00:00:00Z").Save(ctx); err != nil {
		t.Fatalf("delete checkin: %v", err)
	}

	items, err := svc.Feed(ctx, bob.ID, 20, "")
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	if len(items) != 1 || items[0].ID != public.ID {
		ids := make([]string, len(items))
		for i, item := range items {
			ids[i] = item.ID
		}
		t.Fatalf("feed ids = %v, want [%s]", ids, public.ID)
	}

	following, err := svc.FeedFollowing(ctx, bob.ID, 20, "")
	if err != nil {
		t.Fatalf("following feed: %v", err)
	}
	if len(following) != 1 || following[0].ID != public.ID {
		t.Fatalf("following feed = %+v, want only public %s", following, public.ID)
	}
}

func TestFeedFollowedFriendVisibility(t *testing.T) {
	tests := []struct {
		name          string
		friends       bool
		visibility    string
		followingOnly bool
		want          bool
	}{
		{"following: followed friend sees friends check-in", true, "friends", true, true},
		{"following: followed stranger does not see friends check-in", false, "friends", true, false},
		{"following: followed stranger sees public check-in", false, "public", true, true},
		{"feed: friend sees friends check-in", true, "friends", false, true},
		{"feed: followed stranger does not see friends check-in", false, "friends", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, database, svc, alice, bob, drink := newToastFixture(t)
			if err := svc.Follow(ctx, bob.ID, "alice"); err != nil {
				t.Fatalf("follow: %v", err)
			}
			if tt.friends {
				a, b := alice.ID, bob.ID
				if a > b {
					a, b = b, a
				}
				if _, err := database.Client.Friendship.Create().
					SetUserA(a).SetUserB(b).SetRequestedBy(alice.ID).SetStatus("accepted").Save(ctx); err != nil {
					t.Fatalf("friendship: %v", err)
				}
			}
			c := mustCheckin(t, ctx, database, alice, drink, tt.visibility)

			var items []social.FeedItem
			var err error
			if tt.followingOnly {
				items, err = svc.FeedFollowing(ctx, bob.ID, 20, "")
			} else {
				items, err = svc.Feed(ctx, bob.ID, 20, "")
			}
			if err != nil {
				t.Fatalf("feed: %v", err)
			}
			found := false
			seen := map[string]bool{}
			for _, item := range items {
				if seen[item.ID] {
					t.Fatalf("duplicate feed item %s", item.ID)
				}
				seen[item.ID] = true
				if item.ID == c.ID {
					found = true
				}
			}
			if found != tt.want {
				t.Fatalf("check-in %s present = %v, want %v (items %d)", c.ID, found, tt.want, len(items))
			}
		})
	}
}
