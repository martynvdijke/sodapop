package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"sodapop/ent/checkin"
)

func TestNearbyPageStartsWithoutLocation(t *testing.T) {
	h := newV12Harness(t)
	h.srv.Render = rendererForPage(t, "nearby", `{{.Title}}|{{.LocationSet}}|{{.Radius}}|{{if .Items}}items{{else}}empty{{end}}`)

	req := httptest.NewRequest(http.MethodGet, "/nearby", nil)
	rec := httptest.NewRecorder()
	h.srv.nearbyPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if got, want := rec.Body.String(), "Nearby|false|2|empty"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("cache-control = %q", got)
	}
}

func TestNearbyPageReturnsOwnPours(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	viewer := h.user(t, "nearby-viewer")

	drink, err := h.db.Client.Drink.Create().SetName("Ginger Beer").SetSlug("ginger-beer").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	venue, err := h.db.Client.Venue.Create().
		SetName("Corner Fridge").SetSlug("corner-fridge").
		SetLat(51.500).SetLng(-0.120).Save(ctx)
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	if _, err := h.db.Client.Checkin.Create().
		SetUserID(viewer.ID).SetDrinkID(drink.ID).SetVenueID(venue.ID).
		SetVisibility(checkin.VisibilityPublic).Save(ctx); err != nil {
		t.Fatalf("checkin: %v", err)
	}

	h.srv.Render = rendererForPage(t, "nearby",
		`{{.LocationSet}}|{{.Empty}}|{{.HasMore}}|{{range .Items}}{{.DrinkName}}:{{.DistanceM}}:{{.VenueName}}:{{.Handle}}{{end}}`)

	form := h.form()
	form.Set("lat", "51.500")
	form.Set("lng", "-0.120")
	form.Set("radius", "2")
	rec := httptest.NewRecorder()
	h.srv.nearbyPage(rec, h.request(http.MethodPost, "/nearby", viewer, form))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	want := "true|false|false|Ginger Beer:0:Corner Fridge:nearby-viewer"
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestNearbyPageRateLimitedPerUser(t *testing.T) {
	h := newV12Harness(t)
	viewer := h.user(t, "nearby-rate")
	h.srv.Render = rendererForPage(t, "nearby", `{{.Error}}|{{.Radius}}|{{.HasMore}}|{{if .Items}}items{{else}}empty{{end}}`)

	form := h.form()
	form.Set("lat", "51.500")
	form.Set("lng", "-0.120")
	form.Set("radius", "2")
	for i := 0; i < 30; i++ {
		rec := httptest.NewRecorder()
		h.srv.nearbyPage(rec, h.request(http.MethodPost, "/nearby", viewer, form))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200 (%s)", i+1, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	h.srv.nearbyPage(rec, h.request(http.MethodPost, "/nearby", viewer, form))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 31: got %d, want 429 (%s)", rec.Code, rec.Body.String())
	}
	if got, want := rec.Body.String(), "Too many nearby searches. Wait a minute and try again.|2|false|empty"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
}

func TestNearbyPageRejectsBadRadius(t *testing.T) {
	h := newV12Harness(t)
	viewer := h.user(t, "nearby-radius")
	h.srv.Render = rendererForPage(t, "nearby", `{{.Error}}|{{.Radius}}|{{.LocationSet}}`)

	form := h.form()
	form.Set("lat", "51.500")
	form.Set("lng", "-0.120")
	form.Set("radius", "99")
	rec := httptest.NewRecorder()
	h.srv.nearbyPage(rec, h.request(http.MethodPost, "/nearby", viewer, form))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	want := "Choose a radius between 0.1 and 25 km.|2|true"
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
