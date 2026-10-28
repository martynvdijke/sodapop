package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"sodapop/ent/user"
	"sodapop/internal/account"
	"sodapop/internal/db"
	"sodapop/internal/integrations"
)

func newAccountHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.server.Integrations = integrations.New(h.server.DB, h.server.Cfg, h.server.Log)
	h.server.Account = account.New(h.server.DB.Client, h.server.Integrations)
	return h
}

func TestMeExportMatchesService(t *testing.T) {
	h := newAccountHarness(t)
	ctx := context.Background()

	want, err := h.server.Account.Export(ctx, h.alice)
	if err != nil {
		t.Fatalf("service export: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/me/export", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="sodapop-export.json"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Errorf("Pragma = %q, want no-cache", got)
	}
	if strings.Contains(rec.Body.String(), `"data"`) {
		t.Error("export response was wrapped in a data envelope")
	}

	// exported_at is a live timestamp, so compare the documents without it.
	var gotDoc, wantDoc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &gotDoc); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if err := json.Unmarshal(want, &wantDoc); err != nil {
		t.Fatalf("decode service export: %v", err)
	}
	delete(gotDoc, "exported_at")
	delete(wantDoc, "exported_at")
	if !reflect.DeepEqual(gotDoc, wantDoc) {
		t.Error("API export does not match the shared account service output")
	}
}

func TestMeExportDoesNotRequireVerifiedEmail(t *testing.T) {
	h := newAccountHarness(t)
	if err := h.server.DB.Client.User.UpdateOneID(h.alice).
		ClearEmailVerifiedAt().SetUpdatedAt(db.Now()).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := h.do(t, http.MethodGet, "/api/v1/me/export", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("unverified export = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestMeDeleteRequiresMatchingHandle(t *testing.T) {
	h := newAccountHarness(t)
	for _, body := range []string{`{"confirm":"wrong"}`, `{}`} {
		rec := h.do(t, http.MethodDelete, "/api/v1/me", body, true)
		if rec.Code != http.StatusBadRequest || !containsCode(rec.Body.Bytes(), "invalid_request") {
			t.Errorf("%s: status = %d, want 400 invalid_request: %s", body, rec.Code, rec.Body.String())
		}
	}
	row, err := h.server.DB.Client.User.Get(context.Background(), h.alice)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status == user.StatusDeleted {
		t.Error("account was deleted after a mismatched confirmation")
	}
}

func TestMeDeleteClosesAccountAndRevokesToken(t *testing.T) {
	h := newAccountHarness(t)
	rec := h.do(t, http.MethodDelete, "/api/v1/me", `{"confirm":"alice"}`, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	row, err := h.server.DB.Client.User.Get(context.Background(), h.alice)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != user.StatusDeleted || row.DeletedAt == nil {
		t.Errorf("account not closed: status=%s deleted_at=%v", row.Status, row.DeletedAt)
	}

	rec = h.do(t, http.MethodGet, "/api/v1/me", "", true)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token still authenticates after deletion: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMeDeleteRefusesSoleLiveAdmin(t *testing.T) {
	h := newAccountHarness(t)
	if err := h.server.DB.Client.User.UpdateOneID(h.alice).
		SetRole(user.RoleAdmin).SetUpdatedAt(db.Now()).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := h.do(t, http.MethodDelete, "/api/v1/me", `{"confirm":"alice"}`, true)
	if rec.Code != http.StatusConflict || !containsCode(rec.Body.Bytes(), "last_admin") {
		t.Fatalf("sole admin delete = %d, want 409 last_admin: %s", rec.Code, rec.Body.String())
	}
	row, err := h.server.DB.Client.User.Get(context.Background(), h.alice)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status == user.StatusDeleted {
		t.Error("sole live admin was deleted")
	}
}
