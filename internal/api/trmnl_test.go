package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent/checkin"
	"sodapop/ent/report"
	"sodapop/ent/user"
)

func trmnlCheckin(t *testing.T, h *harness, userID string, index, rating int, note string) {
	t.Helper()
	ctx := context.Background()
	d, err := h.server.DB.Client.Drink.Create().
		SetName("TRMNL Cola").SetSlug("trmnl-cola-" + strconv.Itoa(index)).
		SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	create := h.server.DB.Client.Checkin.Create().
		SetUserID(userID).
		SetDrinkID(d.ID).
		SetVisibility(checkin.VisibilityPublic).
		SetNote(note)
	if rating >= 0 {
		create.SetRating(rating)
	}
	if _, err := create.Save(ctx); err != nil {
		t.Fatalf("checkin: %v", err)
	}
}

func TestTrmnlCheckinsPublic(t *testing.T) {
	h := newHarness(t)
	makePreviewPublic(t, h, h.alice)
	for i := 0; i < 25; i++ {
		trmnlCheckin(t, h, h.alice, i, 17, "line one\nline two")
	}

	// Default is 10.
	rec := h.do(t, http.MethodGet, "/api/v1/trmnl/checkins", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("default feed: got %d body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("Cache-Control = %q", got)
	}
	body := decodeBody(t, rec)
	if body["scope"] != "feed" || body["heading"] != "Recent Pours" {
		t.Errorf("scope/heading = %v/%v", body["scope"], body["heading"])
	}
	if body["item_count"].(float64) != 10 {
		t.Errorf("default item_count = %v, want 10", body["item_count"])
	}
	items, _ := body["items"].([]any)
	first, _ := items[0].(map[string]any)
	if first["rating_display"] != "4.25" {
		t.Errorf("rating_display = %v", first["rating_display"])
	}
	if first["rating_pct"].(float64) != 85 {
		t.Errorf("rating_pct = %v", first["rating_pct"])
	}
	if first["note"] != "line one line two" {
		t.Errorf("note = %q", first["note"])
	}
	if first["toasts_label"] != "0 toasts" {
		t.Errorf("toasts_label = %v", first["toasts_label"])
	}
	if body["total_toasts_label"] != "0 toasts" {
		t.Errorf("total_toasts_label = %v", body["total_toasts_label"])
	}
	href, _ := first["href"].(string)
	if !strings.HasPrefix(href, "http://localhost/checkins/") {
		t.Errorf("href = %q, want absolute", href)
	}
	if first["photo_url"] != nil {
		t.Errorf("photo_url = %v, want null", first["photo_url"])
	}
	if body["latest"] == "" || !strings.Contains(body["latest"].(string), " · ") {
		t.Errorf("latest = %v", body["latest"])
	}

	// Limit is capped at 20.
	rec = h.do(t, http.MethodGet, "/api/v1/trmnl/checkins?limit=99", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("limit cap: got %d body %s", rec.Code, rec.Body.String())
	}
	body = decodeBody(t, rec)
	if body["item_count"].(float64) != 20 {
		t.Errorf("limit cap item_count = %v, want 20", body["item_count"])
	}

	// Handle filter uses the public profile scope.
	rec = h.do(t, http.MethodGet, "/api/v1/trmnl/checkins?handle=alice&limit=5", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("handle feed: got %d body %s", rec.Code, rec.Body.String())
	}
	body = decodeBody(t, rec)
	if body["scope"] != "handle" || body["heading"] != "@alice" {
		t.Errorf("handle scope/heading = %v/%v", body["scope"], body["heading"])
	}
	if body["item_count"].(float64) != 5 {
		t.Errorf("handle item_count = %v, want 5", body["item_count"])
	}

	// A non-public profile degrades to a 200 object with an error key.
	rec = h.do(t, http.MethodGet, "/api/v1/trmnl/checkins?handle=bob", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("private handle: got %d body %s", rec.Code, rec.Body.String())
	}
	body = decodeBody(t, rec)
	if body["error"] == nil || body["item_count"].(float64) != 0 {
		t.Errorf("private handle body = %v", body)
	}
	if items, ok := body["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("private handle items = %v, want empty array", body["items"])
	}
}

func TestTrmnlNewDrinksScope(t *testing.T) {
	h := newHarness(t)
	admin := newUser(t, h, "trmnladmin", true, user.RoleAdmin)
	token := h.tokenFor(t, admin.ID)
	ctx := context.Background()

	if _, err := h.server.DB.Client.Drink.Create().
		SetName("Scope Cola").SetSlug("scope-cola").SetStatus("active").Save(ctx); err != nil {
		t.Fatalf("drink: %v", err)
	}
	if _, err := h.server.DB.Client.Venue.Create().
		SetName("Scope Venue").SetSlug("scope-venue").SetKind("bar").Save(ctx); err != nil {
		t.Fatalf("venue: %v", err)
	}
	newUser(t, h, "scopeuser", true, user.RoleUser)

	cases := []struct {
		scope    string
		label    string
		wantIn   string
		wantOuts []string
	}{
		{"drinks", "Drinks", "Scope Cola", []string{"Scope Venue", "scopeuser"}},
		{"venues", "Venues", "Scope Venue", []string{"Scope Cola", "scopeuser"}},
		{"users", "Members", "scopeuser", []string{"Scope Cola", "Scope Venue"}},
		{"all", "Everything", "Scope Cola", nil},
	}
	for _, tc := range cases {
		rec := h.doWith(t, token, http.MethodGet, "/api/v1/trmnl/new-drinks?scope="+tc.scope, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: got %d body %s", tc.scope, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("%s Cache-Control = %q", tc.scope, got)
		}
		body := decodeBody(t, rec)
		if body["scope"] != tc.scope || body["scope_label"] != tc.label {
			t.Errorf("%s scope/label = %v/%v", tc.scope, body["scope"], body["scope_label"])
		}
		wantWindow := "24h"
		if tc.scope == "users" {
			wantWindow = "7d"
		}
		if body["window_label"] != wantWindow {
			t.Errorf("%s window_label = %v, want %s", tc.scope, body["window_label"], wantWindow)
		}
		if body["added_24h"] == nil || body["pending"] == nil {
			t.Errorf("%s missing counters: %v", tc.scope, body)
		}
		titles := ""
		for _, raw := range body["items"].([]any) {
			row := raw.(map[string]any)
			titles += " " + row["title"].(string)
			if href, _ := row["href"].(string); !strings.HasPrefix(href, "http://localhost/") {
				t.Errorf("%s href = %q", tc.scope, href)
			}
		}
		if !strings.Contains(titles, tc.wantIn) {
			t.Errorf("%s titles = %q, want %q", tc.scope, titles, tc.wantIn)
		}
		for _, out := range tc.wantOuts {
			if strings.Contains(titles, out) {
				t.Errorf("%s leaked %q in %q", tc.scope, out, titles)
			}
		}
	}

	// Unknown scopes fall back to drinks.
	body := decodeBody(t, h.doWith(t, token, http.MethodGet, "/api/v1/trmnl/new-drinks?scope=bogus", ""))
	if body["scope"] != "drinks" {
		t.Errorf("bogus scope = %v, want drinks", body["scope"])
	}
}

func TestTrmnlAdminBanner(t *testing.T) {
	h := newHarness(t)
	admin := newUser(t, h, "trmnlmod", true, user.RoleModerator)
	token := h.tokenFor(t, admin.ID)
	ctx := context.Background()

	drink, err := h.server.DB.Client.Drink.Create().
		SetName("Pending Cola").SetSlug("pending-cola").SetStatus("provisional").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	if _, err := h.server.DB.Client.Report.Create().
		SetEntity(report.EntityDrink).SetEntityID(drink.ID).
		SetStatus(report.StatusOpen).Save(ctx); err != nil {
		t.Fatalf("report: %v", err)
	}

	rec := h.doWith(t, token, http.MethodGet, "/api/v1/trmnl/admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: got %d body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	body := decodeBody(t, rec)
	counts := body["counts"].(map[string]any)
	if counts["open_reports"].(float64) != 1 || counts["pending_drinks"].(float64) != 1 {
		t.Errorf("counts = %v", counts)
	}
	if body["needs_attention"] != true {
		t.Errorf("needs_attention = %v, want true", body["needs_attention"])
	}
	if body["banner"] != "1 report open · 1 drink pending" {
		t.Errorf("banner = %v", body["banner"])
	}
	if body["report_count"].(float64) != 1 {
		t.Errorf("report_count = %v", body["report_count"])
	}
	if len(body["events"].([]any)) == 0 {
		t.Error("events empty")
	}

	// A clean instance shows "All clear" and still returns the full object.
	clean := newHarness(t)
	cleanAdmin := newUser(t, clean, "cleadmin", true, user.RoleAdmin)
	cleanToken := clean.tokenFor(t, cleanAdmin.ID)
	rec = clean.doWith(t, cleanToken, http.MethodGet, "/api/v1/trmnl/admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("clean admin: got %d body %s", rec.Code, rec.Body.String())
	}
	body = decodeBody(t, rec)
	if body["needs_attention"] != false || body["banner"] != "All clear" {
		t.Errorf("clean = %v / %v", body["needs_attention"], body["banner"])
	}
	if _, ok := body["reports"].([]any); !ok {
		t.Errorf("clean reports = %v, want array", body["reports"])
	}
	if _, ok := body["events"].([]any); !ok {
		t.Errorf("clean events = %v, want array", body["events"])
	}
}

func TestTrmnlMePayload(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d, err := h.server.DB.Client.Drink.Create().
		SetName("Me Cola").SetSlug("me-cola").SetStatus("active").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	if _, err := h.server.DB.Client.Checkin.Create().
		SetUserID(h.alice).SetDrinkID(d.ID).SetRating(17).
		SetVisibility(checkin.VisibilityPublic).Save(ctx); err != nil {
		t.Fatalf("checkin: %v", err)
	}

	rec := h.doWith(t, h.token, http.MethodGet, "/api/v1/trmnl/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me: got %d body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	body := decodeBody(t, rec)
	if body["handle"] != "alice" || body["display_name"] != "Alice" {
		t.Errorf("identity = %v/%v", body["handle"], body["display_name"])
	}
	if body["pours"].(float64) != 1 {
		t.Errorf("pours = %v", body["pours"])
	}
	if body["avg_rating_display"] != "4.25" {
		t.Errorf("avg_rating_display = %v", body["avg_rating_display"])
	}
	if body["unread"].(float64) != 0 || body["has_notifications"] != false {
		t.Errorf("unread/has_notifications = %v/%v", body["unread"], body["has_notifications"])
	}
	for _, key := range []string{"notifications", "recent"} {
		if _, ok := body[key].([]any); !ok {
			t.Errorf("%s = %v, want array", key, body[key])
		}
	}
	recent := body["recent"].([]any)
	if len(recent) != 1 {
		t.Fatalf("recent = %v", recent)
	}
	row := recent[0].(map[string]any)
	if row["drink_name"] != "Me Cola" || row["rating_display"] != "4.25" {
		t.Errorf("recent row = %v", row)
	}
	if row["photo_url"] != nil {
		t.Errorf("recent photo_url = %v, want null", row["photo_url"])
	}
	if href, _ := row["href"].(string); !strings.HasPrefix(href, "http://localhost/checkins/") {
		t.Errorf("recent href = %q", href)
	}
}

func TestTrmnlAuthRequired(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/api/v1/trmnl/new-drinks", "/api/v1/trmnl/admin", "/api/v1/trmnl/me"} {
		rec := h.do(t, http.MethodGet, path, "", false)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s anonymous: got %d, want 401", path, rec.Code)
		}
	}

	// A normal member token gets the same 404 admin routes always return.
	for _, path := range []string{"/api/v1/trmnl/new-drinks", "/api/v1/trmnl/admin"} {
		rec := h.doWith(t, h.token, http.MethodGet, path, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s non-admin: got %d, want 404 (%s)", path, rec.Code, rec.Body.String())
		}
	}
}
