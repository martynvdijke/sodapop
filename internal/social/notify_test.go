package social_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"sodapop/ent"
	"sodapop/ent/notification"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

func newSocial(t *testing.T) (context.Context, *db.DB, *social.Service, *ent.User, *ent.User) {
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
	return ctx, database, social.New(database), mk("alice", "Alice"), mk("bob", "Bob")
}

func TestNotifyUnreadAndMarkRead(t *testing.T) {
	ctx, database, svc, alice, bob := newSocial(t)

	if err := svc.Notify(ctx, bob.ID, "toast", alice.ID, "checkin-1", "toasted your check-in"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := svc.Notify(ctx, bob.ID, "badge", "", "", "earned a badge"); err != nil {
		t.Fatalf("notify: %v", err)
	}

	unread, err := svc.UnreadCount(ctx, bob.ID)
	if err != nil || unread != 2 {
		t.Fatalf("unread: got %d err %v, want 2", unread, err)
	}

	rows, err := svc.Notifications(ctx, bob.ID, 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("notifications: got %d err %v", len(rows), err)
	}

	// Marking one row leaves the other unread.
	if err := svc.MarkRead(ctx, bob.ID, rows[0].ID); err != nil {
		t.Fatalf("mark one: %v", err)
	}
	if n, _ := svc.UnreadCount(ctx, bob.ID); n != 1 {
		t.Fatalf("after one: got %d unread, want 1", n)
	}

	// Marking all is idempotent.
	if err := svc.MarkRead(ctx, bob.ID, ""); err != nil {
		t.Fatalf("mark all: %v", err)
	}
	if n, _ := svc.UnreadCount(ctx, bob.ID); n != 0 {
		t.Fatalf("after all: got %d unread, want 0", n)
	}
	if err := svc.MarkRead(ctx, bob.ID, ""); err != nil {
		t.Fatalf("mark all again: %v", err)
	}

	// Marking another user's notification must not touch it.
	if err := svc.Notify(ctx, bob.ID, "toast", alice.ID, "checkin-2", "again"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := svc.MarkRead(ctx, alice.ID, ""); err != nil {
		t.Fatalf("mark wrong user: %v", err)
	}
	if n, _ := svc.UnreadCount(ctx, bob.ID); n != 1 {
		t.Fatalf("wrong-user mark changed rows: got %d unread, want 1", n)
	}

	_ = database
}

func TestFriendRequestAndAcceptNotify(t *testing.T) {
	ctx, database, svc, alice, bob := newSocial(t)

	f, err := svc.Request(ctx, alice.ID, "bob")
	if err != nil || f == nil {
		t.Fatalf("request: %v", err)
	}

	rows, err := database.Client.Notification.Query().
		Where(notification.UserID(bob.ID)).
		All(ctx)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d notifications, want 1", len(rows))
	}
	req := rows[0]
	if req.Kind != notification.KindFriendRequest {
		t.Fatalf("kind: got %s", req.Kind)
	}
	if req.ActorID == nil || *req.ActorID != alice.ID {
		t.Fatalf("actor: %v", req.ActorID)
	}
	if req.SubjectID != strconv.Itoa(f.ID) {
		t.Fatalf("subject: got %q, want %d", req.SubjectID, f.ID)
	}

	if err := svc.Accept(ctx, bob.ID, strconv.Itoa(f.ID)); err != nil {
		t.Fatalf("accept: %v", err)
	}

	rows, err = database.Client.Notification.Query().
		Where(notification.UserID(alice.ID), notification.KindEQ(notification.KindFriendAccept)).
		All(ctx)
	if err != nil {
		t.Fatalf("query accept: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d accept notifications, want 1", len(rows))
	}
	if rows[0].ActorID == nil || *rows[0].ActorID != bob.ID {
		t.Fatalf("accept actor: %v", rows[0].ActorID)
	}
}
