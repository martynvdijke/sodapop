package social_test

import (
	"context"
	"strconv"
	"testing"

	"sodapop/ent/notification"
	"sodapop/internal/social"
)

type notifyCall struct {
	userID    string
	kind      string
	actorID   string
	subjectID string
	text      string
}

func captureNotifies(t *testing.T, svc *social.Service) *[]notifyCall {
	t.Helper()
	calls := &[]notifyCall{}
	svc.OnNotify = func(_ context.Context, userID, kind, actorID, subjectID, text string) {
		*calls = append(*calls, notifyCall{userID, kind, actorID, subjectID, text})
	}
	return calls
}

func TestOnNotifyFriendRequestAndAccept(t *testing.T) {
	ctx, _, svc, alice, bob := newSocial(t)
	calls := captureNotifies(t, svc)

	f, err := svc.Request(ctx, alice.ID, "bob")
	if err != nil || f == nil {
		t.Fatalf("request: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("request calls = %d, want 1", len(*calls))
	}
	got := (*calls)[0]
	want := notifyCall{
		userID:    bob.ID,
		kind:      string(notification.KindFriendRequest),
		actorID:   alice.ID,
		subjectID: strconv.Itoa(f.ID),
		text:      "wants to be friends",
	}
	if got != want {
		t.Fatalf("friend request call = %+v, want %+v", got, want)
	}

	*calls = nil
	if err := svc.Accept(ctx, bob.ID, strconv.Itoa(f.ID)); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("accept calls = %d, want 1", len(*calls))
	}
	got = (*calls)[0]
	want = notifyCall{
		userID:    alice.ID,
		kind:      string(notification.KindFriendAccept),
		actorID:   bob.ID,
		subjectID: strconv.Itoa(f.ID),
		text:      "accepted your friend request",
	}
	if got != want {
		t.Fatalf("friend accept call = %+v, want %+v", got, want)
	}
}

func TestOnNotifyToast(t *testing.T) {
	ctx, database, svc, alice, bob, drink := newToastFixture(t)
	c := mustCheckin(t, ctx, database, alice, drink, "public")
	calls := captureNotifies(t, svc)

	if err := svc.Toast(ctx, bob.ID, c.ID); err != nil {
		t.Fatalf("toast: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("toast calls = %d, want 1", len(*calls))
	}
	got := (*calls)[0]
	want := notifyCall{
		userID:    alice.ID,
		kind:      string(notification.KindToast),
		actorID:   bob.ID,
		subjectID: c.ID,
		text:      "toasted your check-in",
	}
	if got != want {
		t.Fatalf("toast call = %+v, want %+v", got, want)
	}
}

func TestOnNotifyCommentAndMention(t *testing.T) {
	ctx, _, svc, alice, bob := newSocial(t)
	calls := captureNotifies(t, svc)

	tests := []struct {
		name string
		kind string
		text string
	}{
		{"comment", string(notification.KindComment), "commented on your check-in"},
		{"mention", string(notification.KindMention), "mentioned you in a comment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*calls = nil
			if err := svc.Notify(ctx, bob.ID, tt.kind, alice.ID, "checkin-1", tt.text); err != nil {
				t.Fatalf("notify: %v", err)
			}
			if len(*calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(*calls))
			}
			got := (*calls)[0]
			want := notifyCall{
				userID:    bob.ID,
				kind:      tt.kind,
				actorID:   alice.ID,
				subjectID: "checkin-1",
				text:      tt.text,
			}
			if got != want {
				t.Fatalf("call = %+v, want %+v", got, want)
			}
		})
	}
}

func TestOnNotifySkipsBadgeAndSystem(t *testing.T) {
	ctx, database, svc, alice, bob := newSocial(t)
	calls := captureNotifies(t, svc)

	if err := svc.Notify(ctx, bob.ID, string(notification.KindBadge), "", "", "earned a badge"); err != nil {
		t.Fatalf("badge notify: %v", err)
	}
	if err := svc.Notify(ctx, bob.ID, string(notification.KindSystem), "", "", "system note"); err != nil {
		t.Fatalf("system notify: %v", err)
	}
	// Unknown kinds fall back to system and must not push either.
	if err := svc.Notify(ctx, bob.ID, "not-a-kind", alice.ID, "", "odd"); err != nil {
		t.Fatalf("unknown notify: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %+v, want none", *calls)
	}

	n, err := database.Client.Notification.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("in-app rows = %d, want 3", n)
	}
}

func TestNilOnNotifyKeepsInAppRows(t *testing.T) {
	ctx, database, svc, alice, bob := newSocial(t)
	// No OnNotify set: notifications must still be recorded and nothing panics.
	f, err := svc.Request(ctx, alice.ID, "bob")
	if err != nil || f == nil {
		t.Fatalf("request: %v", err)
	}
	if err := svc.Notify(ctx, bob.ID, string(notification.KindComment), alice.ID, "checkin-1", "commented"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := svc.Accept(ctx, bob.ID, strconv.Itoa(f.ID)); err != nil {
		t.Fatalf("accept: %v", err)
	}

	n, err := database.Client.Notification.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	// friend_request + comment + friend_accept.
	if n != 3 {
		t.Fatalf("in-app rows = %d, want 3", n)
	}
}
