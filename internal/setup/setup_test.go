package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"sodapop/ent/appsetting"
	"sodapop/ent/session"
	"sodapop/ent/user"
	"sodapop/ent/usersetting"
	"sodapop/internal/auth"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/ratelimit"
)

func newSetupService(t *testing.T, envToken string) (*Service, *db.DB, *bytes.Buffer) {
	return newSetupServiceSeeded(t, envToken, false)
}

func newSetupServiceSeeded(t *testing.T, envToken string, seedAdmin bool) (*Service, *db.DB, *bytes.Buffer) {
	t.Helper()
	dsn := fmt.Sprintf("setup-%d?mode=memory&cache=shared&", time.Now().UnixNano())
	database, err := db.Init(dsn)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if seedAdmin {
		if _, err := database.Client.User.Create().
			SetHandle("root").SetEmail("root@example.com").
			SetRole(user.RoleAdmin).SetEmailVerifiedAt(db.Now()).Save(context.Background()); err != nil {
			t.Fatalf("seed admin: %v", err)
		}
	}
	cfg := &config.Config{
		BaseURL:    "https://sodapop.test",
		SessionKey: []byte("test-session-secret"),
		SessionTTL: time.Hour,
		BcryptCost: 4,
	}
	authSvc := auth.New(cfg, database)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := New(database.Client, authSvc, ratelimit.New(), log, envToken)
	return s, database, &logs
}

func TestRequiredAndGeneratedToken(t *testing.T) {
	s, _, logs := newSetupService(t, "")
	ctx := context.Background()

	required, err := s.Required(ctx)
	if err != nil || !required {
		t.Fatalf("Required = %v, %v; want true", required, err)
	}
	token := s.SetupToken()
	if token == "" {
		t.Fatal("generated token is empty")
	}
	if !s.TokenValid(token) {
		t.Fatal("generated token should validate")
	}
	if s.TokenValid("") || s.TokenValid(token+"x") {
		t.Fatal("wrong/empty token should not validate")
	}
	if !bytes.Contains(logs.Bytes(), []byte("setup required: open /setup and use token "+token)) {
		t.Fatalf("boot log missing generated token: %q", logs.String())
	}
}

func TestAdminExistsDisablesSetup(t *testing.T) {
	s, _, logs := newSetupServiceSeeded(t, "env-token", true)
	required, err := s.Required(context.Background())
	if err != nil || required {
		t.Fatalf("Required = %v, %v; want false", required, err)
	}
	if s.SetupToken() != "" {
		t.Fatal("a service constructed with an admin must not keep a token")
	}
	if bytes.Contains(logs.Bytes(), []byte("setup required")) {
		t.Fatalf("unexpected setup log: %q", logs.String())
	}
}

func TestEnvTokenIsUsedAndNotLogged(t *testing.T) {
	const envToken = "env-secret-setup-token"
	s, _, logs := newSetupService(t, envToken)
	if !s.TokenValid(envToken) {
		t.Fatal("env token should validate")
	}
	if got := s.SetupToken(); got != envToken {
		t.Fatalf("SetupToken = %q, want env token", got)
	}
	if bytes.Contains(logs.Bytes(), []byte(envToken)) || bytes.Contains(logs.Bytes(), []byte("setup required")) {
		t.Fatalf("env token must not be logged: %q", logs.String())
	}
}

func TestWrongTokenIsGenericAndRateLimited(t *testing.T) {
	const envToken = "env-token"
	s, database, _ := newSetupService(t, envToken)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := s.Claim(ctx, "wrong", "alice", "alice@example.com", "longpassword", "")
		if !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("attempt %d err = %v, want ErrInvalidToken", i+1, err)
		}
	}
	_, err := s.Claim(ctx, "wrong", "alice", "alice@example.com", "longpassword", "")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("6th attempt err = %v, want ErrRateLimited", err)
	}
	if n, _ := database.Client.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(ctx); n != 0 {
		t.Fatalf("admins = %d, want 0", n)
	}
}

// TestRateLimitIsPerSourceOnly ensures an unauthenticated source cannot drain a
// shared setup bucket and lock everyone else out.
func TestRateLimitIsPerSourceOnly(t *testing.T) {
	const envToken = "env-token"
	s, _, _ := newSetupService(t, envToken)
	ctx := context.Background()

	for ip := 1; ip <= 5; ip++ {
		for attempt := 0; attempt < 5; attempt++ {
			_, _, err := s.ClaimSession(ctx, "wrong", "alice", "alice@example.com", "longpassword", "",
				fmt.Sprintf("192.0.2.%d:1234", ip), "")
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("ip %d attempt %d err = %v, want ErrInvalidToken", ip, attempt+1, err)
			}
		}
	}
	_, _, err := s.ClaimSession(ctx, "wrong", "alice", "alice@example.com", "longpassword", "",
		"198.51.100.7:1234", "")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("fresh source err = %v, want ErrInvalidToken (no global bucket)", err)
	}
}

func TestSetupReopensAfterLastAdminDemotedWithLatch(t *testing.T) {
	const envToken = "env-token"
	s, database, _ := newSetupServiceSeeded(t, envToken, true)
	ctx := context.Background()
	if _, err := database.Client.AppSetting.Create().
		SetKey(setupCompleteKey).SetValue("1").Save(ctx); err != nil {
		t.Fatalf("seed setup latch: %v", err)
	}
	if _, err := database.Client.User.Update().
		Where(user.RoleEQ(user.RoleAdmin)).
		SetRole(user.RoleUser).Save(ctx); err != nil {
		t.Fatalf("demote last admin: %v", err)
	}

	required, err := s.Required(ctx)
	if err != nil || !required {
		t.Fatalf("Required after demotion = %v, %v; want true despite latch", required, err)
	}
	if !s.TokenValid(envToken) {
		t.Fatal("env token should be lazily installed when setup reopens")
	}
	if _, err := s.Claim(ctx, envToken, "replacement", "replacement@example.com", "longpassword", ""); err != nil {
		t.Fatalf("recovery claim: %v", err)
	}
	n, err := CountLiveAdmins(ctx, database.Client)
	if err != nil || n != 1 {
		t.Fatalf("live admins after recovery = %d, %v; want 1", n, err)
	}
	required, err = s.Required(ctx)
	if err != nil || required {
		t.Fatalf("Required after recovery = %v, %v; want false", required, err)
	}
}

func TestValidClaimCreatesVerifiedAdminAndSession(t *testing.T) {
	const envToken = "env-token"
	s, database, _ := newSetupService(t, envToken)
	ctx := context.Background()

	u, err := s.Claim(ctx, envToken, "Admin_User", "Admin@Example.com", "correct horse", "")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if u.Role != user.RoleAdmin || u.Status != user.StatusActive || u.EmailVerifiedAt == nil {
		t.Fatalf("user role/status/verified = %s/%s/%v", u.Role, u.Status, u.EmailVerifiedAt)
	}
	if u.Handle != "admin_user" || u.Email != "admin@example.com" || u.DisplayName != "admin_user" {
		t.Fatalf("normalised user = %+v", u)
	}
	if u.PasswordHash == nil || !auth.CheckPassword(*u.PasswordHash, "correct horse") {
		t.Fatal("password hash was not stored")
	}
	if n, _ := database.Client.UserSetting.Query().Where(usersetting.UserID(u.ID)).Count(ctx); n != 1 {
		t.Fatalf("user settings = %d, want 1", n)
	}
	if n, _ := database.Client.Session.Query().Where(session.UserID(u.ID)).Count(ctx); n != 1 {
		t.Fatalf("sessions = %d, want 1", n)
	}
	row, err := database.Client.AppSetting.Query().Where(appsetting.KeyEQ(setupCompleteKey)).Only(ctx)
	if err != nil || row.Value != "1" {
		t.Fatalf("setup_complete = %+v, %v", row, err)
	}

	required, err := s.Required(ctx)
	if err != nil || required {
		t.Fatalf("Required after claim = %v, %v; want false", required, err)
	}
	if _, err := s.Claim(ctx, envToken, "second", "second@example.com", "longpassword", ""); !errors.Is(err, ErrSetupComplete) {
		t.Fatalf("second claim err = %v, want ErrSetupComplete", err)
	}
}

func TestConflictsAreGenericAndRollBackSetup(t *testing.T) {
	const envToken = "env-token"
	s, database, _ := newSetupService(t, envToken)
	ctx := context.Background()
	if _, err := database.Client.User.Create().
		SetHandle("taken").SetEmail("taken@example.com").SetDisplayName("Taken").
		Save(ctx); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if _, err := s.Claim(ctx, envToken, "taken", "fresh@example.com", "longpassword", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("handle conflict err = %v, want ErrConflict", err)
	}
	if _, err := s.Claim(ctx, envToken, "fresh", "taken@example.com", "longpassword", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("email conflict err = %v, want ErrConflict", err)
	}
	if n, _ := database.Client.AppSetting.Query().Where(appsetting.KeyEQ(setupCompleteKey)).Count(ctx); n != 0 {
		t.Fatalf("setup_complete was created despite conflicts: %d", n)
	}
	if _, err := s.Claim(ctx, envToken, "fresh", "fresh@example.com", "longpassword", ""); err != nil {
		t.Fatalf("valid claim after conflicts: %v", err)
	}
}

func TestConcurrentClaimsCreateExactlyOneAdmin(t *testing.T) {
	const envToken = "env-token"
	s, database, _ := newSetupService(t, envToken)
	ctx := context.Background()

	const attempts = 5
	var wg sync.WaitGroup
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := s.ClaimSession(ctx, envToken,
				fmt.Sprintf("admin%d", i), fmt.Sprintf("admin%d@example.com", i),
				"longpassword", "", fmt.Sprintf("10.0.0.%d", i), "")
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrSetupComplete) || errors.Is(err, ErrConflict):
			// Losers get generic completion/conflict errors.
		default:
			t.Fatalf("unexpected concurrent claim error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful claims = %d, want 1", successes)
	}
	if n, _ := database.Client.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(ctx); n != 1 {
		t.Fatalf("admins = %d, want 1", n)
	}
	if n, _ := database.Client.AppSetting.Query().Where(appsetting.KeyEQ(setupCompleteKey)).Count(ctx); n != 1 {
		t.Fatalf("setup_complete rows = %d, want 1", n)
	}
}

func TestInvalidInput(t *testing.T) {
	const envToken = "env-token"
	s, database, _ := newSetupService(t, envToken)
	ctx := context.Background()
	cases := []struct{ handle, email, password string }{
		{"AB", "a@example.com", "longpassword"},
		{"valid_handle", "not-an-email", "longpassword"},
		{"valid_handle", "a@example.com", "short"},
	}
	for _, c := range cases {
		if _, err := s.Claim(ctx, envToken, c.handle, c.email, c.password, ""); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Claim(%q,%q) err = %v, want ErrInvalidInput", c.handle, c.email, err)
		}
	}
	if n, _ := database.Client.User.Query().Count(ctx); n != 0 {
		t.Fatalf("users = %d, want 0", n)
	}
}
