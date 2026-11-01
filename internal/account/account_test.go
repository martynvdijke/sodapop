package account_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"sodapop/ent"
	"sodapop/ent/apitoken"
	"sodapop/ent/comment"
	"sodapop/ent/follow"
	"sodapop/ent/inboundtoken"
	"sodapop/ent/notification"
	"sodapop/ent/pushdevice"
	"sodapop/ent/session"
	"sodapop/ent/user"
	"sodapop/ent/useridentity"
	"sodapop/internal/account"
	"sodapop/internal/db"
)

func newTestService(t *testing.T) (*account.Service, *db.DB) {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "account.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return account.New(database.Client, nil, database.SQL), database
}

func makeUser(t *testing.T, database *db.DB, handle string, role user.Role) *ent.User {
	t.Helper()
	u, err := database.Client.User.Create().
		SetHandle(handle).
		SetEmail(handle + "@example.com").
		SetDisplayName(handle).
		SetRole(role).
		SetEmailVerifiedAt(db.Now()).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create %s: %v", handle, err)
	}
	return u
}

func addCredentials(t *testing.T, database *db.DB, userID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := database.Client.Session.Create().
		SetTokenHash("session-" + userID).
		SetUserID(userID).
		SetCsrfToken("csrf").
		SetExpiresAt(db.Now()).
		Save(ctx); err != nil {
		t.Fatalf("session: %v", err)
	}
	if _, err := database.Client.APIToken.Create().
		SetUserID(userID).
		SetName("test").
		SetTokenHash("api-" + userID).
		SetScopes("read,write").
		Save(ctx); err != nil {
		t.Fatalf("api token: %v", err)
	}
	if _, err := database.Client.InboundToken.Create().
		SetUserID(userID).
		SetTokenHash("inbound-" + userID).
		Save(ctx); err != nil {
		t.Fatalf("inbound token: %v", err)
	}
	if _, err := database.Client.PushDevice.Create().
		SetUserID(userID).
		SetToken("device-" + userID).
		SetPlatform(pushdevice.PlatformAndroid).
		Save(ctx); err != nil {
		t.Fatalf("push device: %v", err)
	}
	if _, err := database.SQL.ExecContext(ctx,
		`INSERT INTO share_links (id, user_id, kind, token, created_at) VALUES (?, ?, ?, ?, ?)`,
		"share-"+userID, userID, "passport", "token-"+userID, db.Now()); err != nil {
		t.Fatalf("share link: %v", err)
	}
}

func TestDeleteClosesAccountAndRevokesCredentials(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	u := makeUser(t, database, "alice", user.RoleUser)
	addCredentials(t, database, u.ID)

	if err := svc.Delete(ctx, u.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	row, err := database.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if row.Status != user.StatusDeleted {
		t.Errorf("status = %s, want deleted", row.Status)
	}
	if row.DeletedAt == nil {
		t.Error("deleted_at was not set")
	}

	if n, err := database.Client.Session.Query().Where(session.UserID(u.ID)).Count(ctx); err != nil || n != 0 {
		t.Errorf("sessions = %d (err %v), want 0", n, err)
	}
	apiToken, err := database.Client.APIToken.Query().Where(apitoken.UserID(u.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("api token: %v", err)
	}
	if apiToken.RevokedAt == nil {
		t.Error("API token was not revoked")
	}
	inbound, err := database.Client.InboundToken.Query().Where(inboundtoken.UserID(u.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("inbound token: %v", err)
	}
	if inbound.RevokedAt == nil {
		t.Error("inbound token was not revoked")
	}
	if n, err := database.Client.PushDevice.Query().Where(pushdevice.UserID(u.ID)).Count(ctx); err != nil || n != 0 {
		t.Errorf("push devices = %d (err %v), want 0", n, err)
	}
	var revoked sql.NullString
	if err := database.SQL.QueryRowContext(ctx,
		`SELECT revoked_at FROM share_links WHERE user_id = ?`, u.ID).Scan(&revoked); err != nil {
		t.Fatalf("share link: %v", err)
	}
	if !revoked.Valid {
		t.Error("share link was not revoked")
	}
}

func TestDeleteAnonymisesTheAccount(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	u := makeUser(t, database, "alice", user.RoleUser)
	peer := makeUser(t, database, "bob", user.RoleUser)
	addCredentials(t, database, u.ID)

	passwordHash := "$2a$12$notarealhash"
	if _, err := database.Client.User.UpdateOneID(u.ID).
		SetBio("lives in a van").
		SetCountry("NL").
		SetPasswordHash(passwordHash).
		Save(ctx); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	if _, err := database.Client.UserIdentity.Create().
		SetProvider("oidc").SetSubject("subject-alice").SetUserID(u.ID).
		SetEmail("alice@idp.example").Save(ctx); err != nil {
		t.Fatalf("identity: %v", err)
	}
	if _, err := database.Client.Follow.Create().
		SetFollowerID(u.ID).SetFolloweeID(peer.ID).Save(ctx); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if _, err := database.Client.Notification.Create().
		SetUserID(u.ID).SetKind("system").SetText("hello alice").Save(ctx); err != nil {
		t.Fatalf("notification: %v", err)
	}
	if _, err := database.Client.Comment.Create().
		SetCheckinID("checkin-1").SetUserID(u.ID).SetBody("nice fizz").
		Save(ctx); err != nil {
		t.Fatalf("comment: %v", err)
	}

	if err := svc.Delete(ctx, u.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	row, err := database.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if row.Email == "alice@example.com" || !strings.HasSuffix(row.Email, "@deleted.invalid") {
		t.Errorf("email = %q, want an anonymised address", row.Email)
	}
	if row.Handle == "alice" || !strings.HasPrefix(row.Handle, "deleted-") {
		t.Errorf("handle = %q, want an anonymised handle", row.Handle)
	}
	if row.DisplayName != "Deleted user" {
		t.Errorf("display name = %q, want %q", row.DisplayName, "Deleted user")
	}
	if row.Bio != "" {
		t.Errorf("bio = %q, want empty", row.Bio)
	}
	if row.Country != nil {
		t.Errorf("country = %v, want nil", row.Country)
	}
	if row.PasswordHash != nil && *row.PasswordHash != "" {
		t.Error("password hash survived deletion")
	}
	if row.EmailVerifiedAt != nil {
		t.Error("email verification survived deletion")
	}

	if n, err := database.Client.UserIdentity.Query().Where(useridentity.UserID(u.ID)).Count(ctx); err != nil || n != 0 {
		t.Errorf("identities = %d (err %v), want 0", n, err)
	}
	if n, err := database.Client.Follow.Query().Where(follow.FollowerID(u.ID)).Count(ctx); err != nil || n != 0 {
		t.Errorf("follows = %d (err %v), want 0", n, err)
	}
	if n, err := database.Client.Notification.Query().Where(notification.UserID(u.ID)).Count(ctx); err != nil || n != 0 {
		t.Errorf("notifications = %d (err %v), want 0", n, err)
	}

	// Content other people can still see is kept, which is why the user row has
	// to survive as an anonymous marker.
	if n, err := database.Client.Comment.Query().Where(comment.UserID(u.ID)).Count(ctx); err != nil || n != 1 {
		t.Errorf("comments = %d (err %v), want 1 kept", n, err)
	}
}

func TestDeleteRefusesLastLiveAdmin(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	admin := makeUser(t, database, "admin", user.RoleAdmin)
	addCredentials(t, database, admin.ID)

	if err := svc.Delete(ctx, admin.ID); !errors.Is(err, account.ErrLastAdmin) {
		t.Fatalf("Delete err = %v, want ErrLastAdmin", err)
	}

	row, err := database.Client.User.Get(ctx, admin.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if row.Status != user.StatusActive || row.DeletedAt != nil {
		t.Error("last live admin was modified")
	}
	if n, err := database.Client.Session.Query().Where(session.UserID(admin.ID)).Count(ctx); err != nil || n != 1 {
		t.Errorf("sessions = %d (err %v), want 1", n, err)
	}
	apiToken, err := database.Client.APIToken.Query().Where(apitoken.UserID(admin.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("api token: %v", err)
	}
	if apiToken.RevokedAt != nil {
		t.Error("API token was revoked despite the refusal")
	}
}

func TestDeleteUnknownUser(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.Delete(context.Background(), "missing"); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("Delete err = %v, want ErrNotFound", err)
	}
}

func TestDeleteAllowsAdminWhenAnotherExists(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	first := makeUser(t, database, "first-admin", user.RoleAdmin)
	makeUser(t, database, "second-admin", user.RoleAdmin)
	addCredentials(t, database, first.ID)

	if err := svc.Delete(ctx, first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	row, err := database.Client.User.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if row.Status != user.StatusDeleted {
		t.Errorf("status = %s, want deleted", row.Status)
	}
	if n, err := database.Client.Session.Query().Where(session.UserID(first.ID)).Count(ctx); err != nil || n != 0 {
		t.Errorf("sessions = %d (err %v), want 0", n, err)
	}
	apiToken, err := database.Client.APIToken.Query().Where(apitoken.UserID(first.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("api token: %v", err)
	}
	if apiToken.RevokedAt == nil {
		t.Error("API token was not revoked for a deletable admin")
	}
	inbound, err := database.Client.InboundToken.Query().Where(inboundtoken.UserID(first.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("inbound token: %v", err)
	}
	if inbound.RevokedAt == nil {
		t.Error("inbound token was not revoked for a deletable admin")
	}
}

func TestDeleteAlreadyDeletedUser(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	u := makeUser(t, database, "gone", user.RoleUser)
	addCredentials(t, database, u.ID)
	if _, err := database.Client.User.UpdateOneID(u.ID).
		SetStatus(user.StatusDeleted).
		SetDeletedAt(db.Now()).
		SetUpdatedAt(db.Now()).
		Save(ctx); err != nil {
		t.Fatalf("pre-delete user: %v", err)
	}

	if err := svc.Delete(ctx, u.ID); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("Delete err = %v, want ErrNotFound", err)
	}
	// The refusal must not touch the credentials still on the row.
	if n, err := database.Client.Session.Query().Where(session.UserID(u.ID)).Count(ctx); err != nil || n != 1 {
		t.Errorf("sessions = %d (err %v), want 1", n, err)
	}
	apiToken, err := database.Client.APIToken.Query().Where(apitoken.UserID(u.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("api token: %v", err)
	}
	if apiToken.RevokedAt != nil {
		t.Error("API token was revoked for an already-deleted account")
	}
}

// TestDeleteAdminRaceLeavesOneLiveAdmin runs two admin deletions concurrently.
// The conditional UPDATE makes the guard atomic: whichever transaction writes
// second observes the first deletion and refuses.
func TestDeleteAdminRaceLeavesOneLiveAdmin(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	first := makeUser(t, database, "race-one", user.RoleAdmin)
	second := makeUser(t, database, "race-two", user.RoleAdmin)

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{first.ID, second.ID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			errs <- svc.Delete(ctx, id)
		}(id)
	}
	wg.Wait()
	close(errs)

	var deleted, refused int
	for err := range errs {
		switch {
		case err == nil:
			deleted++
		case errors.Is(err, account.ErrLastAdmin):
			refused++
		default:
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	if deleted != 1 || refused != 1 {
		t.Fatalf("deleted=%d refused=%d, want exactly one of each", deleted, refused)
	}

	n, err := database.Client.User.Query().
		Where(
			user.RoleEQ(user.RoleAdmin),
			user.StatusEQ(user.StatusActive),
			user.DeletedAtIsNil(),
		).
		Count(ctx)
	if err != nil {
		t.Fatalf("count live admins: %v", err)
	}
	if n < 1 {
		t.Errorf("live admins after race = %d, want at least 1", n)
	}
}
