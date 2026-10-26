package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"sodapop/ent/user"
)

func TestSettingsDeleteRefusesLastLiveAdmin(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	h.srv.Render = renderer
	admin := h.verifiedUser(t, "last-admin", user.RoleAdmin)

	rec := httptest.NewRecorder()
	h.srv.settingsDelete(rec, h.request(http.MethodPost, "/settings/delete", admin, url.Values{
		"csrf_token": {"test-csrf"},
		"confirm":    {admin.Handle},
	}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("last-admin delete status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if err := h.srv.deleteAccount(context.Background(), admin.ID); err != errLastLiveAdmin {
		t.Fatalf("deleteAccount err = %v, want errLastLiveAdmin", err)
	}
	row, err := h.db.Client.User.Get(context.Background(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status == user.StatusDeleted || row.DeletedAt != nil {
		t.Fatal("last live admin was deleted")
	}
}

func TestSettingsDeleteAllowsAdminWhenAnotherExists(t *testing.T) {
	h := newV12Harness(t)
	admin := h.verifiedUser(t, "first-admin", user.RoleAdmin)
	h.verifiedUser(t, "second-admin", user.RoleAdmin)

	rec := httptest.NewRecorder()
	h.srv.settingsDelete(rec, h.request(http.MethodPost, "/settings/delete", admin, url.Values{
		"csrf_token": {"test-csrf"},
		"confirm":    {admin.Handle},
	}))
	if rec.Code != http.StatusFound {
		t.Fatalf("delete with another admin status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err := h.db.Client.User.Get(context.Background(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != user.StatusDeleted {
		t.Fatalf("status = %s, want deleted", row.Status)
	}
}
