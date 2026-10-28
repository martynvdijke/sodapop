package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"sodapop/ent"
	"sodapop/ent/venuedrink"
	"sodapop/internal/venues"
)

func (h *v12Harness) adminUser(t *testing.T, handle string) *ent.User {
	t.Helper()
	u := h.user(t, handle)
	if _, err := h.db.Client.User.UpdateOneID(u.ID).SetRole("admin").Save(context.Background()); err != nil {
		t.Fatalf("make %s admin: %v", handle, err)
	}
	u.Role = "admin"
	return u
}

func (h *v12Harness) offerVenueDrink(t *testing.T, venueID int, slug string) {
	t.Helper()
	ctx := context.Background()
	d, err := h.db.Client.Drink.Create().SetName(slug).SetSlug(slug).Save(ctx)
	if err != nil {
		t.Fatalf("create drink: %v", err)
	}
	if _, err := h.db.Client.VenueDrink.Create().
		SetVenueID(venueID).SetDrinkID(d.ID).Save(ctx); err != nil {
		t.Fatalf("create venue drink: %v", err)
	}
}

func (h *v12Harness) onOffer(t *testing.T, venueID int, slug string) bool {
	t.Helper()
	row, err := h.db.Client.VenueDrink.Query().
		Where(venuedrink.VenueID(venueID)).
		Only(context.Background())
	if err != nil {
		t.Fatalf("query venue drink: %v", err)
	}
	if row.DrinkID == 0 {
		t.Fatalf("venue drink for %s missing", slug)
	}
	return row.OnOffer
}

func TestAdminVenueOfferToggle(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	admin := h.adminUser(t, "offer-admin")
	v, _, err := h.srv.Venues.Create(ctx, admin.ID, venues.CreateInput{Name: "Offer Hall", City: "Bristol"})
	if err != nil {
		t.Fatalf("create venue: %v", err)
	}
	h.offerVenueDrink(t, v.ID, "cola-classic")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/venues/{id}/offer", h.srv.requireAdminStrict(h.srv.adminVenueOfferSet))

	tests := []struct {
		name      string
		on        string
		wantState bool
	}{
		{name: "mark on offer", on: "1", wantState: true},
		{name: "remove offer", on: "0", wantState: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			form := h.form()
			form.Set("drink_slug", "cola-classic")
			form.Set("on", tc.on)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, h.request(http.MethodPost, "/admin/venues/"+strconv.Itoa(v.ID)+"/offer", admin, form))

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302 (%s)", rec.Code, rec.Body.String())
			}
			if loc, want := rec.Header().Get("Location"), "/admin/venues/"+strconv.Itoa(v.ID)+"?updated="+tc.on; loc != want {
				t.Fatalf("redirect = %q, want %q", loc, want)
			}
			if got := h.onOffer(t, v.ID, "cola-classic"); got != tc.wantState {
				t.Fatalf("on_offer = %v, want %v", got, tc.wantState)
			}
		})
	}
}

func TestAdminVenueOfferRequiresAdmin(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	user := h.user(t, "plain-user")
	v, _, err := h.srv.Venues.Create(ctx, user.ID, venues.CreateInput{Name: "Offer Hall", City: "Bristol"})
	if err != nil {
		t.Fatalf("create venue: %v", err)
	}
	h.offerVenueDrink(t, v.ID, "cola-classic")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/venues/{id}/offer", h.srv.requireAdminStrict(h.srv.adminVenueOfferSet))

	form := h.form()
	form.Set("drink_slug", "cola-classic")
	form.Set("on", "1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodPost, "/admin/venues/"+strconv.Itoa(v.ID)+"/offer", user, form))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if h.onOffer(t, v.ID, "cola-classic") {
		t.Fatal("on_offer changed despite non-admin request")
	}
}

func TestAdminVenueOfferMissingDrink(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	admin := h.adminUser(t, "offer-admin")
	v, _, err := h.srv.Venues.Create(ctx, admin.ID, venues.CreateInput{Name: "Offer Hall", City: "Bristol"})
	if err != nil {
		t.Fatalf("create venue: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/venues/{id}/offer", h.srv.requireAdminStrict(h.srv.adminVenueOfferSet))

	form := h.form()
	form.Set("drink_slug", "not-a-drink")
	form.Set("on", "1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodPost, "/admin/venues/"+strconv.Itoa(v.ID)+"/offer", admin, form))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/venues?error=missing" {
		t.Fatalf("missing drink = %d %q, want 302 error=missing", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminVenueOffersPage(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	admin := h.adminUser(t, "offer-admin")
	v, _, err := h.srv.Venues.Create(ctx, admin.ID, venues.CreateInput{Name: "Offer Hall", City: "Bristol"})
	if err != nil {
		t.Fatalf("create venue: %v", err)
	}
	h.offerVenueDrink(t, v.ID, "cola-classic")

	h.srv.Render = rendererForPage(t, "admin_venues",
		`{{with .Detail}}{{.Name}}|{{range .Drinks}}{{.DrinkSlug}}={{.OnOffer}};{{end}}{{end}}`)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/venues/{id}", h.srv.requireAdminStrict(h.srv.adminVenueOffers))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodGet, "/admin/venues/"+strconv.Itoa(v.ID), admin, h.form()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !bodyContainsAll(body, "Offer Hall", "cola-classic=false") {
		t.Fatalf("page body = %q", body)
	}
}

func TestAdminVenueOffersUnknownVenue(t *testing.T) {
	h := newV12Harness(t)
	admin := h.adminUser(t, "offer-admin")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/venues/{id}", h.srv.requireAdminStrict(h.srv.adminVenueOffers))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodGet, "/admin/venues/99999", admin, h.form()))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAdminVenuesPageRedirectsLegacyVenueQuery(t *testing.T) {
	h := newV12Harness(t)
	admin := h.adminUser(t, "offer-admin")

	rec := httptest.NewRecorder()
	req := h.request(http.MethodGet, "/admin/venues?venue=42", admin, h.form())
	h.srv.adminVenuesPage(rec, req)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/venues/42" {
		t.Fatalf("redirect = %d %q, want 302 /admin/venues/42", rec.Code, rec.Header().Get("Location"))
	}
}
