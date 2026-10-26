package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/email"
	"sodapop/internal/settings"
)

type settingsHarness struct {
	*v12Harness
	settings *settings.Service
}

func newSettingsHarness(t *testing.T) *settingsHarness {
	t.Helper()
	h := newV12Harness(t)
	// Match the built-in defaults so source reporting is deterministic.
	h.srv.Cfg.AppName = "Sodapop"
	h.srv.Cfg.BaseURL = "http://localhost:8097"
	h.srv.Cfg.EmailDevDir = t.TempDir()
	svc, err := settings.New(h.db.Client, h.srv.Cfg, string(h.srv.Cfg.SessionKey), h.srv.Log)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	if err := svc.Load(context.Background()); err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	h.srv.Settings = svc
	h.srv.Render = renderer
	h.srv.Mailer = email.New(h.srv.Cfg, h.db, h.srv.Log, svc)
	return &settingsHarness{v12Harness: h, settings: svc}
}

func (h *settingsHarness) set(t *testing.T, key, value string, secret bool, by string) {
	t.Helper()
	if err := h.settings.Set(context.Background(), key, value, secret, by); err != nil {
		t.Fatalf("settings.Set(%s): %v", key, err)
	}
}

func (h *settingsHarness) post(t *testing.T, actor *ent.User, handler http.HandlerFunc, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	if form.Get("csrf_token") == "" {
		form.Set("csrf_token", "test-csrf")
	}
	req := h.request(http.MethodPost, target, actor, form)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func (h *settingsHarness) getSettings(t *testing.T, actor *ent.User) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	ctx := context.WithValue(req.Context(), userKey{}, actor)
	ctx = context.WithValue(ctx, sessionKey{}, &ent.Session{CsrfToken: "test-csrf"})
	rec := httptest.NewRecorder()
	h.srv.requireAdminStrict(h.srv.adminSettingsPage)(rec, req.WithContext(ctx))
	return rec
}

func TestAdminSettingsPageMasksSecretsAndReportsSources(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.verifiedUser(t, "settings-admin", user.RoleAdmin)
	h.set(t, settings.KeyBaseURL, "https://db.example.test", false, admin.ID)
	h.set(t, settings.KeySMTPPassword, "hunter2!", true, admin.ID)
	h.set(t, settings.KeyUmamiURL, "https://umami.example.test", false, admin.ID)

	rec := h.getSettings(t, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/settings status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`value="https://db.example.test"`,
		`data-source="db"`,
		`er2!`,
		`value="https://umami.example.test"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings page missing %q", want)
		}
	}
	if strings.Contains(body, "hunter2!") {
		t.Fatal("settings page leaked the SMTP password")
	}
}

func TestAdminSettingsStrictAdminGuard(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.verifiedUser(t, "strict-admin", user.RoleAdmin)
	mod := h.verifiedUser(t, "strict-mod", user.RoleModerator)
	regular := h.verifiedUser(t, "strict-user", user.RoleUser)

	if rec := h.getSettings(t, admin); rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200", rec.Code)
	}
	for _, u := range []*ent.User{mod, regular} {
		rec := httptest.NewRecorder()
		req := h.request(http.MethodGet, "/admin/settings", u, url.Values{})
		h.srv.requireAdminStrict(h.srv.adminSettingsPage)(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d, want 403", u.Role, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "General") {
			t.Fatalf("%s saw the settings form", u.Role)
		}
	}
}

func TestAdminSettingsGeneralSavesAndValidates(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.verifiedUser(t, "general-admin", user.RoleAdmin)

	rec := h.post(t, admin, h.srv.adminSettingsGeneral, "/admin/settings/general", url.Values{
		"site_name": {"Fizz Club"},
		"base_url":  {"https://pop.example.test/"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/settings?notice=general_saved" {
		t.Fatalf("redirect = %q", loc)
	}
	snap := h.settings.Snapshot()
	if snap.SiteName() != "Fizz Club" || snap.BaseURL() != "https://pop.example.test" {
		t.Fatalf("saved settings = %q / %q", snap.SiteName(), snap.BaseURL())
	}
	if snap.Source(settings.KeyBaseURL) != "db" {
		t.Fatalf("base URL source = %q, want db", snap.Source(settings.KeyBaseURL))
	}

	rec = h.post(t, admin, h.srv.adminSettingsGeneral, "/admin/settings/general", url.Values{
		"site_name": {"Bad"},
		"base_url":  {"not a url"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "base_url") {
		t.Fatalf("validation error missing: %s", rec.Body.String())
	}
	if h.settings.Snapshot().SiteName() != "Fizz Club" {
		t.Fatal("invalid form changed the site name")
	}

	req := h.request(http.MethodPost, "/admin/settings/general", admin, url.Values{
		"site_name": {"Hi"}, "base_url": {"https://ok.example.test"},
	})
	rec = httptest.NewRecorder()
	h.srv.adminSettingsGeneral(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", rec.Code)
	}
}

func TestAdminSettingsEmailSecretPreserveAndClear(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.verifiedUser(t, "email-admin", user.RoleAdmin)
	baseForm := func() url.Values {
		return url.Values{
			"smtp_host":     {"smtp.example.test"},
			"smtp_port":     {"2525"},
			"smtp_username": {"mailer"},
			"smtp_from":     {"Sodapop <no-reply@example.test>"},
			"smtp_tls":      {"1"},
		}
	}

	form := baseForm()
	form.Set("smtp_password", "hunter2!")
	rec := h.post(t, admin, h.srv.adminSettingsEmail, "/admin/settings/email", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	snap := h.settings.Snapshot()
	if smtp := snap.SMTP(); smtp.Host != "smtp.example.test" || smtp.Port != 2525 || smtp.TLS != "starttls" || smtp.Password != "hunter2!" {
		t.Fatalf("smtp = %+v", smtp)
	}
	if set, last4 := snap.SecretState(settings.KeySMTPPassword); !set || last4 != "er2!" {
		t.Fatalf("secret state = %v/%q", set, last4)
	}

	// A blank password keeps the stored secret.
	rec = h.post(t, admin, h.srv.adminSettingsEmail, "/admin/settings/email", baseForm())
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("blank save status = %d, want 303", rec.Code)
	}
	if got := h.settings.Snapshot().SMTP().Password; got != "hunter2!" {
		t.Fatalf("blank password changed secret to %q", got)
	}

	// Clear removes the secret.
	clearForm := baseForm()
	clearForm.Set("clear_smtp_password", "1")
	rec = h.post(t, admin, h.srv.adminSettingsEmail, "/admin/settings/email", clearForm)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("clear status = %d, want 303", rec.Code)
	}
	if set, _ := h.settings.Snapshot().SecretState(settings.KeySMTPPassword); set {
		t.Fatal("cleared password still reports as set")
	}

	// Invalid port leaves the previous value intact.
	bad := baseForm()
	bad.Set("smtp_port", "70000")
	rec = h.post(t, admin, h.srv.adminSettingsEmail, "/admin/settings/email", bad)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid port status = %d, want 400", rec.Code)
	}
	if h.settings.Snapshot().SMTP().Port != 2525 {
		t.Fatal("invalid port changed the saved value")
	}
}

func TestAdminSettingsAccessAndRegistrationClosed(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.verifiedUser(t, "access-admin", user.RoleAdmin)

	rec := h.post(t, admin, h.srv.adminSettingsAccess, "/admin/settings/access", url.Values{
		"password_fallback": {"1"},
		"allowed_emails":    {"Allowed@Example.test, second@example.test"},
		"allowed_domains":   {"example.test"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("access status = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	snap := h.settings.Snapshot()
	if snap.RegistrationOpen() {
		t.Fatal("registration should be closed when the checkbox is absent")
	}
	if !snap.AllowPasswordFallback() {
		t.Fatal("password fallback should remain enabled")
	}
	if got := snap.AllowedEmails(); len(got) != 2 || got[0] != "allowed@example.test" {
		t.Fatalf("allowed emails = %v", got)
	}

	// Closed registration renders the closed state and rejects submissions.
	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	rec = httptest.NewRecorder()
	h.srv.registerPage(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Registration is closed") {
		t.Fatalf("closed registration status/body = %d/%q", rec.Code, rec.Body.String())
	}

	form := url.Values{
		"handle": {"newbie"}, "email": {"allowed@example.test"},
		"display_name": {"New"}, "password": {"a-long-password"},
	}
	req = httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.srv.registerSubmit(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("closed registration submit = %d, want 403", rec.Code)
	}
	if n, err := h.db.Client.User.Query().Count(context.Background()); err != nil || n != 1 {
		t.Fatalf("users after closed registration = %d (err %v), want 1", n, err)
	}

	// Re-open and verify the allow-list still gates new accounts.
	h.set(t, settings.KeyRegistrationOpen, "1", false, admin.ID)
	h.set(t, settings.KeyAllowedEmails, "allowed@example.test", false, admin.ID)
	h.set(t, settings.KeyAllowedDomains, "", false, admin.ID)
	form.Set("email", "other@example.test")
	req = httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.srv.registerSubmit(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disallowed email status = %d, want 403", rec.Code)
	}
	if n, _ := h.db.Client.User.Query().Count(context.Background()); n != 1 {
		t.Fatalf("users after disallowed email = %d, want 1", n)
	}
}

func TestAdminSettingsEmailTestTargetsAdminAndUsesRuntimeSink(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.verifiedUser(t, "mail-admin", user.RoleAdmin)
	if err := os.MkdirAll(h.srv.Cfg.EmailDevDir, 0o750); err != nil {
		t.Fatal(err)
	}

	rec := h.post(t, admin, h.srv.adminSettingsEmailTest, "/admin/settings/email/test", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("email test status = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/settings?notice=test_sink" {
		t.Fatalf("redirect = %q, want development sink notice", loc)
	}
	files, err := filepath.Glob(filepath.Join(h.srv.Cfg.EmailDevDir, "*.eml"))
	if err != nil || len(files) != 1 {
		t.Fatalf("dev sink files = %v (err %v), want 1", files, err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "To: mail-admin@example.com") {
		t.Fatalf("test email did not target the admin: %s", raw)
	}
}

func TestAdminSettingsAnalyticsAndAI(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.verifiedUser(t, "analytics-admin", user.RoleAdmin)

	rec := h.post(t, admin, h.srv.adminSettingsAnalytics, "/admin/settings/analytics", url.Values{
		"umami_url": {"https://umami.example.test"}, "umami_website_id": {"site-123"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("analytics status = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	if got := h.settings.Snapshot().Umami(); got.URL != "https://umami.example.test" || got.WebsiteID != "site-123" {
		t.Fatalf("umami = %+v", got)
	}

	if rec := h.post(t, admin, h.srv.adminSettingsAI, "/admin/settings/ai", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("ai off status = %d, want 303", rec.Code)
	}
	if h.settings.Snapshot().AIEnabled() {
		t.Fatal("AI should be disabled when the checkbox is absent")
	}
	if rec := h.post(t, admin, h.srv.adminSettingsAI, "/admin/settings/ai", url.Values{"ai_enabled": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("ai on status = %d, want 303", rec.Code)
	}
	if !h.settings.Snapshot().AIEnabled() {
		t.Fatal("AI should be enabled when the checkbox is present")
	}
}

func TestWebBaseAndCSPReadRuntimeSettings(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.verifiedUser(t, "runtime-admin", user.RoleAdmin)
	h.set(t, settings.KeyAppName, "Runtime Pop", false, admin.ID)
	h.set(t, settings.KeyBaseURL, "https://runtime.example.test", false, admin.ID)
	h.set(t, settings.KeyUmamiURL, "https://metrics.example.test", false, admin.ID)
	h.set(t, settings.KeyUmamiWebsiteID, "runtime-site", false, admin.ID)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	data := h.srv.base(req)
	if data["AppName"] != "Runtime Pop" {
		t.Fatalf("AppName = %v", data["AppName"])
	}
	if data["UmamiURL"] != "https://metrics.example.test" || data["UmamiID"] != "runtime-site" {
		t.Fatalf("umami data = %v / %v", data["UmamiURL"], data["UmamiID"])
	}

	rec := httptest.NewRecorder()
	h.srv.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://metrics.example.test") {
		t.Fatalf("CSP missing runtime Umami origin: %s", csp)
	}
}
