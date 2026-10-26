package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProfileBlockedEitherDirectionIs404(t *testing.T) {
	for _, tc := range []struct {
		name    string
		blocker func(alice, bob string) string
	}{
		{"viewer blocks target", func(alice, bob string) string { return alice }},
		{"target blocks viewer", func(alice, bob string) string { return bob }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newV12Harness(t)
			alice := h.user(t, "alice")
			bob := h.user(t, "bob")
			// A public profile must still be indistinguishable from a missing one.
			if err := h.db.Client.UserSetting.Create().
				SetUserID(bob.ID).SetPublicProfile(true).Exec(context.Background()); err != nil {
				t.Fatalf("settings: %v", err)
			}
			if err := h.srv.Social.Block(context.Background(), tc.blocker(alice.ID, bob.ID),
				map[bool]string{true: "alice", false: "bob"}[tc.blocker(alice.ID, bob.ID) == bob.ID]); err != nil {
				t.Fatalf("block: %v", err)
			}

			h.renderWith(t, "error", `{{.Title}}:{{.Message}}`)
			req := h.request(http.MethodGet, "/u/bob", alice, h.form())
			req.SetPathValue("handle", "bob")
			rec := httptest.NewRecorder()
			h.srv.profilePage(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("profile status = %d, want 404", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "not in Sodapop") {
				t.Fatalf("body = %q, want missing-profile copy", rec.Body.String())
			}
		})
	}
}

func TestSettingsBlockedPageAndUnblock(t *testing.T) {
	h := newV12Harness(t)
	alice := h.user(t, "alice")
	bob := h.user(t, "bob")
	if err := h.srv.Social.Block(context.Background(), alice.ID, bob.Handle); err != nil {
		t.Fatalf("block: %v", err)
	}

	h.renderWith(t, "settings_blocked", `{{len .BlockedUsers}}:{{range .BlockedUsers}}{{.Handle}} {{end}}`)
	req := h.request(http.MethodGet, "/settings/blocked", alice, h.form())
	rec := httptest.NewRecorder()
	h.srv.settingsBlockedPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "1:bob " {
		t.Fatalf("body = %q, want blocked bob", got)
	}

	// The unblock form removes only the viewer's own row.
	ureq := h.request(http.MethodPost, "/u/bob/unblock", alice, h.form())
	ureq.SetPathValue("handle", "bob")
	urec := httptest.NewRecorder()
	h.srv.unblockUser(urec, ureq)
	if urec.Code != http.StatusFound {
		t.Fatalf("unblock status = %d, want 302", urec.Code)
	}
	if loc := urec.Header().Get("Location"); loc != "/settings/blocked" {
		t.Fatalf("unblock location = %q", loc)
	}
	n, err := h.db.Client.Block.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("blocks after unblock = %d, want 0", n)
	}
}

func TestBlockHandlerCreatesBlockAndRejectsSelf(t *testing.T) {
	h := newV12Harness(t)
	alice := h.user(t, "alice")
	h.user(t, "bob")

	req := h.request(http.MethodPost, "/u/bob/block", alice, h.form())
	req.SetPathValue("handle", "bob")
	rec := httptest.NewRecorder()
	h.srv.blockUser(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("block status = %d, want 302", rec.Code)
	}
	n, _ := h.db.Client.Block.Query().Count(context.Background())
	if n != 1 {
		t.Fatalf("blocks = %d, want 1", n)
	}

	self := h.request(http.MethodPost, "/u/alice/block", alice, h.form())
	self.SetPathValue("handle", "alice")
	srec := httptest.NewRecorder()
	h.srv.blockUser(srec, self)
	if srec.Code != http.StatusFound {
		t.Fatalf("self block status = %d, want 302", srec.Code)
	}
	n, _ = h.db.Client.Block.Query().Count(context.Background())
	if n != 1 {
		t.Fatalf("self block created a row: %d", n)
	}
}
