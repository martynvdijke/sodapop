package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"sodapop/ent/apitoken"
	"sodapop/ent/inboundtoken"
	"sodapop/ent/session"
	"sodapop/ent/user"
	"sodapop/internal/account"
)

func wireAccount(h *v12Harness) {
	h.srv.Account = account.New(h.db.Client, nil)
}

func TestSettingsDeleteRefusesLastLiveAdmin(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	h.srv.Render = renderer
	wireAccount(h)
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
	wireAccount(h)
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

func TestSettingsDeleteRejectsWrongConfirmation(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	h.srv.Render = renderer
	wireAccount(h)
	u := h.user(t, "careful")

	rec := httptest.NewRecorder()
	h.srv.settingsDelete(rec, h.request(http.MethodPost, "/settings/delete", u, url.Values{
		"csrf_token": {"test-csrf"},
		"confirm":    {"not-my-handle"},
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong confirm = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	row, err := h.db.Client.User.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status == user.StatusDeleted || row.DeletedAt != nil {
		t.Fatal("account was deleted despite a mismatched confirmation")
	}
}

func TestSettingsDeleteRevokesNormalUserCredentials(t *testing.T) {
	h := newV12Harness(t)
	wireAccount(h)
	ctx := context.Background()
	u := h.user(t, "normal")

	if _, err := h.srv.Auth.CreateSession(ctx, u.ID, "192.0.2.1", "agent"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.srv.Auth.CreateAPIToken(ctx, u.ID, "test", []string{"read"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Client.InboundToken.Create().
		SetUserID(u.ID).SetTokenHash("inbound-" + u.ID).Save(ctx); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.srv.settingsDelete(rec, h.request(http.MethodPost, "/settings/delete", u, url.Values{
		"csrf_token": {"test-csrf"},
		"confirm":    {u.Handle},
	}))
	if rec.Code != http.StatusFound {
		t.Fatalf("delete = %d, want 302: %s", rec.Code, rec.Body.String())
	}

	row, err := h.db.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != user.StatusDeleted || row.DeletedAt == nil {
		t.Errorf("account not closed: status=%s deleted_at=%v", row.Status, row.DeletedAt)
	}
	if n, err := h.db.Client.Session.Query().Where(session.UserID(u.ID)).Count(ctx); err != nil || n != 0 {
		t.Errorf("sessions = %d (err %v), want 0", n, err)
	}
	apiToken, err := h.db.Client.APIToken.Query().Where(apitoken.UserID(u.ID)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if apiToken.RevokedAt == nil {
		t.Error("API token was not revoked")
	}
	inbound, err := h.db.Client.InboundToken.Query().Where(inboundtoken.UserID(u.ID)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if inbound.RevokedAt == nil {
		t.Error("inbound token was not revoked")
	}
}
