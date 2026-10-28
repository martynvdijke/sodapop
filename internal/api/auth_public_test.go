package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sodapop/ent/emailverificationtoken"
	"sodapop/ent/passwordresettoken"
	"sodapop/ent/user"
	"sodapop/internal/auth"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/ratelimit"
	"sodapop/internal/settings"
)

// newAuthHarness builds a server with no seeded accounts, for tests that need
// the first-account bootstrap path.
func newAuthHarness(t *testing.T) *harness {
	t.Helper()
	cfg := &config.Config{
		AppName:          "Sodapop",
		Env:              "test",
		BaseURL:          "http://localhost",
		Version:          "test",
		SessionKey:       []byte("0123456789abcdef0123456789abcdef"),
		SessionTTL:       time.Hour,
		BcryptCost:       10,
		RateLimitPerMin:  120,
		PasswordFallback: true,
		DataDir:          t.TempDir(),
	}
	database, err := db.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	authSvc := auth.New(cfg, database)
	if err := authSvc.InitOIDC(context.Background()); err != nil {
		t.Fatalf("oidc init: %v", err)
	}
	srv := &Server{
		Cfg:     cfg,
		Auth:    authSvc,
		Limiter: ratelimit.New(),
		DB:      database,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	srv.Register(mux)
	return &harness{server: srv, mux: mux}
}

func (h *harness) runtimeSettings(t *testing.T) *settings.Service {
	t.Helper()
	svc, err := settings.New(h.server.DB.Client, h.server.Cfg, "", h.server.Log)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	h.server.Auth.Settings = svc
	return svc
}

func TestPublicAuthConfigShape(t *testing.T) {
	h := newAuthHarness(t)
	rec := h.do(t, http.MethodGet, "/api/v1/auth/config", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("config = %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			RegistrationOpen          bool `json:"registration_open"`
			PasswordFallback          bool `json:"password_fallback"`
			OIDCEnabled               bool `json:"oidc_enabled"`
			EmailVerificationRequired bool `json:"email_verification_required"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Data.RegistrationOpen || !payload.Data.PasswordFallback {
		t.Errorf("unexpected config: %+v", payload.Data)
	}
	if payload.Data.OIDCEnabled {
		t.Error("oidc_enabled = true with OIDC disabled")
	}
	if !payload.Data.EmailVerificationRequired {
		t.Error("email_verification_required = false")
	}
}

func TestPublicAuthRegisterSuccess(t *testing.T) {
	h := newHarness(t)
	h.server.Cfg.PasswordFallback = true
	rec := h.do(t, http.MethodPost, "/api/v1/auth/register",
		`{"email":"  Carol@Example.COM ","handle":" Carol_1 ","display_name":"Carol","password":"longenough1"}`, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("register response leaked a token: %s", rec.Body.String())
	}
	var payload struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Status != "verification_sent" {
		t.Errorf("status = %q, want verification_sent", payload.Data.Status)
	}
	ctx := context.Background()
	u, err := h.server.DB.Client.User.Query().Where(user.Email("carol@example.com")).Only(ctx)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if u.Handle != "carol_1" || u.EmailVerifiedAt != nil {
		t.Errorf("unexpected user: handle=%q verified=%v", u.Handle, u.EmailVerifiedAt)
	}
	tokens, err := h.server.DB.Client.EmailVerificationToken.Query().
		Where(emailverificationtoken.UserID(u.ID)).Count(ctx)
	if err != nil || tokens != 1 {
		t.Fatalf("verification tokens = %d, err = %v", tokens, err)
	}
}

func TestPublicAuthFirstAccountIsVerifiedAdmin(t *testing.T) {
	h := newAuthHarness(t)
	rec := h.do(t, http.MethodPost, "/api/v1/auth/register",
		`{"email":"owner@example.com","handle":"owner","password":"longenough1"}`, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Status != "created" {
		t.Errorf("status = %q, want created", payload.Data.Status)
	}
	u, err := h.server.DB.Client.User.Query().Where(user.Handle("owner")).Only(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != user.RoleAdmin || u.EmailVerifiedAt == nil {
		t.Errorf("bootstrap owner role=%q verified=%v", u.Role, u.EmailVerifiedAt)
	}
}

func TestPublicAuthRegisterValidation(t *testing.T) {
	h := newAuthHarness(t)
	for _, body := range []string{
		`{"email":"valid@example.com","handle":"AB","password":"longenough1"}`,
		`{"email":"valid@example.com","handle":"ab","password":"longenough1"}`,
		`{"email":"not-an-email","handle":"valid_handle","password":"longenough1"}`,
		`{"email":"valid@example.com","handle":"valid_handle","password":"short"}`,
	} {
		rec := h.do(t, http.MethodPost, "/api/v1/auth/register", body, false)
		if rec.Code != http.StatusBadRequest || !containsCode(rec.Body.Bytes(), "invalid_request") {
			t.Errorf("%s: got %d %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestPublicAuthRegisterConflict(t *testing.T) {
	h := newHarness(t)
	h.server.Cfg.PasswordFallback = true
	rec := h.do(t, http.MethodPost, "/api/v1/auth/register",
		`{"email":"alice@example.com","handle":"alice2","password":"longenough1"}`, false)
	if rec.Code != http.StatusConflict || !containsCode(rec.Body.Bytes(), "registration_conflict") {
		t.Fatalf("email conflict = %d %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodPost, "/api/v1/auth/register",
		`{"email":"new@example.com","handle":"alice","password":"longenough1"}`, false)
	if rec.Code != http.StatusConflict || !containsCode(rec.Body.Bytes(), "registration_conflict") {
		t.Fatalf("handle conflict = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPublicAuthRegistrationClosed(t *testing.T) {
	h := newAuthHarness(t)
	svc := h.runtimeSettings(t)
	if err := svc.Set(context.Background(), settings.KeyRegistrationOpen, "0", false, "test"); err != nil {
		t.Fatal(err)
	}
	rec := h.do(t, http.MethodPost, "/api/v1/auth/register",
		`{"email":"new@example.com","handle":"newuser","password":"longenough1"}`, false)
	if rec.Code != http.StatusForbidden || !containsCode(rec.Body.Bytes(), "registration_closed") {
		t.Fatalf("closed registration = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPublicAuthEmailNotAllowedViaSnapshot(t *testing.T) {
	h := newAuthHarness(t)
	svc := h.runtimeSettings(t)
	if err := svc.Set(context.Background(), settings.KeyAllowedEmails, "allowed@example.com", false, "test"); err != nil {
		t.Fatal(err)
	}
	rec := h.do(t, http.MethodPost, "/api/v1/auth/register",
		`{"email":"other@example.com","handle":"other","password":"longenough1"}`, false)
	if rec.Code != http.StatusForbidden || !containsCode(rec.Body.Bytes(), "email_not_allowed") {
		t.Fatalf("disallowed email = %d %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodPost, "/api/v1/auth/register",
		`{"email":"allowed@example.com","handle":"allowed","password":"longenough1"}`, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("allowed email = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPublicAuthPasswordFallbackDisabled(t *testing.T) {
	h := newAuthHarness(t)
	h.server.Cfg.PasswordFallback = false
	rec := h.do(t, http.MethodPost, "/api/v1/auth/register",
		`{"email":"new@example.com","handle":"newuser","password":"longenough1"}`, false)
	if rec.Code != http.StatusForbidden || !containsCode(rec.Body.Bytes(), "password_login_disabled") {
		t.Fatalf("disabled fallback = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPublicAuthVerify(t *testing.T) {
	h := newHarness(t)
	h.server.Cfg.PasswordFallback = true
	ctx := context.Background()
	hash, err := h.server.Auth.HashPassword("longenough1")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := h.server.Auth.RegisterLocal(ctx, nil, auth.RegisterInput{
		Handle: "vera", Email: "vera@example.com", PasswordHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := reg.VerificationToken

	rec := h.do(t, http.MethodPost, "/api/v1/auth/verify", `{"token":"`+raw+`"}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify = %d: %s", rec.Code, rec.Body.String())
	}
	u, err := h.server.DB.Client.User.Get(ctx, reg.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.EmailVerifiedAt == nil {
		t.Error("user not marked verified")
	}

	// Single use.
	rec = h.do(t, http.MethodPost, "/api/v1/auth/verify", `{"token":"`+raw+`"}`, false)
	if rec.Code != http.StatusBadRequest || !containsCode(rec.Body.Bytes(), "invalid_token") {
		t.Fatalf("second verify = %d %s", rec.Code, rec.Body.String())
	}

	// Expired token.
	expiredRaw := auth.RandomToken(32)
	if _, err := h.server.DB.Client.EmailVerificationToken.Create().
		SetTokenHash(auth.HashToken(expiredRaw)).
		SetUserID(reg.User.ID).
		SetEmail("vera@example.com").
		SetExpiresAt(time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)).
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	rec = h.do(t, http.MethodPost, "/api/v1/auth/verify", `{"token":"`+expiredRaw+`"}`, false)
	if rec.Code != http.StatusBadRequest || !containsCode(rec.Body.Bytes(), "invalid_token") {
		t.Fatalf("expired verify = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPublicAuthResendIsGeneric(t *testing.T) {
	h := newAuthHarness(t)
	rec := h.do(t, http.MethodPost, "/api/v1/auth/verify/resend", `{"email":"nobody@example.com"}`, false)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("resend unknown = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"sent"`) {
		t.Errorf("resend body = %s", rec.Body.String())
	}
}

func TestPublicAuthResendReplacesToken(t *testing.T) {
	h := newHarness(t)
	h.server.Cfg.PasswordFallback = true
	ctx := context.Background()
	hash, err := h.server.Auth.HashPassword("longenough1")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := h.server.Auth.RegisterLocal(ctx, nil, auth.RegisterInput{
		Handle: "wendy", Email: "wendy@example.com", PasswordHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := h.do(t, http.MethodPost, "/api/v1/auth/verify/resend", `{"email":"wendy@example.com"}`, false)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("resend = %d: %s", rec.Code, rec.Body.String())
	}
	old, err := h.server.DB.Client.EmailVerificationToken.Query().
		Where(emailverificationtoken.TokenHash(auth.HashToken(reg.VerificationToken))).Only(ctx)
	if err != nil || old.UsedAt == nil {
		t.Fatalf("old token not retired: err=%v", err)
	}
	fresh, err := h.server.DB.Client.EmailVerificationToken.Query().
		Where(emailverificationtoken.UserID(reg.User.ID), emailverificationtoken.UsedAtIsNil()).Count(ctx)
	if err != nil || fresh != 1 {
		t.Fatalf("unused tokens = %d, err = %v", fresh, err)
	}
}

func TestPublicAuthPasswordResetRequestIsGeneric(t *testing.T) {
	h := newAuthHarness(t)
	rec := h.do(t, http.MethodPost, "/api/v1/auth/password/reset", `{"email":"nobody@example.com"}`, false)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("reset unknown = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"sent"`) {
		t.Errorf("reset body = %s", rec.Body.String())
	}
}

func TestPublicAuthPasswordResetConfirm(t *testing.T) {
	h := newAuthHarness(t)
	ctx := context.Background()
	hash, err := h.server.Auth.HashPassword("oldpassword1")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := h.server.Auth.RegisterLocal(ctx, nil, auth.RegisterInput{
		Handle: "rita", Email: "rita@example.com", PasswordHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := auth.RandomToken(32)
	if _, err := h.server.DB.Client.PasswordResetToken.Create().
		SetTokenHash(auth.HashToken(raw)).
		SetUserID(reg.User.ID).
		SetExpiresAt(time.Now().UTC().Add(time.Hour).Format(time.RFC3339)).
		Save(ctx); err != nil {
		t.Fatal(err)
	}

	// Weak password is rejected but the token stays usable.
	rec := h.do(t, http.MethodPost, "/api/v1/auth/password/reset/confirm",
		`{"token":"`+raw+`","new_password":"short"}`, false)
	if rec.Code != http.StatusBadRequest || !containsCode(rec.Body.Bytes(), "weak_password") {
		t.Fatalf("weak reset = %d %s", rec.Code, rec.Body.String())
	}
	// Invalid token.
	rec = h.do(t, http.MethodPost, "/api/v1/auth/password/reset/confirm",
		`{"token":"not-a-token","new_password":"brandnewpass"}`, false)
	if rec.Code != http.StatusBadRequest || !containsCode(rec.Body.Bytes(), "invalid_token") {
		t.Fatalf("invalid reset = %d %s", rec.Code, rec.Body.String())
	}
	// Success.
	rec = h.do(t, http.MethodPost, "/api/v1/auth/password/reset/confirm",
		`{"token":"`+raw+`","new_password":"brandnewpass"}`, false)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body.String())
	}
	u, err := h.server.DB.Client.User.Get(ctx, reg.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash == nil || !auth.CheckPassword(*u.PasswordHash, "brandnewpass") {
		t.Error("new password not stored")
	}
	row, err := h.server.DB.Client.PasswordResetToken.Query().
		Where(passwordresettoken.TokenHash(auth.HashToken(raw))).Only(ctx)
	if err != nil || row.UsedAt == nil {
		t.Errorf("reset token not marked used: err=%v", err)
	}
}

func TestPublicAuthRateLimited(t *testing.T) {
	h := newAuthHarness(t)
	for i := 0; i < 5; i++ {
		rec := h.do(t, http.MethodGet, "/api/v1/auth/config", "", false)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d", i, rec.Code)
		}
	}
	rec := h.do(t, http.MethodGet, "/api/v1/auth/config", "", false)
	if rec.Code != http.StatusTooManyRequests || !containsCode(rec.Body.Bytes(), "rate_limited") {
		t.Fatalf("sixth request = %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
}
