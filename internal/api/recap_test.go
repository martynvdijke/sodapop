package api

import (
	"context"
	"net/http"
	"testing"

	"sodapop/ent"
	"sodapop/internal/checkins"
)

// seedRecapCheckin creates one check-in for userID and moves it to the given
// year with a raw update, because checkins.Create always stamps "now".
func seedRecapCheckin(t *testing.T, h *harness, userID, slug string, year int) {
	t.Helper()
	d := newDrink(t, h, "Recap "+slug, slug, nil)
	if _, _, err := h.server.Checkins.Create(context.Background(), mustUser(t, h, userID), checkins.CreateInput{
		DrinkID:    d.ID,
		Visibility: "private",
	}); err != nil {
		t.Fatalf("checkin: %v", err)
	}
	stamp := map[int]string{2024: "2024-06-15T12:00:00Z", 2025: "2025-06-15T12:00:00Z"}[year]
	if stamp == "" {
		t.Fatalf("no stamp for year %d", year)
	}
	if _, err := h.server.DB.SQL.ExecContext(context.Background(),
		`UPDATE checkins SET created_at = ? WHERE user_id = ? AND drink_id = ?`,
		stamp, userID, d.ID); err != nil {
		t.Fatalf("stamp checkin: %v", err)
	}
}

func TestMeRecapIsOwnerScopedAndYearScoped(t *testing.T) {
	h := newHarness(t)
	seedRecapCheckin(t, h, h.alice, "recap-2025", 2025)
	seedRecapCheckin(t, h, h.alice, "recap-2024", 2024)

	rec := h.do(t, http.MethodGet, "/api/v1/me/recap/2025", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("recap: got %d body %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	if got := data["year"]; got != float64(2025) {
		t.Fatalf("year: got %v want 2025", got)
	}
	if got := data["has_data"]; got != true {
		t.Fatalf("has_data: got %v want true", got)
	}
	totals, ok := data["totals"].(map[string]any)
	if !ok {
		t.Fatalf("totals is not an object: %s", rec.Body.String())
	}
	if got := totals["checkins"]; got != float64(1) {
		t.Fatalf("checkins: got %v want 1 (only the 2025 row)", got)
	}
	// The fingerprint is embedded in the recap payload, so the mobile client
	// gets the palate picture without a second request.
	if _, ok := data["fingerprint"]; !ok {
		t.Fatalf("recap is missing the fingerprint: %s", rec.Body.String())
	}
	// The route never lists another member's rows: bob logged nothing.
	bob := h.doWith(t, h.tokenFor(t, h.bob), http.MethodGet, "/api/v1/me/recap/2025", "")
	if bob.Code != http.StatusOK {
		t.Fatalf("bob recap: got %d body %s", bob.Code, bob.Body.String())
	}
	bobData := dataMap(t, bob)
	if got := bobData["has_data"]; got != false {
		t.Fatalf("bob has_data: got %v want false (must not see alice's rows)", got)
	}
}

func TestMeRecapEmptyYearIsNotAnError(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodGet, "/api/v1/me/recap/2019", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty year: got %d body %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	if got := data["has_data"]; got != false {
		t.Fatalf("has_data: got %v want false", got)
	}
	if got := data["sparse"]; got != false {
		t.Fatalf("sparse: got %v want false when there is nothing at all", got)
	}
}

func TestMeRecapRejectsBadYear(t *testing.T) {
	h := newHarness(t)
	for _, year := range []string{"abc", "1999", "2101", "", "2025.5"} {
		rec := h.do(t, http.MethodGet, "/api/v1/me/recap/"+year, "", true)
		if year == "" {
			// Go's mux treats the missing segment as no match at all.
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
				t.Errorf("empty year: got %d, want 400/404", rec.Code)
			}
			continue
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("year %q: got %d, want 400 (body %s)", year, rec.Code, rec.Body.String())
		}
	}
}

func TestMeRecapRequiresAuth(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodGet, "/api/v1/me/recap/2025", "", false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: got %d, want 401", rec.Code)
	}
}

// The payload carries the year switcher and the year's badges, so the Android
// recap does not need to probe years or call the badge endpoint.
func TestMeRecapCarriesYearsAndBadges(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	seedRecapCheckin(t, h, h.alice, "recap-years-2024", 2024)
	seedRecapCheckin(t, h, h.alice, "recap-years-2025", 2025)

	badgeRow, err := h.server.Auth.Client.Badge.Create().
		SetSlug("recap-first").SetName("First Recap").Save(ctx)
	if err != nil {
		t.Fatalf("badge: %v", err)
	}
	other, err := h.server.Auth.Client.Badge.Create().
		SetSlug("recap-old").SetName("Old Recap").Save(ctx)
	if err != nil {
		t.Fatalf("badge: %v", err)
	}
	for _, row := range []struct {
		badge *ent.Badge
		at    string
	}{
		{badgeRow, "2025-03-02T09:00:00Z"},
		{other, "2024-03-02T09:00:00Z"},
	} {
		if _, err := h.server.Auth.Client.UserBadge.Create().
			SetUserID(h.alice).SetBadgeID(row.badge.ID).SetLevel(1).
			SetAwardedAt(row.at).Save(ctx); err != nil {
			t.Fatalf("award: %v", err)
		}
	}

	rec := h.do(t, http.MethodGet, "/api/v1/me/recap/2025", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("recap: got %d body %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)

	years, ok := data["years_with_data"].([]any)
	if !ok || len(years) != 2 {
		t.Fatalf("years_with_data = %v", data["years_with_data"])
	}
	if years[0].(float64) != 2024 || years[1].(float64) != 2025 {
		t.Fatalf("years_with_data = %v, want ascending 2024, 2025", years)
	}

	rows, ok := data["badges"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("badges = %v", data["badges"])
	}
	badge := rows[0].(map[string]any)
	if badge["slug"] != "recap-first" || badge["name"] != "First Recap" ||
		badge["awarded_at"] != "2025-03-02T09:00:00Z" {
		t.Fatalf("badge = %+v", badge)
	}

	fingerprint, ok := data["fingerprint"].(map[string]any)
	if !ok {
		t.Fatalf("fingerprint missing: %s", rec.Body.String())
	}
	if fingerprint["whole_history"] != true {
		t.Fatalf("whole_history = %v, want true", fingerprint["whole_history"])
	}
}

// A member with no check-ins still gets an empty (not null) year list.
func TestMeRecapEmptyYearLists(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodGet, "/api/v1/me/recap/2025", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("recap: got %d body %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	if years, ok := data["years_with_data"].([]any); !ok || len(years) != 0 {
		t.Fatalf("years_with_data = %v, want []", data["years_with_data"])
	}
	if badges, ok := data["badges"].([]any); !ok || len(badges) != 0 {
		t.Fatalf("badges = %v, want []", data["badges"])
	}
}
