package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sodapop/ent/checkin"
	"sodapop/ent/drink"
)

func mkPublicDrink(t *testing.T, h *harness, name, slug string, status drink.Status, published bool) *drinkRow {
	t.Helper()
	d, err := h.server.DB.Client.Drink.Create().
		SetName(name).
		SetSlug(slug).
		SetStatus(status).
		SetPublished(published).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create drink %s: %v", slug, err)
	}
	return &drinkRow{ID: d.ID, Slug: d.Slug}
}

type drinkRow struct {
	ID   int
	Slug string
}

func ratePublicDrink(t *testing.T, h *harness, handle string, drinkID, rating int) {
	t.Helper()
	ctx := context.Background()
	u, err := h.server.DB.Client.User.Create().
		SetHandle(handle).SetEmail(handle + "@example.com").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.server.DB.Client.Checkin.Create().
		SetUserID(u.ID).SetDrinkID(drinkID).SetRating(rating).
		SetVisibility(checkin.VisibilityPublic).Save(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPublicDrinkReturnsOnlyPublicFacts(t *testing.T) {
	h := newHarness(t)
	d := mkPublicDrink(t, h, "Cherry Cola", "cherry-cola", drink.StatusActive, true)
	ratePublicDrink(t, h, "rater-one", d.ID, 20)
	ratePublicDrink(t, h, "rater-two", d.ID, 16)

	rec := h.do(t, http.MethodGet, "/api/v1/public/drinks/"+d.Slug, "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unexpected CORS header %q", got)
	}

	var payload struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	allowed := map[string]bool{
		"slug": true, "name": true, "description": true, "brand": true, "style": true,
		"container": true, "volume_ml": true, "caffeine_mg_per_100ml": true,
		"sugar_g_per_100ml": true, "calories_kcal_per_100ml": true, "sweetener": true,
		"carbonation": true, "acid": true, "status": true, "image_url": true,
		"flavours": true, "rating": true,
	}
	for key := range payload.Data {
		if !allowed[key] {
			t.Errorf("public payload leaks field %q", key)
		}
	}
	for key := range allowed {
		if _, ok := payload.Data[key]; !ok {
			t.Errorf("public payload is missing field %q", key)
		}
	}
	if payload.Data["slug"] != d.Slug {
		t.Errorf("slug = %v", payload.Data["slug"])
	}
	if payload.Data["rating"] != nil {
		t.Errorf("rating shown with only 2 raters: %v", payload.Data["rating"])
	}
	// Explicit regressions for the internal fields listed in the contract.
	for _, forbidden := range []string{"id", "source", "created_by", "barcodes", "aliases", "barcode_count", "media"} {
		if _, ok := payload.Data[forbidden]; ok {
			t.Errorf("public payload contains internal field %q", forbidden)
		}
	}

	ratePublicDrink(t, h, "rater-three", d.ID, 12)
	rec = h.do(t, http.MethodGet, "/api/v1/public/drinks/"+d.Slug, "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status after third rater = %d: %s", rec.Code, rec.Body.String())
	}
	payload.Data = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	rating, ok := payload.Data["rating"].(map[string]any)
	if !ok {
		t.Fatalf("rating = %v, want object", payload.Data["rating"])
	}
	if rating["average"] != 4.0 || rating["rater_count"] != float64(3) {
		t.Errorf("rating = %v, want average 4 rater_count 3", rating)
	}
}

func TestPublicDrinkHidesNonPublicRows(t *testing.T) {
	h := newHarness(t)
	unpublished := mkPublicDrink(t, h, "Unpublished", "unpublished", drink.StatusActive, false)
	provisional := mkPublicDrink(t, h, "Provisional", "provisional", drink.StatusProvisional, true)
	merged := mkPublicDrink(t, h, "Merged", "merged", drink.StatusActive, true)
	target := mkPublicDrink(t, h, "Target", "target", drink.StatusActive, true)
	if _, err := h.server.DB.Client.Drink.UpdateOneID(merged.ID).
		SetMergedIntoID(target.ID).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	discontinued := mkPublicDrink(t, h, "Discontinued", "discontinued", drink.StatusDiscontinued, true)

	for _, tc := range []struct{ name, slug string }{
		{"unpublished", unpublished.Slug},
		{"provisional", provisional.Slug},
		{"merged", merged.Slug},
		{"discontinued", discontinued.Slug},
		{"unknown", "nope"},
		{"malformed", "bad_slug"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, http.MethodGet, "/api/v1/public/drinks/"+tc.slug, "", false)
			if rec.Code != http.StatusNotFound || !containsCode(rec.Body.Bytes(), "not_found") {
				t.Fatalf("status = %d body %s, want 404 not_found", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPublicDrinksCollectionDoesNotExist(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodGet, "/api/v1/public/drinks", "", false)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("collection status = %d, want 404/405", rec.Code)
	}
}

func TestPublicDrinkRateLimit(t *testing.T) {
	h := newHarness(t)
	d := mkPublicDrink(t, h, "Rate Limited", "rate-limited", drink.StatusActive, true)
	for i := 0; i < 30; i++ {
		rec := h.do(t, http.MethodGet, "/api/v1/public/drinks/"+d.Slug, "", false)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := h.do(t, http.MethodGet, "/api/v1/public/drinks/"+d.Slug, "", false)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 31 = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
	if !containsCode(rec.Body.Bytes(), "rate_limited") {
		t.Errorf("body = %s, want rate_limited", rec.Body.String())
	}
}

// publicDrinkFrom issues a public drink request with an explicit peer and
// X-Forwarded-For so proxy-aware rate-limit derivation can be exercised.
func publicDrinkFrom(h *harness, slug, remote, xff string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/drinks/"+slug, nil)
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func TestPublicDrinkRateLimitUsesClientBehindTrustedProxy(t *testing.T) {
	h := newHarness(t)
	h.server.Cfg.TrustedProxies = []string{"10.0.0.0/8"}
	d := mkPublicDrink(t, h, "Proxy Rate", "proxy-rate", drink.StatusActive, true)

	// The proxy is trusted and appends itself; the client is the untrusted hop.
	xff := "203.0.113.7, 10.0.0.5"
	for i := 0; i < 30; i++ {
		rec := publicDrinkFrom(h, d.Slug, "10.0.0.5:5555", xff)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	// Same forwarded client shares one bucket.
	rec := publicDrinkFrom(h, d.Slug, "10.0.0.5:5555", xff)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 31 = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	// A different client through the same proxy keeps its own bucket.
	other := publicDrinkFrom(h, d.Slug, "10.0.0.5:5555", "203.0.113.8, 10.0.0.5")
	if other.Code != http.StatusOK {
		t.Fatalf("other client = %d, want 200: %s", other.Code, other.Body.String())
	}
}

func TestPublicDrinkRateLimitIgnoresSpoofedHeaderFromUntrustedPeer(t *testing.T) {
	h := newHarness(t)
	d := mkPublicDrink(t, h, "Spoof Rate", "spoof-rate", drink.StatusActive, true)

	for i := 0; i < 30; i++ {
		rec := publicDrinkFrom(h, d.Slug, "192.0.2.10:4567", "203.0.113."+string(rune('0'+i%10)))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	// Rotating a spoofed header must not reset the peer's bucket.
	rec := publicDrinkFrom(h, d.Slug, "192.0.2.10:4567", "203.0.113.99")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 31 = %d, want 429: %s", rec.Code, rec.Body.String())
	}
}
