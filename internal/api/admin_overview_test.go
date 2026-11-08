package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"sodapop/ent/checkin"
	"sodapop/ent/report"
	"sodapop/ent/user"
)

func TestAdminOverviewAPI(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := newUser(t, h, "overadmin", true, user.RoleAdmin)
	token := h.tokenFor(t, admin.ID)

	drink, err := h.server.DB.Client.Drink.Create().
		SetName("Cola").SetSlug("cola-overview").SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	if _, err := h.server.DB.Client.Checkin.Create().
		SetUserID(admin.ID).SetDrinkID(drink.ID).
		SetVisibility(checkin.VisibilityPublic).Save(ctx); err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if _, err := h.server.DB.Client.Report.Create().
		SetEntity(report.EntityDrink).SetEntityID(drink.ID).SetStatus(report.StatusOpen).
		Save(ctx); err != nil {
		t.Fatalf("report: %v", err)
	}

	rec := h.doWith(t, token, http.MethodGet, "/api/v1/admin/overview", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin overview: got %d body %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	rows, _ := body["data"].([]any)
	if len(rows) == 0 {
		t.Fatalf("overview returned no events: %s", rec.Body.String())
	}
	meta, _ := body["meta"].(map[string]any)
	if meta == nil {
		t.Fatalf("overview meta missing: %s", rec.Body.String())
	}
	if _, ok := meta["next_before"]; !ok {
		t.Fatalf("meta.next_before missing: %s", rec.Body.String())
	}

	// Non-admins get the same 404 shape as other admin routes, never 403.
	for _, path := range []string{"/api/v1/admin/overview", "/api/v1/admin/overview/counts"} {
		rec = h.do(t, http.MethodGet, path, "", true)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s non-admin: got %d, want 404 (%s)", path, rec.Code, rec.Body.String())
		}
		errBody := decodeBody(t, rec)
		apiErr, _ := errBody["error"].(map[string]any)
		if apiErr["code"] != "not_found" {
			t.Fatalf("%s non-admin error = %v, want not_found", path, errBody)
		}
	}

	// kind filter keeps only the requested source.
	rec = h.doWith(t, token, http.MethodGet, "/api/v1/admin/overview?kind=drink", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("kind filter: got %d body %s", rec.Code, rec.Body.String())
	}
	for _, raw := range dataList(t, rec) {
		row, _ := raw.(map[string]any)
		if row["kind"] != "drink" {
			t.Fatalf("kind filter leaked %v", row["kind"])
		}
	}

	// All-unknown kinds produce an empty page, not everything.
	rec = h.doWith(t, token, http.MethodGet, "/api/v1/admin/overview?kind=bogus", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown kind: got %d body %s", rec.Code, rec.Body.String())
	}
	if rows := dataList(t, rec); len(rows) != 0 {
		t.Fatalf("unknown kind returned %d rows", len(rows))
	}

	// Malformed cursor is rejected rather than silently widening the page.
	rec = h.doWith(t, token, http.MethodGet, "/api/v1/admin/overview?before=garbage", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor: got %d, want 400 (%s)", rec.Code, rec.Body.String())
	}

	// The page size is clamped to 100 even when the caller asks for more.
	for i := 0; i < 105; i++ {
		if _, err := h.server.DB.Client.User.Create().
			SetHandle(fmt.Sprintf("bulk%03d", i)).
			SetEmail(fmt.Sprintf("bulk%03d@example.com", i)).Save(ctx); err != nil {
			t.Fatalf("bulk user %d: %v", i, err)
		}
	}
	rec = h.doWith(t, token, http.MethodGet, "/api/v1/admin/overview?kind=user&limit=999", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("limit clamp: got %d body %s", rec.Code, rec.Body.String())
	}
	rows = dataList(t, rec)
	if len(rows) != 100 {
		t.Fatalf("limit clamp returned %d rows, want 100", len(rows))
	}
	meta, _ = decodeBody(t, rec)["meta"].(map[string]any)
	if meta["next_before"] == "" {
		t.Fatal("limit clamp page is missing next_before")
	}

	// Counts endpoint.
	rec = h.doWith(t, token, http.MethodGet, "/api/v1/admin/overview/counts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("counts: got %d body %s", rec.Code, rec.Body.String())
	}
	counts := dataMap(t, rec)
	if counts["checkins_24h"].(float64) != 1 {
		t.Fatalf("checkins_24h = %v, want 1", counts["checkins_24h"])
	}
	if counts["open_reports"].(float64) != 1 {
		t.Fatalf("open_reports = %v, want 1", counts["open_reports"])
	}
}
