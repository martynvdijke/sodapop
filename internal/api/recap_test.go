package api

import (
	"context"
	"net/http"
	"testing"

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
