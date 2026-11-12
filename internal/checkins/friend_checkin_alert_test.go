package checkins_test

import (
	"testing"

	"sodapop/ent"
	"sodapop/ent/notification"
)

// follow records that follower follows followee.
func (e *testEnv) follow(t *testing.T, follower, followee *ent.User) {
	t.Helper()
	if _, err := e.db.Client.Follow.Create().
		SetFollowerID(follower.ID).SetFolloweeID(followee.ID).Save(e.ctx); err != nil {
		t.Fatalf("follow: %v", err)
	}
}

// setFriendCheckinAlerts pins the recipient's preference.
func (e *testEnv) setFriendCheckinAlerts(t *testing.T, u *ent.User, on bool) {
	t.Helper()
	if _, err := e.db.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetFriendCheckinAlerts(on).
		Save(e.ctx); err != nil {
		t.Fatalf("settings: %v", err)
	}
}

func (e *testEnv) friendCheckinCount(t *testing.T, userID string) int {
	t.Helper()
	n, err := e.db.Client.Notification.Query().
		Where(
			notification.UserID(userID),
			notification.KindEQ(notification.KindFriendCheckin),
		).
		Count(e.ctx)
	if err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	return n
}

func TestFriendCheckinAlerts(t *testing.T) {
	cases := []struct {
		name string
		// visibility of the author's new check-in.
		visibility string
		// friendsFollow reports whether the follower is also an accepted friend.
		following bool
		friends   bool
		// noSettings leaves the follower without a user_settings row.
		noSettings bool
		// prefOff writes friend_checkin_alerts = 0.
		prefOff bool
		want    int
	}{
		{name: "public notifies follower without settings row", visibility: "public", following: true, noSettings: true, want: 1},
		{name: "public notifies follower with pref on", visibility: "public", following: true, want: 1},
		{name: "public suppressed by pref off", visibility: "public", following: true, prefOff: true, want: 0},
		{name: "friends notifies friend follower", visibility: "friends", following: true, friends: true, want: 1},
		{name: "friends skips non-friend follower", visibility: "friends", following: true, want: 0},
		{name: "private notifies nobody", visibility: "private", following: true, friends: true, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			if tc.following {
				e.follow(t, e.bob, e.alice)
			}
			if tc.friends {
				e.befriend(t, e.alice, e.bob)
			}
			if !tc.noSettings {
				e.setFriendCheckinAlerts(t, e.bob, !tc.prefOff)
			}

			c := e.createCheckin(t, e.alice, tc.visibility)
			if got := e.friendCheckinCount(t, e.bob.ID); got != tc.want {
				t.Fatalf("notifications = %d, want %d", got, tc.want)
			}
			if tc.want == 0 {
				return
			}

			n, err := e.db.Client.Notification.Query().
				Where(
					notification.UserID(e.bob.ID),
					notification.KindEQ(notification.KindFriendCheckin),
				).
				Only(e.ctx)
			if err != nil {
				t.Fatalf("notification: %v", err)
			}
			if n.SubjectID != c.ID {
				t.Errorf("subject = %q, want %q", n.SubjectID, c.ID)
			}
			if n.ActorID == nil || *n.ActorID != e.alice.ID {
				t.Errorf("actor = %v, want %q", n.ActorID, e.alice.ID)
			}
			if n.Text != "Alice checked in Ginger Beer" {
				t.Errorf("text = %q", n.Text)
			}
		})
	}
}

func TestFriendCheckinSkipsAuthor(t *testing.T) {
	e := newTestEnv(t)
	// Defensive: even a self-follow must not notify the author.
	e.follow(t, e.alice, e.alice)
	e.createCheckin(t, e.alice, "public")
	if got := e.friendCheckinCount(t, e.alice.ID); got != 0 {
		t.Fatalf("author notifications = %d, want 0", got)
	}
}
