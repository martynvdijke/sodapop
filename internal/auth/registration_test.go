package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"sodapop/ent/emailverificationtoken"
	"sodapop/ent/list"
	"sodapop/ent/user"
	"sodapop/ent/usersetting"
)

func registerInput(t *testing.T, s *Service, handle, email string) RegisterInput {
	t.Helper()
	hash, err := s.HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return RegisterInput{
		Handle:       handle,
		Email:        email,
		DisplayName:  handle,
		PasswordHash: hash,
	}
}

func assertAccountScaffolding(t *testing.T, s *Service, userID string) {
	t.Helper()
	ctx := context.Background()
	if n, err := s.Client.UserSetting.Query().Where(usersetting.UserID(userID)).Count(ctx); err != nil || n != 1 {
		t.Fatalf("settings count = %d, err = %v, want 1", n, err)
	}
	wishlist, err := s.Client.List.Query().
		Where(list.UserID(userID), list.Slug("wishlist")).
		Only(ctx)
	if err != nil {
		t.Fatalf("wishlist: %v", err)
	}
	if wishlist.Name != "Wishlist" || !wishlist.IsSystem || wishlist.Visibility != list.VisibilityPrivate {
		t.Fatalf("wishlist = %+v, want private system wishlist", wishlist)
	}
	if n, err := s.Client.EmailVerificationToken.Query().
		Where(emailverificationtoken.UserID(userID)).Count(ctx); err != nil || n != 1 {
		t.Fatalf("verification tokens = %d, err = %v, want 1", n, err)
	}
}

func TestRegisterLocalFirstUserIsBootstrapAdmin(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	res, err := s.RegisterLocal(ctx, nil, registerInput(t, s, "owner", "owner@example.com"))
	if err != nil {
		t.Fatalf("RegisterLocal: %v", err)
	}
	if res.User.Role != user.RoleAdmin {
		t.Errorf("role = %q, want admin", res.User.Role)
	}
	if res.User.Status != user.StatusActive {
		t.Errorf("status = %q, want active", res.User.Status)
	}
	if res.User.EmailVerifiedAt == nil || *res.User.EmailVerifiedAt == "" {
		t.Error("first user should be email-verified")
	}
	assertAccountScaffolding(t, s, res.User.ID)

	tok, err := s.Client.EmailVerificationToken.Query().
		Where(emailverificationtoken.UserID(res.User.ID)).
		Only(ctx)
	if err != nil {
		t.Fatalf("read verification token: %v", err)
	}
	if tok.TokenHash != HashToken(res.VerificationToken) {
		t.Error("stored token hash does not match the returned raw token")
	}
	if tok.Email != "owner@example.com" {
		t.Errorf("token email = %q, want owner@example.com", tok.Email)
	}
	expires, err := time.Parse(time.RFC3339, tok.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expiry: %v", err)
	}
	if ttl := time.Until(expires); ttl < 23*time.Hour || ttl > 25*time.Hour {
		t.Errorf("token ttl = %v, want about 24h", ttl)
	}
}

func TestRegisterLocalSecondUserIsUnverifiedUser(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	if _, err := s.RegisterLocal(ctx, nil, registerInput(t, s, "owner", "owner@example.com")); err != nil {
		t.Fatalf("first RegisterLocal: %v", err)
	}
	res, err := s.RegisterLocal(ctx, nil, registerInput(t, s, "second", "second@example.com"))
	if err != nil {
		t.Fatalf("second RegisterLocal: %v", err)
	}
	if res.User.Role != user.RoleUser {
		t.Errorf("role = %q, want user", res.User.Role)
	}
	if res.User.Status != user.StatusActive {
		t.Errorf("status = %q, want active", res.User.Status)
	}
	if res.User.EmailVerifiedAt != nil {
		t.Errorf("email_verified_at = %q, want nil", *res.User.EmailVerifiedAt)
	}
	assertAccountScaffolding(t, s, res.User.ID)
}

func TestRegisterLocalSequentialOnlyFirstIsAdmin(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	first, err := s.RegisterLocal(ctx, nil, registerInput(t, s, "owner", "owner@example.com"))
	if err != nil {
		t.Fatalf("first RegisterLocal: %v", err)
	}
	second, err := s.RegisterLocal(ctx, nil, registerInput(t, s, "later", "later@example.com"))
	if err != nil {
		t.Fatalf("second RegisterLocal: %v", err)
	}
	if first.User.Role != user.RoleAdmin {
		t.Errorf("first role = %q, want admin", first.User.Role)
	}
	if second.User.Role != user.RoleUser {
		t.Errorf("second role = %q, want user", second.User.Role)
	}
	admins, err := s.Client.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(ctx)
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if admins != 1 {
		t.Errorf("admin count = %d, want exactly 1", admins)
	}
}

func TestRegisterLocalExistingUsersNeverBootstrapAdmin(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	createUser(t, s, "existing", "existing@example.com", "password-123")

	res, err := s.RegisterLocal(ctx, nil, registerInput(t, s, "newbie", "newbie@example.com"))
	if err != nil {
		t.Fatalf("RegisterLocal: %v", err)
	}
	if res.User.Role != user.RoleUser {
		t.Errorf("role = %q, want user", res.User.Role)
	}
	if res.User.EmailVerifiedAt != nil {
		t.Error("new user should not be auto-verified when users already exist")
	}
}

func TestRegisterLocalDuplicateHandleAndEmail(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	if _, err := s.RegisterLocal(ctx, nil, registerInput(t, s, "owner", "owner@example.com")); err != nil {
		t.Fatalf("first RegisterLocal: %v", err)
	}
	for _, in := range []RegisterInput{
		registerInput(t, s, "owner", "other@example.com"),
		registerInput(t, s, "other", "owner@example.com"),
	} {
		if _, err := s.RegisterLocal(ctx, nil, in); !errors.Is(err, ErrRegistrationConflict) {
			t.Errorf("RegisterLocal(%s, %s) error = %v, want ErrRegistrationConflict", in.Handle, in.Email, err)
		}
	}
	n, err := s.Client.User.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Errorf("user count = %d, want 1 after rejected duplicates", n)
	}
}

func TestRegisterLocalRollbackLeavesNoRows(t *testing.T) {
	s, database := newTestService(t)
	ctx := context.Background()

	// Force the last write in the transaction to fail so every earlier insert
	// must roll back.
	if _, err := database.SQL.ExecContext(ctx,
		`CREATE TRIGGER fail_wishlist_insert BEFORE INSERT ON lists
		 BEGIN SELECT RAISE(ABORT, 'forced list failure'); END;`,
	); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := s.RegisterLocal(ctx, nil, registerInput(t, s, "doomed", "doomed@example.com")); err == nil {
		t.Fatal("RegisterLocal succeeded, want forced failure")
	}

	checks := []struct {
		name  string
		count func() (int, error)
	}{
		{"users", func() (int, error) { return s.Client.User.Query().Count(ctx) }},
		{"settings", func() (int, error) { return s.Client.UserSetting.Query().Count(ctx) }},
		{"lists", func() (int, error) { return s.Client.List.Query().Count(ctx) }},
		{"verification tokens", func() (int, error) { return s.Client.EmailVerificationToken.Query().Count(ctx) }},
	}
	for _, check := range checks {
		n, err := check.count()
		if err != nil {
			t.Fatalf("count %s: %v", check.name, err)
		}
		if n != 0 {
			t.Errorf("%s count = %d, want 0 after rollback", check.name, n)
		}
	}
}
