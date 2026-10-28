package comments_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/friendship"
	"sodapop/ent/notification"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
	"sodapop/internal/comments"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

type fixture struct {
	ctx      context.Context
	db       *db.DB
	svc      *comments.Service
	checkins *checkins.Service
	owner    *ent.User
	friend   *ent.User
	stranger *ent.User
	admin    *ent.User
	drink    *ent.Drink
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "comments.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()

	mk := func(handle, name string, role user.Role) *ent.User {
		u, err := database.Client.User.Create().
			SetHandle(handle).
			SetEmail(handle + "@example.com").
			SetDisplayName(name).
			SetRole(role).
			Save(ctx)
		if err != nil {
			t.Fatalf("user %s: %v", handle, err)
		}
		return u
	}

	owner := mk("alice", "Alice", user.RoleUser)
	friend := mk("bob", "Bob", user.RoleUser)
	stranger := mk("carol", "Carol", user.RoleUser)
	admin := mk("dana", "Dana", user.RoleAdmin)

	a, b := owner.ID, friend.ID
	if a > b {
		a, b = b, a
	}
	if _, err := database.Client.Friendship.Create().
		SetUserA(a).SetUserB(b).
		SetRequestedBy(owner.ID).
		SetStatus(friendship.StatusAccepted).
		Save(ctx); err != nil {
		t.Fatalf("friendship: %v", err)
	}

	drink, err := database.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}

	soc := social.New(database)
	checkinSvc := checkins.New(database, soc)
	return &fixture{
		ctx:      ctx,
		db:       database,
		svc:      comments.New(database, checkinSvc),
		checkins: checkinSvc,
		owner:    owner,
		friend:   friend,
		stranger: stranger,
		admin:    admin,
		drink:    drink,
	}
}

func (f *fixture) checkin(t *testing.T, visibility string) *ent.Checkin {
	t.Helper()
	c, err := f.db.Client.Checkin.Create().
		SetUserID(f.owner.ID).
		SetDrinkID(f.drink.ID).
		SetVisibility(checkin.Visibility(visibility)).
		Save(f.ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func TestVisibilityMatrix(t *testing.T) {
	tests := []struct {
		name       string
		visibility string
		viewer     string // owner, friend, stranger, anonymous
		want       bool
	}{
		{"public owner", "public", "owner", true},
		{"public friend", "public", "friend", true},
		{"public stranger", "public", "stranger", true},
		{"public anonymous", "public", "anonymous", true},
		{"friends owner", "friends", "owner", true},
		{"friends friend", "friends", "friend", true},
		{"friends stranger", "friends", "stranger", false},
		{"friends anonymous", "friends", "anonymous", false},
		{"private owner", "private", "owner", true},
		{"private friend", "private", "friend", false},
		{"private stranger", "private", "stranger", false},
		{"private anonymous", "private", "anonymous", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			c := f.checkin(t, tt.visibility)

			var viewer *ent.User
			switch tt.viewer {
			case "owner":
				viewer = f.owner
			case "friend":
				viewer = f.friend
			case "stranger":
				viewer = f.stranger
			}
			viewerID := ""
			if viewer != nil {
				viewerID = viewer.ID
			}

			list, err := f.svc.List(f.ctx, viewerID, c.ID)
			if tt.want {
				if err != nil {
					t.Fatalf("List: %v", err)
				}
				if len(list) != 0 {
					t.Fatalf("List returned %d comments, want 0", len(list))
				}
			} else if !errors.Is(err, comments.ErrNotFound) {
				t.Fatalf("List error = %v, want ErrNotFound", err)
			}

			if viewer == nil {
				return // writes require an authenticated user
			}
			_, err = f.svc.Create(f.ctx, viewer.ID, c.ID, "hello")
			if tt.want {
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
			} else if !errors.Is(err, comments.ErrNotFound) {
				t.Fatalf("Create error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestCreateValidation(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")

	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "   "); !errors.Is(err, comments.ErrInvalid) {
		t.Fatalf("empty body: got %v, want ErrInvalid", err)
	}
	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, strings.Repeat("a", 1001)); !errors.Is(err, comments.ErrInvalid) {
		t.Fatalf("1001 runes: got %v, want ErrInvalid", err)
	}
	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, strings.Repeat("🍺", 1000)); err != nil {
		t.Fatalf("1000 multibyte runes: %v", err)
	}
	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "  trimmed  "); err != nil {
		t.Fatalf("trimmed body: %v", err)
	}
	list, err := f.svc.List(f.ctx, f.owner.ID, c.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("comments = %d, want 2", len(list))
	}
	foundTrimmed := false
	for _, c := range list {
		if c.Body == "trimmed" {
			foundTrimmed = true
		}
	}
	if !foundTrimmed {
		t.Fatalf("comments = %+v, want a trimmed body", list)
	}
}

func TestRateLimitAtThirtyPerHour(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")

	for i := 0; i < 30; i++ {
		if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "ok"); err != nil {
			t.Fatalf("comment %d: %v", i+1, err)
		}
	}
	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "one too many"); !errors.Is(err, comments.ErrRateLimited) {
		t.Fatalf("31st comment: got %v, want ErrRateLimited", err)
	}
}

func TestDeleteRules(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")

	created, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "mine")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A stranger cannot delete or learn that the comment exists.
	if err := f.svc.Delete(f.ctx, f.stranger.ID, false, created.ID); !errors.Is(err, comments.ErrNotFound) {
		t.Fatalf("stranger delete: got %v, want ErrNotFound", err)
	}

	// The author can delete; the row disappears from List.
	if err := f.svc.Delete(f.ctx, f.friend.ID, false, created.ID); err != nil {
		t.Fatalf("author delete: %v", err)
	}
	list, err := f.svc.List(f.ctx, f.owner.ID, c.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List after delete = %d comments, want 0", len(list))
	}
	// Repeated delete leaves the row deleted.
	if err := f.svc.Delete(f.ctx, f.friend.ID, false, created.ID); !errors.Is(err, comments.ErrNotFound) {
		t.Fatalf("repeat delete: got %v, want ErrNotFound", err)
	}

	// An admin can remove another user's comment.
	other, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "remove me")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := f.svc.Delete(f.ctx, f.admin.ID, true, other.ID); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
	list, err = f.svc.List(f.ctx, f.owner.ID, c.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List after admin delete = %d comments, want 0", len(list))
	}
}

func TestDeleteRequiresVisibleParentForAuthor(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "friends")
	created, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "friend comment")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Drop the friendship; the author can no longer see the friends-only parent,
	// so the delete behaves like a missing comment.
	if _, err := f.db.Client.Friendship.Delete().Exec(f.ctx); err != nil {
		t.Fatalf("remove friendship: %v", err)
	}
	if err := f.svc.Delete(f.ctx, f.friend.ID, false, created.ID); !errors.Is(err, comments.ErrNotFound) {
		t.Fatalf("delete with invisible parent: got %v, want ErrNotFound", err)
	}
}

func TestNotificationForOwnerOnly(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")

	created, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "nice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rows, err := f.db.Client.Notification.Query().All(f.ctx)
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("notifications = %d, want 1", len(rows))
	}
	n := rows[0]
	if n.UserID != f.owner.ID || n.ActorID == nil || *n.ActorID != f.friend.ID {
		t.Fatalf("notification = %+v, want owner/subject audit", n)
	}
	if n.Kind != notification.KindComment || n.SubjectID != c.ID || n.Text != "commented on your check-in" {
		t.Fatalf("notification fields = %+v", n)
	}
	_ = created

	// Self-comments do not notify.
	if _, err := f.svc.Create(f.ctx, f.owner.ID, c.ID, "self"); err != nil {
		t.Fatalf("self comment: %v", err)
	}
	count, err := f.db.Client.Notification.Query().Count(f.ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("notifications after self-comment = %d, want 1", count)
	}
}

func TestCreateNotifiesThroughSocialExactlyOnce(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")

	var calls int
	var gotUserID, gotKind, gotActorID, gotSubjectID, gotText string
	f.svc.Social.OnNotify = func(_ context.Context, userID, kind, actorID, subjectID, text string) {
		calls++
		gotUserID, gotKind, gotActorID, gotSubjectID, gotText = userID, kind, actorID, subjectID, text
	}

	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "nice"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if calls != 1 {
		t.Fatalf("OnNotify calls = %d, want 1", calls)
	}
	if gotKind != string(notification.KindComment) {
		t.Fatalf("OnNotify kind = %q, want %q", gotKind, notification.KindComment)
	}
	if gotUserID != f.owner.ID {
		t.Fatalf("OnNotify userID = %q, want check-in owner %q", gotUserID, f.owner.ID)
	}
	if gotActorID != f.friend.ID {
		t.Fatalf("OnNotify actorID = %q, want commenter %q", gotActorID, f.friend.ID)
	}
	if gotSubjectID != c.ID {
		t.Fatalf("OnNotify subjectID = %q, want check-in %q", gotSubjectID, c.ID)
	}
	if gotText != "commented on your check-in" {
		t.Fatalf("OnNotify text = %q", gotText)
	}

	// Exactly one in-app row: the social path must not also insert directly.
	rows, err := f.db.Client.Notification.Query().
		Where(notification.KindEQ(notification.KindComment)).
		All(f.ctx)
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("comment notifications = %d, want 1", len(rows))
	}
	if rows[0].UserID != f.owner.ID || rows[0].ActorID == nil || *rows[0].ActorID != f.friend.ID {
		t.Fatalf("notification = %+v, want owner/commenter", rows[0])
	}

	// A self-comment must not notify through either path.
	calls = 0
	if _, err := f.svc.Create(f.ctx, f.owner.ID, c.ID, "self"); err != nil {
		t.Fatalf("self comment: %v", err)
	}
	if calls != 0 {
		t.Fatalf("OnNotify calls after self-comment = %d, want 0", calls)
	}
	count, err := f.db.Client.Notification.Query().
		Where(notification.KindEQ(notification.KindComment)).
		Count(f.ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("comment notifications after self-comment = %d, want 1", count)
	}
}

func TestMentionNotifications(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")
	soc := social.New(f.db)

	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "hey @carol and @Carol, also @alice and @bob"); err != nil {
		t.Fatalf("create: %v", err)
	}

	rows, err := f.db.Client.Notification.Query().All(f.ctx)
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	// One comment notification for the owner, one mention for carol. The owner
	// and the commenter are excluded from mentions; carol is deduplicated.
	if len(rows) != 2 {
		t.Fatalf("notifications = %d (%+v), want 2", len(rows), rows)
	}
	var mention *ent.Notification
	for _, n := range rows {
		if n.Kind == notification.KindMention {
			mention = n
		}
	}
	if mention == nil {
		t.Fatalf("no mention notification: %+v", rows)
	}
	if mention.UserID != f.stranger.ID || mention.ActorID == nil || *mention.ActorID != f.friend.ID {
		t.Fatalf("mention target/actor = %+v", mention)
	}
	if mention.SubjectID != c.ID || mention.Text != "mentioned you in a comment" {
		t.Fatalf("mention fields = %+v", mention)
	}

	// A block suppresses the mention but never the comment itself.
	if err := soc.Block(f.ctx, f.stranger.ID, f.friend.Handle); err != nil {
		t.Fatalf("block: %v", err)
	}
	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "blocked mention @carol"); err != nil {
		t.Fatalf("create after block: %v", err)
	}
	count, err := f.db.Client.Notification.Query().
		Where(notification.KindEQ(notification.KindMention), notification.UserID(f.stranger.ID)).
		Count(f.ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("mentions after block = %d, want 1", count)
	}
}

func TestMentionSkipsUsersWhoCannotSeeParent(t *testing.T) {
	f := newFixture(t)
	// Friends-only: carol is a stranger and must not be told about a check-in
	// she cannot open.
	c := f.checkin(t, "friends")
	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "secret @carol"); err != nil {
		t.Fatalf("create: %v", err)
	}
	count, err := f.db.Client.Notification.Query().
		Where(notification.KindEQ(notification.KindMention), notification.UserID(f.stranger.ID)).
		Count(f.ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("mentions for hidden parent = %d, want 0", count)
	}
}

func TestListHidesSoftDeletedUsers(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")

	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "still here"); err != nil {
		t.Fatalf("create: %v", err)
	}
	list, err := f.svc.List(f.ctx, f.owner.ID, c.ID)
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("live comments = %d, want 1", len(list))
	}

	if err := f.db.Client.User.UpdateOneID(f.friend.ID).
		SetDeletedAt(db.Now()).
		SetStatus(user.StatusDeleted).
		Exec(f.ctx); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	list, err = f.svc.List(f.ctx, f.owner.ID, c.ID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("comments from deleted user = %d, want 0", len(list))
	}
}

func TestListPagePaginatesOlderComments(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")

	const total = 120
	for i := 0; i < total; i++ {
		if _, err := f.db.SQL.ExecContext(f.ctx,
			`INSERT INTO comments (id, checkin_id, user_id, body, created_at) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("comment-%03d", i), c.ID, f.owner.ID,
			fmt.Sprintf("body-%03d", i),
			fmt.Sprintf("2026-01-01T00:%02d:%02dZ", i/60, i%60)); err != nil {
			t.Fatalf("insert comment %d: %v", i, err)
		}
	}

	seen := map[string]bool{}
	var pages [][]comments.Comment
	before := ""
	for page := 0; page < 3; page++ {
		items, gotTotal, hasMore, err := f.svc.ListPage(f.ctx, f.owner.ID, c.ID, before, 50)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if gotTotal != total {
			t.Fatalf("page %d total = %d, want %d", page, gotTotal, total)
		}
		wantMore := page < 2
		if hasMore != wantMore {
			t.Fatalf("page %d hasMore = %v, want %v", page, hasMore, wantMore)
		}
		wantLen := 50
		if page == 2 {
			wantLen = 20
		}
		if len(items) != wantLen {
			t.Fatalf("page %d len = %d, want %d", page, len(items), wantLen)
		}
		for i := 1; i < len(items); i++ {
			if items[i-1].CreatedAt > items[i].CreatedAt {
				t.Fatalf("page %d not oldest-first at %d: %q > %q", page, i, items[i-1].CreatedAt, items[i].CreatedAt)
			}
		}
		for _, item := range items {
			if seen[item.ID] {
				t.Fatalf("duplicate comment %s across pages", item.ID)
			}
			seen[item.ID] = true
		}
		pages = append(pages, items)
		if hasMore {
			before = items[0].ID
		}
	}
	if pages[0][0].Body != "body-070" || pages[2][0].Body != "body-000" {
		t.Fatalf("unexpected page boundaries: first=%q last=%q", pages[0][0].Body, pages[2][len(pages[2])-1].Body)
	}

	// An unknown or foreign cursor is rejected.
	if _, _, _, err := f.svc.ListPage(f.ctx, f.owner.ID, c.ID, "does-not-exist", 50); !errors.Is(err, comments.ErrInvalid) {
		t.Fatalf("invalid before = %v, want ErrInvalid", err)
	}
	other := f.checkin(t, "public")
	foreign, err := f.db.Client.Comment.Create().SetCheckinID(other.ID).SetUserID(f.owner.ID).SetBody("elsewhere").Save(f.ctx)
	if err != nil {
		t.Fatalf("foreign comment: %v", err)
	}
	if _, _, _, err := f.svc.ListPage(f.ctx, f.owner.ID, c.ID, foreign.ID, 50); !errors.Is(err, comments.ErrInvalid) {
		t.Fatalf("foreign before = %v, want ErrInvalid", err)
	}

	// List remains the newest-page wrapper.
	list, err := f.svc.List(f.ctx, f.owner.ID, c.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 50 {
		t.Fatalf("List len = %d, want 50", len(list))
	}
	if list[0].ID != pages[0][0].ID || list[len(list)-1].ID != pages[0][len(pages[0])-1].ID {
		t.Fatalf("List did not return the newest page")
	}
}

func TestMentionHandles(t *testing.T) {
	body := "@Alice hi @bob @ALICE @carol_1 @dave @erin @frank @grace"
	got := comments.MentionHandles(body)
	want := []string{"alice", "bob", "carol_1", "dave", "erin"}
	if len(got) != len(want) {
		t.Fatalf("handles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("handles = %v, want %v", got, want)
		}
	}
	for _, bad := range []string{"@ab", "@this_handle_is_far_too_long", "no mentions"} {
		if got := comments.MentionHandles(bad); len(got) != 0 {
			t.Fatalf("MentionHandles(%q) = %v, want none", bad, got)
		}
	}
}

func TestRenderBodyEscapesThenLinksMentions(t *testing.T) {
	got := string(comments.RenderBody(`<script>alert(1)</script> hi @Alice and @bob`))
	if strings.Contains(got, "<script>") {
		t.Fatalf("raw script survived: %s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("script not escaped: %s", got)
	}
	if !strings.Contains(got, `<a href="/u/alice" class="mention">@Alice</a>`) {
		t.Fatalf("alice mention not linked: %s", got)
	}
	if !strings.Contains(got, `<a href="/u/bob" class="mention">@bob</a>`) {
		t.Fatalf("bob mention not linked: %s", got)
	}
	if strings.Contains(got, "<a href=\"/u/ab\"") {
		t.Fatalf("short handle linked: %s", got)
	}
}

func TestCommentInitial(t *testing.T) {
	tests := []struct {
		name string
		c    comments.Comment
		want string
	}{
		{"display name", comments.Comment{DisplayName: "alice", Handle: "alice"}, "A"},
		{"handle fallback", comments.Comment{Handle: "bob"}, "B"},
		{"empty", comments.Comment{}, "?"},
		{"non-ascii", comments.Comment{DisplayName: "émile"}, "É"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Initial(); got != tt.want {
				t.Fatalf("Initial() = %q, want %q", got, tt.want)
			}
		})
	}
}
