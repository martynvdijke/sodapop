package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// nearbyShelfTemplate renders the wishlist-shelf view data compactly.
const nearbyShelfTemplate = `{{range .WishlistShelf}}{{.VenueName}}|{{.DistanceLabel}}|{{range .Matches}}{{.DrinkName}};{{.Brand}};{{.PriceLabel}};{{.DistanceLabel}};{{.AgeLabel}};{{.Reporters}}/{{end}}#{{end}}`

// wishlistFor creates the viewer's system wishlist and adds the given drinks.
func wishlistFor(t *testing.T, h *v12Harness, viewerID string, drinkIDs ...int) {
	t.Helper()
	ctx := context.Background()
	l, err := h.db.Client.List.Create().
		SetUserID(viewerID).SetName("Wishlist").SetSlug("wishlist").SetIsSystem(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("wishlist: %v", err)
	}
	for _, id := range drinkIDs {
		if _, err := h.db.Client.ListItem.Create().SetListID(l.ID).SetDrinkID(id).Save(ctx); err != nil {
			t.Fatalf("wishlist item: %v", err)
		}
	}
}

// stockDrink records a fresh shelf report for a drink at a venue.
func stockDrink(t *testing.T, h *v12Harness, reporterID string, venueID, drinkID int, price *int) {
	t.Helper()
	_, err := h.db.Client.VenueStockReport.Create().
		SetVenueID(venueID).SetDrinkID(drinkID).SetUserID(reporterID).
		SetSeenAt(time.Now().UTC().Format(time.RFC3339)).
		SetNillablePriceCents(price).
		Save(context.Background())
	if err != nil {
		t.Fatalf("stock report: %v", err)
	}
}

func TestNearbyPageShowsWishlistShelf(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	viewer := h.user(t, "nearby-shelf")
	reporter := h.user(t, "nearby-shelf-reporter")

	wanted, err := h.db.Client.Drink.Create().SetName("Yuzu Soda").SetSlug("yuzu-soda").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	other, err := h.db.Client.Drink.Create().SetName("Root Beer").SetSlug("root-beer").Save(ctx)
	if err != nil {
		t.Fatalf("other drink: %v", err)
	}
	brand, err := h.db.Client.Brand.Create().SetName("Kiosk").SetSlug("kiosk").Save(ctx)
	if err != nil {
		t.Fatalf("brand: %v", err)
	}
	if _, err := h.db.Client.Drink.UpdateOneID(wanted.ID).SetBrandID(brand.ID).Save(ctx); err != nil {
		t.Fatalf("brand link: %v", err)
	}
	near, err := h.db.Client.Venue.Create().
		SetName("Corner Fridge").SetSlug("corner-fridge").
		SetLat(51.500).SetLng(-0.120).Save(ctx)
	if err != nil {
		t.Fatalf("near venue: %v", err)
	}
	far, err := h.db.Client.Venue.Create().
		SetName("Station Kiosk").SetSlug("station-kiosk").
		SetLat(51.509).SetLng(-0.120).Save(ctx)
	if err != nil {
		t.Fatalf("far venue: %v", err)
	}
	// A venue that only stocks a drink outside the wishlist must drop out.
	unwanted, err := h.db.Client.Venue.Create().
		SetName("Back Shelf").SetSlug("back-shelf").
		SetLat(51.5005).SetLng(-0.120).Save(ctx)
	if err != nil {
		t.Fatalf("unwanted venue: %v", err)
	}

	wishlistFor(t, h, viewer.ID, wanted.ID)
	price := 150
	stockDrink(t, h, reporter.ID, far.ID, wanted.ID, nil)
	stockDrink(t, h, reporter.ID, near.ID, wanted.ID, &price)
	stockDrink(t, h, reporter.ID, unwanted.ID, other.ID, nil)

	h.srv.Render = rendererForPage(t, "nearby", nearbyShelfTemplate)

	form := h.form()
	form.Set("lat", "51.500")
	form.Set("lng", "-0.120")
	form.Set("radius", "2")
	rec := httptest.NewRecorder()
	h.srv.nearbyPage(rec, h.request(http.MethodPost, "/nearby", viewer, form))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	want := "Corner Fridge|0 m|Yuzu Soda;Kiosk;1.50;0 m;today;1/#" +
		"Station Kiosk|1 km|Yuzu Soda;Kiosk;;1 km;today;1/#"
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestNearbyPageOmitsWishlistShelfWithoutWishlist(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	viewer := h.user(t, "nearby-no-wishlist")

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
	stockDrink(t, h, viewer.ID, venue.ID, drink.ID, nil)

	h.srv.Render = rendererForPage(t, "nearby",
		nearbyShelfTemplate+`|{{.Empty}}|{{range .Items}}{{.DrinkName}}{{end}}`)

	form := h.form()
	form.Set("lat", "51.500")
	form.Set("lng", "-0.120")
	form.Set("radius", "2")
	rec := httptest.NewRecorder()
	h.srv.nearbyPage(rec, h.request(http.MethodPost, "/nearby", viewer, form))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if want, got := "|true|", rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestNearbyPageKeepsResultsWhenWishlistShelfIsEmpty(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	viewer := h.user(t, "nearby-wishlist-empty")
	reporter := h.user(t, "nearby-wishlist-reporter")

	wanted, err := h.db.Client.Drink.Create().SetName("Yuzu Soda").SetSlug("yuzu-soda").Save(ctx)
	if err != nil {
		t.Fatalf("wishlist drink: %v", err)
	}
	poured, err := h.db.Client.Drink.Create().SetName("Ginger Beer").SetSlug("ginger-beer").Save(ctx)
	if err != nil {
		t.Fatalf("poured drink: %v", err)
	}
	venue, err := h.db.Client.Venue.Create().
		SetName("Corner Fridge").SetSlug("corner-fridge").
		SetLat(51.500).SetLng(-0.120).Save(ctx)
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	wishlistFor(t, h, viewer.ID, wanted.ID)
	stockDrink(t, h, reporter.ID, venue.ID, poured.ID, nil)
	if _, err := h.db.Client.Checkin.Create().
		SetUserID(viewer.ID).SetDrinkID(poured.ID).SetVenueID(venue.ID).
		SetVisibility(checkin.VisibilityPublic).Save(ctx); err != nil {
		t.Fatalf("checkin: %v", err)
	}

	h.srv.Render = rendererForPage(t, "nearby",
		nearbyShelfTemplate+`|{{range .Items}}{{.DrinkName}}{{end}}`)

	form := h.form()
	form.Set("lat", "51.500")
	form.Set("lng", "-0.120")
	form.Set("radius", "2")
	rec := httptest.NewRecorder()
	h.srv.nearbyPage(rec, h.request(http.MethodPost, "/nearby", viewer, form))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if want, got := "|Ginger Beer", rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
