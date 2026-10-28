package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"sodapop/ent/user"
	"sodapop/internal/backup"
)

func newSystemHarness(t *testing.T) *v12Harness {
	t.Helper()
	h := newV12Harness(t)
	h.srv.Cfg.Version = "9.9.9-test"
	h.srv.Cfg.SessionKey = []byte("super-secret-session-key-do-not-render")
	h.srv.Cfg.SMTPPass = "smtp-secret-do-not-render"
	h.srv.Cfg.DataDir = t.TempDir()
	h.srv.Backup = backup.New(h.db, filepath.Join(t.TempDir(), "backups"), 2)
	h.renderWith(t, "admin_system", `VERSION={{.Version}}|DB={{.DBPath}}|BACKUPS={{len .Backups}}|NOTICE={{.Notice}}|ERROR={{.Error}}|COUNTS={{len .Counts}}|GO={{.GoVersion}}`)
	return h
}

func systemMux(h *v12Harness) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/system", h.srv.requireAdminStrict(h.srv.adminSystemPage))
	mux.HandleFunc("POST /admin/system/backup", h.srv.requireAdminStrict(h.srv.adminBackupCreate))
	return mux
}

func TestAdminSystemPageVisibleToAdminOnly(t *testing.T) {
	h := newSystemHarness(t)
	admin := h.verifiedUser(t, "system-admin", user.RoleAdmin)
	mux := systemMux(h)

	req := h.request(http.MethodGet, "/admin/system", admin, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "VERSION=9.9.9-test") {
		t.Fatalf("page missing version: %s", body)
	}
	if !strings.Contains(body, "COUNTS=6") {
		t.Fatalf("page missing counts: %s", body)
	}
	for _, secret := range []string{"super-secret-session-key-do-not-render", "smtp-secret-do-not-render"} {
		if strings.Contains(body, secret) {
			t.Fatalf("page leaked secret %q", secret)
		}
	}

	moderator := h.verifiedUser(t, "system-mod", user.RoleModerator)
	req = h.request(http.MethodGet, "/admin/system", moderator, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("moderator status = %d, want 403", rec.Code)
	}

	req = h.request(http.MethodGet, "/admin/system", nil, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("anonymous status = %d, want redirect", rec.Code)
	}
}

func TestAdminBackupCreateRedirectsAndRendersNotice(t *testing.T) {
	h := newSystemHarness(t)
	admin := h.verifiedUser(t, "system-backup", user.RoleAdmin)
	mux := systemMux(h)

	req := h.request(http.MethodPost, "/admin/system/backup", admin, h.form())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/system?backup=ok" {
		t.Fatalf("Location = %q", loc)
	}

	req = h.request(http.MethodGet, "/admin/system?backup=ok", admin, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "BACKUPS=1") {
		t.Fatalf("snapshot not listed: %s", body)
	}
	if !strings.Contains(body, "NOTICE=Backup created.") {
		t.Fatalf("notice missing: %s", body)
	}
}

func TestAdminBackupCreateRequiresCSRF(t *testing.T) {
	h := newSystemHarness(t)
	admin := h.verifiedUser(t, "system-csrf", user.RoleAdmin)
	mux := systemMux(h)

	req := h.request(http.MethodPost, "/admin/system/backup", admin, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
