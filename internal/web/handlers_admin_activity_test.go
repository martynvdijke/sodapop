package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"sodapop/ent/checkin"
	"sodapop/ent/user"
)

func newActivityHarness(t *testing.T) *v12Harness {
	t.Helper()
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	return h
}

func activityMux(h *v12Harness) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/activity", h.srv.requireAdmin(h.srv.adminActivityPage))
	return mux
}

func TestAdminActivityPageVisibleToAdminAndModerator(t *testing.T) {
	h := newActivityHarness(t)
	admin := h.verifiedUser(t, "activity-admin", user.RoleAdmin)
	owner := h.verifiedUser(t, "activity-owner", user.RoleUser)
	ctx := context.Background()

	if _, err := h.db.Client.Drink.Create().SetName("Fizz Pop").SetSlug("fizz-pop").Save(ctx); err != nil {
		t.Fatalf("drink: %v", err)
	}
	h.createCheckin(t, owner)

	mux := activityMux(h)
	req := h.request(http.MethodGet, "/admin/activity", admin, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Fizz Pop", "Cola", "/checkins/", "/drinks/fizz-pop", "@activity-owner", "Check-ins"} {
		if !strings.Contains(body, want) {
			t.Fatalf("page missing %q: %s", want, body)
		}
	}

	moderator := h.verifiedUser(t, "activity-mod", user.RoleModerator)
	req = h.request(http.MethodGet, "/admin/activity", moderator, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("moderator status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	member := h.verifiedUser(t, "activity-member", user.RoleUser)
	req = h.request(http.MethodGet, "/admin/activity", member, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Fizz Pop") {
		t.Fatalf("member response leaked activity rows: %s", rec.Body.String())
	}
}

func TestAdminActivityKindFilter(t *testing.T) {
	h := newActivityHarness(t)
	admin := h.verifiedUser(t, "activity-admin", user.RoleAdmin)
	owner := h.verifiedUser(t, "activity-owner", user.RoleUser)
	ctx := context.Background()

	if _, err := h.db.Client.Drink.Create().SetName("Fizz Pop").SetSlug("fizz-pop").Save(ctx); err != nil {
		t.Fatalf("drink: %v", err)
	}
	drink, err := h.db.Client.Drink.Create().SetName("Cherry Cola").SetSlug("cherry-cola").Save(ctx)
	if err != nil {
		t.Fatalf("checkin drink: %v", err)
	}
	if _, err := h.db.Client.Checkin.Create().
		SetUserID(owner.ID).SetDrinkID(drink.ID).
		SetVisibility(checkin.VisibilityPublic).Save(ctx); err != nil {
		t.Fatalf("checkin: %v", err)
	}

	mux := activityMux(h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodGet, "/admin/activity?kind=checkin", admin, nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, body)
	}
	if !strings.Contains(body, "Cherry Cola") || !strings.Contains(body, "/checkins/") {
		t.Fatalf("check-in filter dropped the check-in row: %s", body)
	}
	if strings.Contains(body, "Fizz Pop") {
		t.Fatalf("check-in filter leaked a soda row: %s", body)
	}
	if !strings.Contains(body, `href="/admin/activity?kind=checkin"`) {
		t.Fatalf("filter link missing: %s", body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodGet, "/admin/activity?kind=drink", admin, nil))
	body = rec.Body.String()
	if !strings.Contains(body, "Fizz Pop") {
		t.Fatalf("drink filter dropped the soda row: %s", body)
	}
	if strings.Contains(body, "/checkins/") {
		t.Fatalf("drink filter leaked a check-in row: %s", body)
	}

	// An unknown kind is passed to the service, which ignores it and returns
	// an empty page rather than every source.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodGet, "/admin/activity?kind=rubbish", admin, nil))
	body = rec.Body.String()
	if !strings.Contains(body, "Nothing has happened") || strings.Contains(body, "Fizz Pop") {
		t.Fatalf("unknown kind should render an empty feed: %s", body)
	}
}

func TestAdminActivityCursorPaging(t *testing.T) {
	h := newActivityHarness(t)
	admin := h.verifiedUser(t, "activity-admin", user.RoleAdmin)
	owner := h.verifiedUser(t, "activity-owner", user.RoleUser)
	ctx := context.Background()

	newest, err := h.db.Client.Drink.Create().SetName("Fizz Pop").SetSlug("fizz-pop").
		SetCreatedAt("2026-01-02T00:00:00Z").Save(ctx)
	if err != nil {
		t.Fatalf("newest drink: %v", err)
	}
	drink, err := h.db.Client.Drink.Create().SetName("Cherry Cola").SetSlug("cherry-cola").
		SetCreatedAt("2026-01-01T00:00:01Z").Save(ctx)
	if err != nil {
		t.Fatalf("checkin drink: %v", err)
	}
	if _, err := h.db.Client.Checkin.Create().
		SetUserID(owner.ID).SetDrinkID(drink.ID).
		SetVisibility(checkin.VisibilityPublic).
		SetCreatedAt("2026-01-01T00:00:00Z").Save(ctx); err != nil {
		t.Fatalf("checkin: %v", err)
	}

	mux := activityMux(h)

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, h.request(http.MethodGet, "/admin/activity", admin, nil))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", first.Code, first.Body.String())
	}
	if strings.Contains(first.Body.String(), ">Older<") {
		t.Fatalf("Older link should be absent on a short page: %s", first.Body.String())
	}

	cursor := "2026-01-02T00:00:00Z|drink:" + strconv.Itoa(newest.ID)
	target := "/admin/activity?before=" + url.QueryEscape(cursor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodGet, target, admin, nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("cursor status = %d, want 200: %s", rec.Code, body)
	}
	if strings.Contains(body, "Fizz Pop") {
		t.Fatalf("cursor page repeated the newer row: %s", body)
	}
	if !strings.Contains(body, "Cherry Cola") {
		t.Fatalf("cursor page dropped the older row: %s", body)
	}
	if !strings.Contains(body, ">Latest<") {
		t.Fatalf("cursor page should offer a Latest link: %s", body)
	}
}

func TestActivityWhen(t *testing.T) {
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		iso  string
		want string
	}{
		{"2026-01-10T11:59:40Z", "just now"},
		{"2026-01-10T11:58:00Z", "2 min ago"},
		{"2026-01-10T09:00:00Z", "3 h ago"},
		{"2026-01-09T20:00:00Z", "yesterday"},
		{"2026-01-02T08:30:00Z", "2 Jan 2026"},
		{"not a timestamp", "not a timestamp"},
	}
	for _, tc := range cases {
		if got := activityWhen(tc.iso, now); got != tc.want {
			t.Fatalf("activityWhen(%q) = %q, want %q", tc.iso, got, tc.want)
		}
	}
}
