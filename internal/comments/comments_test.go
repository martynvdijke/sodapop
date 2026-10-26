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

func TestListCapsAtNewest200OldestFirst(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")

	const total = 201
	for i := 0; i < total; i++ {
		if _, err := f.db.SQL.ExecContext(f.ctx,
			`INSERT INTO comments (id, checkin_id, user_id, body, created_at) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("comment-%03d", i), c.ID, f.owner.ID,
			fmt.Sprintf("body-%03d", i),
			fmt.Sprintf("2026-01-01T00:%02d:%02dZ", i/60, i%60)); err != nil {
			t.Fatalf("insert comment %d: %v", i, err)
		}
	}

	list, err := f.svc.List(f.ctx, f.owner.ID, c.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != total-1 {
		t.Fatalf("comments = %d, want %d", len(list), total-1)
	}
	if list[0].Body != "body-001" {
		t.Fatalf("first body = %q, want body-001 (oldest of the kept window)", list[0].Body)
	}
	if list[len(list)-1].Body != "body-200" {
		t.Fatalf("last body = %q, want body-200", list[len(list)-1].Body)
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].CreatedAt > list[i].CreatedAt {
			t.Fatalf("comments not oldest-first at %d: %q > %q", i, list[i-1].CreatedAt, list[i].CreatedAt)
		}
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
