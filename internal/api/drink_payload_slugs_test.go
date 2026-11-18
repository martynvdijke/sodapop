package api

import (
	"context"
	"net/http"
	"testing"
)

// TestDrinkPayloadCarriesSlugFields pins the payload contract mobile uses to
// prefill a from-drink drop alert: the style, brand and first flavour slugs
// travel beside the display names, in snake_case.
func TestDrinkPayloadCarriesSlugFields(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	brand, err := h.server.DB.Client.Brand.Create().
		SetName("Acme Soda").SetSlug("acme-soda").Save(ctx)
	if err != nil {
		t.Fatalf("brand: %v", err)
	}
	style, err := h.server.DB.Client.Style.Create().
		SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	flavour, err := h.server.DB.Client.Flavour.Create().
		SetName("Citrus").SetSlug("citrus").Save(ctx)
	if err != nil {
		t.Fatalf("flavour: %v", err)
	}

	d, err := h.server.DB.Client.Drink.Create().
		SetName("Acme Cola").SetSlug("acme-cola").
		SetBrandID(brand.ID).SetStyleID(style.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	if _, err := h.server.DB.Client.DrinkFlavour.Create().
		SetDrinkID(d.ID).SetFlavourID(flavour.ID).SetIntensity(4).Save(ctx); err != nil {
		t.Fatalf("drink flavour: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/drinks/acme-cola", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)

	if got := data["brand_slug"]; got != "acme-soda" {
		t.Errorf("brand_slug = %v, want acme-soda", got)
	}
	if got := data["style_slug"]; got != "cola" {
		t.Errorf("style_slug = %v, want cola", got)
	}
	slugs, ok := data["flavour_slugs"].([]any)
	if !ok {
		t.Fatalf("flavour_slugs is %T, want array: %v", data["flavour_slugs"], data["flavour_slugs"])
	}
	if len(slugs) != 1 || slugs[0] != "citrus" {
		t.Errorf("flavour_slugs = %v, want [citrus]", slugs)
	}
	// The slug list runs parallel to the display names.
	flavours, ok := data["flavours"].([]any)
	if !ok || len(flavours) != len(slugs) {
		t.Fatalf("flavours = %v, want one entry matching flavour_slugs", data["flavours"])
	}
}

// TestDrinkPayloadFlavourSlugsEmptyMatchesFlavours keeps the new list's
// nil/empty handling identical to the existing flavours list.
func TestDrinkPayloadFlavourSlugsEmptyMatchesFlavours(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.server.DB.Client.Drink.Create().
		SetName("Plain Cola").SetSlug("plain-cola").Save(ctx); err != nil {
		t.Fatalf("drink: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/drinks/plain-cola", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)

	for _, key := range []string{"flavours", "flavour_slugs"} {
		if _, present := data[key]; !present {
			t.Errorf("%s key missing from payload", key)
		}
	}
	if data["flavour_slugs"] != data["flavours"] {
		t.Errorf("flavour_slugs = %v, flavours = %v; nil/empty handling must match",
			data["flavour_slugs"], data["flavours"])
	}
}
