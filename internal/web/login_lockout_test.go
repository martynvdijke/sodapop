package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sodapop/ent/loginattempt"
	"sodapop/internal/auth"
)

func loginRequest(identifier, password string) *http.Request {
	form := url.Values{"identifier": {identifier}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestLoginLockoutKeepsGenericResponse(t *testing.T) {
	h := newV12Harness(t)
	h.srv.Cfg.PasswordFallback = true
	h.renderWith(t, "login", `{{.Error}}`)
	email := "locked@example.com"
	ctx := context.Background()
	for i := 0; i < auth.LoginFailureLimit; i++ {
		if err := h.srv.Auth.RecordLoginFailure(ctx, email); err != nil {
			t.Fatalf("failure %d: %v", i, err)
		}
	}

	rec := httptest.NewRecorder()
	h.srv.loginSubmit(rec, loginRequest(email, "wrong-password"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	body := strings.ToLower(rec.Body.String())
	if !strings.Contains(body, "combination did not match") {
		t.Fatalf("body = %q, want the generic credentials message", rec.Body.String())
	}
	if strings.Contains(body, "locked") || strings.Contains(body, "too many") {
		t.Fatalf("lockout state leaked: %q", rec.Body.String())
	}
}

func TestLoginFailureRecordedAndSuccessClears(t *testing.T) {
	h := newV12Harness(t)
	h.srv.Cfg.PasswordFallback = true
	h.renderWith(t, "login", `{{.Error}}`)
	ctx := context.Background()
	u := h.user(t, "loginuser")
	hash, err := h.srv.Auth.HashPassword("correct horse")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := h.db.Client.User.UpdateOneID(u.ID).SetPasswordHash(hash).Save(ctx); err != nil {
		t.Fatalf("set password: %v", err)
	}
	key := loginattempt.Key(auth.LoginKey(u.Email))

	rec := httptest.NewRecorder()
	h.srv.loginSubmit(rec, loginRequest(u.Email, "wrong"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401", rec.Code)
	}
	if n, err := h.db.Client.LoginAttempt.Query().Where(key).Count(ctx); err != nil || n != 1 {
		t.Fatalf("failures after wrong password = %d, %v; want 1", n, err)
	}

	rec = httptest.NewRecorder()
	h.srv.loginSubmit(rec, loginRequest(u.Email, "correct horse"))
	if rec.Code != http.StatusFound {
		t.Fatalf("correct password status = %d, want 302", rec.Code)
	}
	if n, err := h.db.Client.LoginAttempt.Query().Where(key).Count(ctx); err != nil || n != 0 {
		t.Fatalf("failures after success = %d, %v; want 0", n, err)
	}
}
