package social_test

import (
	"testing"
	"time"

	"sodapop/ent/notification"
)

func TestNotifyOnceInsertsOnceInsideWindow(t *testing.T) {
	ctx, database, svc, alice, bob := newSocial(t)

	count := func() int {
		t.Helper()
		n, err := database.Client.Notification.Query().
			Where(notification.UserID(bob.ID), notification.KindEQ(notification.KindWishlistFriend)).
			Count(ctx)
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	created, err := svc.NotifyOnce(ctx, bob.ID, "wishlist_friend", alice.ID, "42", "Ada checked in Pomegranate Fizz", 7*24*time.Hour)
	if err != nil || !created {
		t.Fatalf("first NotifyOnce = %v, %v; want true, nil", created, err)
	}
	created, err = svc.NotifyOnce(ctx, bob.ID, "wishlist_friend", alice.ID, "42", "again", 7*24*time.Hour)
	if err != nil || created {
		t.Fatalf("second NotifyOnce = %v, %v; want false, nil", created, err)
	}
	if got := count(); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}

	// Age the existing row beyond the window and the next event inserts again.
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	if err := database.Client.Notification.Update().
		Where(notification.UserID(bob.ID), notification.KindEQ(notification.KindWishlistFriend)).
		SetCreatedAt(old).
		Exec(ctx); err != nil {
		t.Fatalf("age row: %v", err)
	}
	created, err = svc.NotifyOnce(ctx, bob.ID, "wishlist_friend", alice.ID, "42", "again", 7*24*time.Hour)
	if err != nil || !created {
		t.Fatalf("after window NotifyOnce = %v, %v; want true, nil", created, err)
	}
	if got := count(); got != 2 {
		t.Fatalf("rows after window = %d, want 2", got)
	}
}

func TestNotifyOnceRespectsBlocks(t *testing.T) {
	ctx, database, svc, alice, bob := newSocial(t)

	if err := svc.Block(ctx, bob.ID, alice.Handle); err != nil {
		t.Fatalf("block: %v", err)
	}
	if _, err := svc.NotifyOnce(ctx, bob.ID, "wishlist_friend", alice.ID, "42", "text", time.Hour); err != nil {
		t.Fatalf("NotifyOnce: %v", err)
	}
	n, err := database.Client.Notification.Query().
		Where(notification.UserID(bob.ID)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("blocked pair received %d notifications, want 0", n)
	}
}
