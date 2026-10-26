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

// TestSetupRateLimitIgnoresForwardedFor proves a spoofed header cannot mint a
// fresh setup bucket; only the socket address counts.
func TestSetupRateLimitIgnoresForwardedFor(t *testing.T) {
	h := newSetupHarness(t)
	for i := 0; i < 5; i++ {
		nonce := setupNonceFrom(t, h)
		req := setupFormPost(h, "wrong-token", nonce, "http://sodapop.test")
		req.RemoteAddr = "192.0.2.10:4567"
		req.Header.Set("X-Forwarded-For", "203.0.113."+string(rune('1'+i)))
		rec := httptest.NewRecorder()
		h.srv.setupSubmit(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("attempt %d = %d, want 403", i+1, rec.Code)
		}
	}
	nonce := setupNonceFrom(t, h)
	req := setupFormPost(h, "wrong-token", nonce, "http://sodapop.test")
	req.RemoteAddr = "192.0.2.10:4567"
	req.Header.Set("X-Forwarded-For", "198.51.100.99")
	rec := httptest.NewRecorder()
	h.srv.setupSubmit(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed X-Forwarded-For status = %d, want 429", rec.Code)
	}
}

func TestSetupSourceIPTrustedProxies(t *testing.T) {
	h := newSetupHarness(t)
	tests := []struct {
		name    string
		remote  string
		trusted []string
		xff     string
		want    string
	}{
		{
			name: "untrusted peer ignores spoofed header", remote: "192.0.2.10:4567",
			trusted: nil, xff: "203.0.113.1", want: "192.0.2.10",
		},
		{
			name: "trusted peer uses client hop", remote: "10.0.0.5:4321",
			trusted: []string{"10.0.0.0/8"}, xff: "203.0.113.7", want: "203.0.113.7",
		},
		{
			name: "trusted chain picks rightmost untrusted", remote: "10.0.0.5:1",
			trusted: []string{"10.0.0.0/8", "192.0.2.0/24"},
			xff:     "203.0.113.9, 192.0.2.50, 10.0.0.7", want: "203.0.113.9",
		},
		{
			name: "trusted peer without header falls back", remote: "10.0.0.5:1",
			trusted: []string{"10.0.0.5"}, xff: "", want: "10.0.0.5",
		},
		{
			name: "bracketed IPv6 and ports normalize", remote: "[::1]:1234",
			trusted: []string{"::1"}, xff: "[2001:db8::2]:555", want: "2001:db8::2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.srv.Cfg.TrustedProxies = tt.trusted
			req := httptest.NewRequest(http.MethodPost, "/setup", nil)
			req.RemoteAddr = tt.remote
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := h.srv.setupSourceIP(req); got != tt.want {
				t.Fatalf("setupSourceIP = %q, want %q", got, tt.want)
			}
		})
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
