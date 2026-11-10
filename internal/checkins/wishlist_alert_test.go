package checkins_test

import (
	"testing"

	"sodapop/ent"
	"sodapop/ent/notification"
	"sodapop/internal/checkins"
)

// wishlistFriend setups a friend with the env drink on their system wishlist
// and the friend alert preference in the requested state.
func (e *testEnv) wishlistFriend(t *testing.T, handle string, prefOn bool) *ent.User {
	t.Helper()
	friend := createUser(t, e.ctx, e.db.Client, handle, handle)
	e.befriend(t, e.alice, friend)

	list, err := e.db.Client.List.Create().
		SetUserID(friend.ID).
		SetName("Wishlist").
		SetSlug("wishlist").
		SetIsSystem(true).
		Save(e.ctx)
	if err != nil {
		t.Fatalf("wishlist list: %v", err)
	}
	if _, err := e.db.Client.ListItem.Create().
		SetListID(list.ID).
		SetDrinkID(e.drink.ID).
		Save(e.ctx); err != nil {
		t.Fatalf("wishlist item: %v", err)
	}
	if _, err := e.db.Client.UserSetting.Create().
		SetUserID(friend.ID).
		SetWishlistFriendAlerts(prefOn).
		Save(e.ctx); err != nil {
		t.Fatalf("settings: %v", err)
	}
	return friend
}

func (e *testEnv) friendAlertCount(t *testing.T, userID string) int {
	t.Helper()
	n, err := e.db.Client.Notification.Query().
		Where(
			notification.UserID(userID),
			notification.KindEQ(notification.KindWishlistFriend),
		).
		Count(e.ctx)
	if err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	return n
}

func TestWishlistFriendCheckinAlert(t *testing.T) {
	cases := []struct {
		name       string
		prefOn     bool
		visibility string
		already    bool
		second     bool
		want       int
	}{
		{name: "wishlisted with pref on", prefOn: true, visibility: "public", want: 1},
		{name: "pref off", prefOn: false, visibility: "public", want: 0},
		{name: "private check-in", prefOn: true, visibility: "private", want: 0},
		{name: "recipient already checked it in", prefOn: true, visibility: "public", already: true, want: 0},
		{name: "second check-in inside window", prefOn: true, visibility: "friends", second: true, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			friend := e.wishlistFriend(t, "dave", tc.prefOn)

			if tc.already {
				if _, _, err := e.svc.Create(e.ctx, friend, checkins.CreateInput{
					DrinkID: e.drink.ID, Visibility: "friends",
				}); err != nil {
					t.Fatalf("recipient check-in: %v", err)
				}
			}

			if _, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
				DrinkID: e.drink.ID, Visibility: tc.visibility,
			}); err != nil {
				t.Fatalf("create: %v", err)
			}
			if tc.second {
				if _, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
					DrinkID: e.drink.ID, Visibility: tc.visibility,
				}); err != nil {
					t.Fatalf("second create: %v", err)
				}
			}

			if got := e.friendAlertCount(t, friend.ID); got != tc.want {
				t.Fatalf("notifications = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestWishlistFriendCheckinTextNamesFriendAndDrink(t *testing.T) {
	e := newTestEnv(t)
	friend := e.wishlistFriend(t, "dave", true)
	if _, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
		DrinkID: e.drink.ID, Visibility: "public",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	row, err := e.db.Client.Notification.Query().
		Where(notification.UserID(friend.ID), notification.KindEQ(notification.KindWishlistFriend)).
		Only(e.ctx)
	if err != nil {
		t.Fatalf("notification: %v", err)
	}
	want := "Alice checked in Ginger Beer"
	if row.Text != want {
		t.Fatalf("text = %q, want %q", row.Text, want)
	}
	if row.ActorID == nil || *row.ActorID != e.alice.ID {
		t.Fatalf("actor = %v, want alice", row.ActorID)
	}
	if row.SubjectID == "" {
		t.Fatal("subject id should be the drink id")
	}
}
