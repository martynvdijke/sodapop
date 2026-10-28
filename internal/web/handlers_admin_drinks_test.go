package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/catalogrevision"
	"sodapop/ent/drink"
	"sodapop/ent/user"
)

func TestLimitedUntilForRequiresDateForLimitedAndSeasonal(t *testing.T) {
	for _, status := range []drink.Status{drink.StatusLimited, drink.StatusSeasonal} {
		if _, err := limitedUntilFor(status, ""); !errors.Is(err, errLimitedUntilRequired) {
			t.Fatalf("%s without date: got %v, want errLimitedUntilRequired", status, err)
		}
		if _, err := limitedUntilFor(status, "   "); !errors.Is(err, errLimitedUntilRequired) {
			t.Fatalf("%s blank date: got %v, want errLimitedUntilRequired", status, err)
		}
		if _, err := limitedUntilFor(status, "soon"); !errors.Is(err, errLimitedUntilFormat) {
			t.Fatalf("%s bad date: got %v, want errLimitedUntilFormat", status, err)
		}
		got, err := limitedUntilFor(status, "2026-12-31")
		if err != nil {
			t.Fatalf("%s valid date: %v", status, err)
		}
		if got == nil {
			t.Fatalf("%s valid date: got nil", status)
		}
		want := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		if *got != want {
			t.Fatalf("%s limited_until = %q, want %q", status, *got, want)
		}
	}
}

func TestLimitedUntilForClearsForOtherStatuses(t *testing.T) {
	for _, status := range []drink.Status{drink.StatusActive, drink.StatusDiscontinued} {
		got, err := limitedUntilFor(status, "")
		if err != nil || got != nil {
			t.Fatalf("%s empty: got %v, %v; want nil, nil", status, got, err)
		}
		// A stale value is still parsed, then discarded for non-limited statuses.
		got, err = limitedUntilFor(status, "2026-12-31")
		if err != nil || got != nil {
			t.Fatalf("%s dated: got %v, %v; want nil, nil", status, got, err)
		}
		if _, err := limitedUntilFor(status, "nope"); !errors.Is(err, errLimitedUntilFormat) {
			t.Fatalf("%s invalid date: got %v, want errLimitedUntilFormat", status, err)
		}
	}
}

func adminPublishRequest(h *v12Harness, u *ent.User, drinkID int, form url.Values) *http.Request {
	req := h.request(http.MethodPost, "/admin/drinks/"+strconv.Itoa(drinkID)+"/publish", u, form)
	req.SetPathValue("id", strconv.Itoa(drinkID))
	return req
}

func TestAdminDrinkPublishTogglesAndRecordsRevision(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	admin := h.verifiedUser(t, "publish-admin", user.RoleAdmin)
	d, err := h.db.Client.Drink.Create().
		SetName("Publish Me").SetSlug("publish-me").SetStatus(drink.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.srv.adminDrinkPublish(rec, adminPublishRequest(h, admin, d.ID, url.Values{
		"csrf_token": {"test-csrf"},
		"published":  {"1"},
	}))
	if rec.Code != http.StatusFound {
		t.Fatalf("publish = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/drinks/publish-me?published=1" {
		t.Errorf("Location = %q", got)
	}
	row, err := h.db.Client.Drink.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !row.Published {
		t.Fatal("drink was not published")
	}
	if n, err := h.db.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityDrink), catalogrevision.EntityID(d.ID), catalogrevision.Action("publish")).
		Count(ctx); err != nil || n != 1 {
		t.Fatalf("publish revisions = %d (err %v), want 1", n, err)
	}

	rec = httptest.NewRecorder()
	h.srv.adminDrinkPublish(rec, adminPublishRequest(h, admin, d.ID, url.Values{
		"csrf_token": {"test-csrf"},
		"published":  {"0"},
	}))
	if rec.Code != http.StatusFound {
		t.Fatalf("unpublish = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/drinks/publish-me?published=0" {
		t.Errorf("Location = %q", got)
	}
	row, err = h.db.Client.Drink.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Published {
		t.Fatal("drink was not unpublished")
	}
	if n, err := h.db.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityDrink), catalogrevision.EntityID(d.ID), catalogrevision.Action("unpublish")).
		Count(ctx); err != nil || n != 1 {
		t.Fatalf("unpublish revisions = %d (err %v), want 1", n, err)
	}

	// Repeating the same state is a no-op and writes no revision.
	total, err := h.db.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityDrink), catalogrevision.EntityID(d.ID)).
		Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.srv.adminDrinkPublish(rec, adminPublishRequest(h, admin, d.ID, url.Values{
		"csrf_token": {"test-csrf"},
		"published":  {"0"},
	}))
	if rec.Code != http.StatusFound {
		t.Fatalf("no-op unpublish = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	after, err := h.db.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityDrink), catalogrevision.EntityID(d.ID)).
		Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after != total {
		t.Errorf("revisions after no-op = %d, want %d", after, total)
	}
}

func TestAdminDrinkPublishValidatesInputAndRole(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	admin := h.verifiedUser(t, "publish-admin", user.RoleAdmin)
	normal := h.verifiedUser(t, "publish-user", user.RoleUser)
	d, err := h.db.Client.Drink.Create().
		SetName("Validate Me").SetSlug("validate-me").SetStatus(drink.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Missing CSRF token.
	rec := httptest.NewRecorder()
	h.srv.adminDrinkPublish(rec, adminPublishRequest(h, admin, d.ID, url.Values{"published": {"1"}}))
	if rec.Code != http.StatusForbidden {
		t.Errorf("missing CSRF = %d, want 403", rec.Code)
	}

	// Invalid id.
	rec = httptest.NewRecorder()
	req := h.request(http.MethodPost, "/admin/drinks/nope/publish", admin, url.Values{
		"csrf_token": {"test-csrf"}, "published": {"1"},
	})
	req.SetPathValue("id", "nope")
	h.srv.adminDrinkPublish(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("invalid id = %d, want 404", rec.Code)
	}

	// Invalid value.
	rec = httptest.NewRecorder()
	h.srv.adminDrinkPublish(rec, adminPublishRequest(h, admin, d.ID, url.Values{
		"csrf_token": {"test-csrf"}, "published": {"2"},
	}))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid value = %d, want 400", rec.Code)
	}

	// A normal user is rejected by the route guard.
	rec = httptest.NewRecorder()
	h.srv.requireAdmin(h.srv.adminDrinkPublish)(rec, adminPublishRequest(h, normal, d.ID, url.Values{
		"csrf_token": {"test-csrf"}, "published": {"1"},
	}))
	if rec.Code != http.StatusForbidden {
		t.Errorf("normal user = %d, want 403", rec.Code)
	}
	row, err := h.db.Client.Drink.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Published {
		t.Fatal("drink was published by an invalid request")
	}
}
