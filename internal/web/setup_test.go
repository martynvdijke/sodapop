package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sodapop/ent/user"
	"sodapop/internal/setup"
)

const webSetupToken = "web-setup-token"

func newSetupHarness(t *testing.T) *v12Harness {
	t.Helper()
	h := newV12Harness(t)
	h.srv.Setup = setup.New(h.db.Client, h.srv.Auth, h.srv.Limiter, h.srv.Log, webSetupToken)
	h.renderWith(t, "setup", `NONCE={{.Nonce}} ERROR={{.Error}}`)
	return h
}

func setupNonceFrom(t *testing.T, h *v12Harness) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	h.srv.setupPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /setup = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), webSetupToken) {
		t.Fatalf("setup token leaked into HTML: %q", rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == setupNonceCookie {
			payload, err := h.srv.Auth.VerifyValue(c.Value)
			if err != nil {
				t.Fatalf("verify nonce cookie: %v", err)
			}
			return string(payload)
		}
	}
	t.Fatal("GET /setup did not set a nonce cookie")
	return ""
}

func setupFormPost(h *v12Harness, token, nonce, origin string) *http.Request {
	form := url.Values{
		"token":        {token},
		"handle":       {"firstadmin"},
		"email":        {"first@example.com"},
		"password":     {"longpassword"},
		"display_name": {"First Admin"},
		"setup_nonce":  {nonce},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "sodapop.test"
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if nonce != "" {
		req.AddCookie(&http.Cookie{
			Name:  setupNonceCookie,
			Value: h.srv.Auth.SignValue([]byte(nonce)),
		})
	}
	return req
}

func TestSetupPageAndValidClaim(t *testing.T) {
	h := newSetupHarness(t)
	nonce := setupNonceFrom(t, h)

	rec := httptest.NewRecorder()
	h.srv.setupSubmit(rec, setupFormPost(h, webSetupToken, nonce, "http://sodapop.test"))
	if rec.Code != http.StatusFound {
		t.Fatalf("POST /setup = %d, want 302 (body %q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/admin/settings" {
		t.Fatalf("redirect = %q, want /admin/settings", got)
	}
	var sessionCookie bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sd_session" && c.Value != "" {
			sessionCookie = true
		}
	}
	if !sessionCookie {
		t.Fatal("successful setup did not set a session cookie")
	}

	ctx := context.Background()
	u, err := h.db.Client.User.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query admin: %v", err)
	}
	if u.Role != user.RoleAdmin || u.Status != user.StatusActive || u.EmailVerifiedAt == nil {
		t.Fatalf("admin = role %s status %s verified %v", u.Role, u.Status, u.EmailVerifiedAt)
	}

	rec = httptest.NewRecorder()
	h.srv.setupPage(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /setup after claim = %d, want 404", rec.Code)
	}
}

func TestSetupRejectsBadTokenAndNonce(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		origin     string
		useNonce   bool
		wantStatus int
	}{
		{"wrong token", "wrong-token", "http://sodapop.test", true, http.StatusForbidden},
		{"missing token", "", "http://sodapop.test", true, http.StatusForbidden},
		{"cross origin", webSetupToken, "https://evil.example", true, http.StatusForbidden},
		{"missing nonce", webSetupToken, "http://sodapop.test", false, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newSetupHarness(t)
			nonce := setupNonceFrom(t, h)
			if !tt.useNonce {
				nonce = ""
			}
			rec := httptest.NewRecorder()
			h.srv.setupSubmit(rec, setupFormPost(h, tt.token, nonce, tt.origin))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.token != "" && strings.Contains(rec.Body.String(), tt.token) {
				t.Fatalf("submitted token leaked into HTML: %q", rec.Body.String())
			}
			if n, _ := h.db.Client.User.Query().Count(context.Background()); n != 0 {
				t.Fatalf("users = %d, want 0", n)
			}
		})
	}
}

func TestSetupTamperedNonceRejected(t *testing.T) {
	h := newSetupHarness(t)
	nonce := setupNonceFrom(t, h)
	req := setupFormPost(h, webSetupToken, nonce, "http://sodapop.test")
	req.Header.Del("Cookie")
	req.AddCookie(&http.Cookie{Name: setupNonceCookie, Value: "tampered"})
	rec := httptest.NewRecorder()
	h.srv.setupSubmit(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSetupRateLimitedOverHTTP(t *testing.T) {
	h := newSetupHarness(t)
	for i := 0; i < 5; i++ {
		nonce := setupNonceFrom(t, h)
		rec := httptest.NewRecorder()
		h.srv.setupSubmit(rec, setupFormPost(h, "wrong-token", nonce, "http://sodapop.test"))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("attempt %d = %d, want 403", i+1, rec.Code)
		}
	}
	nonce := setupNonceFrom(t, h)
	rec := httptest.NewRecorder()
	h.srv.setupSubmit(rec, setupFormPost(h, "wrong-token", nonce, "http://sodapop.test"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt = %d, want 429", rec.Code)
	}
}

func TestSetupHiddenWhenAdminExists(t *testing.T) {
	h := newSetupHarness(t)
	u, err := h.db.Client.User.Create().
		SetHandle("existing").SetEmail("existing@example.com").
		SetRole(user.RoleAdmin).SetEmailVerifiedAt("2026-01-01T00:00:00Z").Save(context.Background())
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	rec := httptest.NewRecorder()
	h.srv.setupPage(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /setup = %d, want 404", rec.Code)
	}
	_ = u
}
