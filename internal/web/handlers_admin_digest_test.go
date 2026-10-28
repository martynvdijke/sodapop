package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sodapop/internal/integrations"
)

func TestAdminDigestPageShowsRecipientsAndNextRun(t *testing.T) {
	h := newV12Harness(t)
	actor := h.user(t, "digest-admin")
	h.srv.Integrations = integrations.New(h.db, h.srv.Cfg, h.srv.Log)

	if _, err := h.db.Client.UserSetting.Create().
		SetUserID(actor.ID).
		SetEmailWeekly(true).
		Save(t.Context()); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if _, err := h.db.Client.EmailOutbox.Create().
		SetToEmail("digest-admin@example.com").
		SetTemplate("weekly_digest").
		SetPayloadJSON("{}").
		SetNextAttemptAt(time.Now().UTC().Format(time.RFC3339)).
		Save(t.Context()); err != nil {
		t.Fatalf("outbox: %v", err)
	}

	h.srv.Render = rendererForPage(t, "admin_digest",
		`{{.Recipients}}|{{.Pending}}|{{.NextRun}}|{{.LastRun}}`)
	req := httptest.NewRequest(http.MethodGet, "/admin/digest", nil)
	req = withTestUser(req, actor)
	rec := httptest.NewRecorder()
	h.srv.adminDigestPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !bodyContainsAll(body, "1|1|", "09:00 UTC", "Never") {
		t.Fatalf("page body = %q", body)
	}
}

func TestAdminDigestRedirectsInsteadOfErroring(t *testing.T) {
	h := newV12Harness(t)
	actor := h.user(t, "digest-admin")
	h.srv.Integrations = integrations.New(h.db, h.srv.Cfg, h.srv.Log)

	req := h.request(http.MethodPost, "/admin/digest", actor, h.form())
	rec := httptest.NewRecorder()
	h.srv.adminDigest(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/digest?sent=1" {
		t.Fatalf("redirect = %q, want /admin/digest?sent=1", loc)
	}
}
