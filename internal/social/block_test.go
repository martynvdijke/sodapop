package social_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/friendship"
	"sodapop/ent/notification"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

func newBlockFixture(t *testing.T) (context.Context, *db.DB, *social.Service, *ent.User, *ent.User, *ent.User, *ent.Drink) {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "block.db"))
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
	return ctx, database, social.New(database), mk("alice", "Alice"), mk("bob", "Bob"), mk("carol", "Carol"), drink
}

func blockCount(t *testing.T, ctx context.Context, database *db.DB) int {
	t.Helper()
	n, err := database.Client.Block.Query().Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBlockIdempotentSelfAndUnknown(t *testing.T) {
	ctx, database, svc, alice, bob, _, _ := newBlockFixture(t)

	if err := svc.Block(ctx, alice.ID, "BOB"); err != nil {
		t.Fatalf("block: %v", err)
	}
	if err := svc.Block(ctx, alice.ID, "bob"); err != nil {
		t.Fatalf("second block: %v", err)
	}
	if got := blockCount(t, ctx, database); got != 1 {
		t.Fatalf("blocks = %d, want 1", got)
	}

	// Either direction reports blocked.
	if ok, err := svc.AreBlocked(ctx, alice.ID, bob.ID); err != nil || !ok {
		t.Fatalf("AreBlocked alice->bob = %v, %v", ok, err)
	}
	if ok, err := svc.AreBlocked(ctx, bob.ID, alice.ID); err != nil || !ok {
		t.Fatalf("AreBlocked bob->alice = %v, %v", ok, err)
	}

	if err := svc.Block(ctx, alice.ID, "alice"); !errors.Is(err, social.ErrSelfAction) {
		t.Fatalf("self block = %v, want ErrSelfAction", err)
	}
	if err := svc.Block(ctx, alice.ID, "nobody"); err != nil {
		t.Fatalf("unknown block = %v, want silent no-op", err)
	}
	if got := blockCount(t, ctx, database); got != 1 {
		t.Fatalf("blocks after self/unknown = %d, want 1", got)
	}

	// Unblock removes only the blocker's own row.
	if err := svc.Unblock(ctx, bob.ID, "alice"); err != nil {
		t.Fatalf("wrong-way unblock: %v", err)
	}
	if got := blockCount(t, ctx, database); got != 1 {
		t.Fatalf("wrong-way unblock removed row: got %d, want 1", got)
	}
	if err := svc.Unblock(ctx, alice.ID, "bob"); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if got := blockCount(t, ctx, database); got != 0 {
		t.Fatalf("blocks after unblock = %d, want 0", got)
	}
}

func TestBlockEndsFriendshipButKeepsContent(t *testing.T) {
	ctx, database, svc, alice, bob, carol, drink := newBlockFixture(t)

	a, b := alice.ID, bob.ID
	if a > b {
		a, b = b, a
	}
	if _, err := database.Client.Friendship.Create().
		SetUserA(a).SetUserB(b).SetRequestedBy(alice.ID).
		SetStatus(friendship.StatusAccepted).Save(ctx); err != nil {
		t.Fatalf("friendship: %v", err)
	}
	// A pending request to a third user must also disappear.
	if _, err := svc.Request(ctx, alice.ID, carol.Handle); err != nil || svc == nil {
		t.Fatalf("request: %v", err)
	}

	c, err := database.Client.Checkin.Create().
		SetUserID(bob.ID).SetDrinkID(drink.ID).SetVisibility(checkin.VisibilityPublic).Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if _, err := database.Client.Comment.Create().
		SetCheckinID(c.ID).SetUserID(alice.ID).SetBody("nice").Save(ctx); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, err := database.Client.CheckinLike.Create().
		SetCheckinID(c.ID).SetUserID(alice.ID).Save(ctx); err != nil {
		t.Fatalf("like: %v", err)
	}

	if err := svc.Block(ctx, alice.ID, bob.Handle); err != nil {
		t.Fatalf("block bob: %v", err)
	}
	if err := svc.Block(ctx, alice.ID, carol.Handle); err != nil {
		t.Fatalf("block carol: %v", err)
	}

	n, err := database.Client.Friendship.Query().Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("friendships after block = %d, want 0", n)
	}
	if n, _ := database.Client.Checkin.Query().Count(ctx); n != 1 {
		t.Fatalf("checkins deleted: %d", n)
	}
	if n, _ := database.Client.Comment.Query().Count(ctx); n != 1 {
		t.Fatalf("comments deleted: %d", n)
	}
	if n, _ := database.Client.CheckinLike.Query().Count(ctx); n != 1 {
		t.Fatalf("toasts deleted: %d", n)
	}
}

func TestBlockedFeedBothDirections(t *testing.T) {
	ctx, database, svc, alice, bob, _, drink := newBlockFixture(t)

	if _, err := database.Client.Follow.Create().
		SetFollowerID(alice.ID).SetFolloweeID(bob.ID).Save(ctx); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if _, err := database.Client.Checkin.Create().
		SetUserID(bob.ID).SetDrinkID(drink.ID).SetVisibility(checkin.VisibilityPublic).Save(ctx); err != nil {
		t.Fatalf("checkin: %v", err)
	}

	followCount := func() int {
		t.Helper()
		n, err := database.Client.Follow.Query().Count(ctx)
		if err != nil {
			t.Fatalf("follow count: %v", err)
		}
		return n
	}
	assertFeeds := func(want int, label string) {
		t.Helper()
		feed, err := svc.Feed(ctx, alice.ID, 20, "")
		if err != nil {
			t.Fatalf("feed: %v", err)
		}
		if len(feed) != want {
			t.Fatalf("%s: feed size = %d, want %d", label, len(feed), want)
		}
		following, err := svc.FeedFollowing(ctx, alice.ID, 20, "")
		if err != nil {
			t.Fatalf("following: %v", err)
		}
		if len(following) != want {
			t.Fatalf("%s: following size = %d, want %d", label, len(following), want)
		}
	}

	// Unblocked and following: both feeds see Bob's public check-in.
	assertFeeds(1, "initial follow")

	// Alice blocks Bob: the follow row disappears with the block, so neither
	// feed can include a blocked account.
	if err := svc.Block(ctx, alice.ID, bob.Handle); err != nil {
		t.Fatalf("block alice->bob: %v", err)
	}
	if n := followCount(); n != 0 {
		t.Fatalf("follows after block = %d, want 0", n)
	}
	assertFeeds(0, "alice blocks bob")

	// Unblocking removes only the block; it must not restore the follow.
	if err := svc.Unblock(ctx, alice.ID, bob.Handle); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if n := followCount(); n != 0 {
		t.Fatalf("follows after unblock = %d, want 0 (unblock must not restore)", n)
	}
	assertFeeds(0, "after unblock")

	// Re-following explicitly restores the feed.
	if err := svc.Follow(ctx, alice.ID, bob.Handle); err != nil {
		t.Fatalf("re-follow: %v", err)
	}
	assertFeeds(1, "re-follow")

	// The reverse direction is symmetric.
	if err := svc.Block(ctx, bob.ID, alice.Handle); err != nil {
		t.Fatalf("block bob->alice: %v", err)
	}
	if n := followCount(); n != 0 {
		t.Fatalf("follows after reverse block = %d, want 0", n)
	}
	assertFeeds(0, "bob blocks alice")
}

func TestFollowSilentAcrossEitherDirectionBlock(t *testing.T) {
	ctx, database, svc, alice, bob, _, _ := newBlockFixture(t)

	countFollows := func() int {
		t.Helper()
		n, err := database.Client.Follow.Query().Count(ctx)
		if err != nil {
			t.Fatalf("follow count: %v", err)
		}
		return n
	}

	// Bob blocks Alice, then Alice tries to follow Bob. The result must be
	// indistinguishable from following an unknown handle: no error, no row.
	if err := svc.Block(ctx, bob.ID, alice.Handle); err != nil {
		t.Fatalf("block bob->alice: %v", err)
	}
	if err := svc.Follow(ctx, alice.ID, bob.Handle); err != nil {
		t.Fatalf("follow across block = %v, want silent nil", err)
	}
	if n := countFollows(); n != 0 {
		t.Fatalf("follow rows after blocked follow = %d, want 0", n)
	}

	// The reverse direction is equally silent after unblocking and swapping.
	if err := svc.Unblock(ctx, bob.ID, alice.Handle); err != nil {
		t.Fatalf("unblock bob->alice: %v", err)
	}
	if err := svc.Block(ctx, alice.ID, bob.Handle); err != nil {
		t.Fatalf("block alice->bob: %v", err)
	}
	if err := svc.Follow(ctx, bob.ID, alice.Handle); err != nil {
		t.Fatalf("reverse follow across block = %v, want silent nil", err)
	}
	if n := countFollows(); n != 0 {
		t.Fatalf("follow rows after reverse blocked follow = %d, want 0", n)
	}
}

func TestBlockedToastNotificationAndFriendRequests(t *testing.T) {
	ctx, database, svc, alice, bob, _, drink := newBlockFixture(t)

	c, err := database.Client.Checkin.Create().
		SetUserID(bob.ID).SetDrinkID(drink.ID).SetVisibility(checkin.VisibilityPublic).Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}

	// A notification written before the block is hidden afterwards.
	if err := svc.Notify(ctx, bob.ID, "toast", alice.ID, c.ID, "toasted your check-in"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := svc.Block(ctx, bob.ID, alice.Handle); err != nil {
		t.Fatalf("block: %v", err)
	}

	if err := svc.Toast(ctx, alice.ID, c.ID); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("blocked toast = %v, want ErrNotFound", err)
	}
	if err := svc.Untoast(ctx, alice.ID, c.ID); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("blocked untoast = %v, want ErrNotFound", err)
	}

	// Notify is a silent no-op across a block in either direction.
	if err := svc.Notify(ctx, bob.ID, "toast", alice.ID, c.ID, "again"); err != nil {
		t.Fatalf("blocked notify: %v", err)
	}
	rows, err := svc.Notifications(ctx, bob.ID, 50)
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("notifications visible across block = %d, want 0", len(rows))
	}
	if unread, err := svc.UnreadCount(ctx, bob.ID); err != nil || unread != 0 {
		t.Fatalf("unread across block = %d, %v; want 0, nil", unread, err)
	}
	if n, _ := database.Client.Notification.Query().Where(notification.ActorID(alice.ID)).Count(ctx); n != 1 {
		t.Fatalf("extra notification written across block: %d", n)
	}

	// No new friend request can cross the block.
	if f, err := svc.Request(ctx, alice.ID, bob.Handle); err != nil || f != nil {
		t.Fatalf("blocked request = %v, %v, want nil, nil", f, err)
	}

	// Even a manually inserted pending row cannot be accepted.
	x, y := alice.ID, bob.ID
	if x > y {
		x, y = y, x
	}
	pending, err := database.Client.Friendship.Create().
		SetUserA(x).SetUserB(y).SetRequestedBy(alice.ID).
		SetStatus(friendship.StatusPending).Save(ctx)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if err := svc.Accept(ctx, bob.ID, itoa(pending.ID)); !errors.Is(err, social.ErrNotFound) {
		t.Fatalf("blocked accept = %v, want ErrNotFound", err)
	}
}

func TestBlockedUsersAndIDs(t *testing.T) {
	ctx, _, svc, alice, bob, carol, _ := newBlockFixture(t)

	if err := svc.Block(ctx, alice.ID, bob.Handle); err != nil {
		t.Fatal(err)
	}
	if err := svc.Block(ctx, carol.ID, alice.Handle); err != nil {
		t.Fatal(err)
	}

	users, err := svc.BlockedUsers(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != bob.ID || users[0].Handle != "bob" {
		t.Fatalf("blocked users = %+v", users)
	}
	ids, err := svc.BlockedUserIDs(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("blocked ids = %v, want bob and carol", ids)
	}
	// Another user's list must not include the reverse-direction block.
	users, err = svc.BlockedUsers(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Fatalf("bob blocked users = %+v, want empty", users)
	}
}

// itoa avoids importing strconv in several places in this file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
