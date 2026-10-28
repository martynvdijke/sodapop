package api

import (
	"context"
	"net/http"
	"testing"

	"sodapop/internal/checkins"
	"sodapop/internal/db"
)

func TestPassportStampsAPI(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)

	style, err := h.server.DB.Client.Style.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	brand, err := h.server.DB.Client.Brand.Create().SetName("Alice Soda").SetSlug("alice-soda").SetCountry("US").Save(ctx)
	if err != nil {
		t.Fatalf("brand: %v", err)
	}
	bobBrand, err := h.server.DB.Client.Brand.Create().SetName("Bob Soda").SetSlug("bob-soda").SetCountry("JP").Save(ctx)
	if err != nil {
		t.Fatalf("bob brand: %v", err)
	}
	bar, err := h.server.DB.Client.Venue.Create().SetName("Bar").SetSlug("bar").SetKind("bar").Save(ctx)
	if err != nil {
		t.Fatalf("venue: %v", err)
	}

	drink := newDrink(t, h, "Alice Cola", "alice-cola", &style.ID)
	drink2 := newDrink(t, h, "Alice Cola Zero", "alice-cola-zero", &style.ID)
	bobDrink := newDrink(t, h, "Bob Cola", "bob-cola", nil)
	if _, err := drink.Update().SetBrandID(brand.ID).Save(ctx); err != nil {
		t.Fatalf("brand link: %v", err)
	}
	if _, err := drink2.Update().SetBrandID(brand.ID).Save(ctx); err != nil {
		t.Fatalf("brand link 2: %v", err)
	}
	if _, err := bobDrink.Update().SetBrandID(bobBrand.ID).Save(ctx); err != nil {
		t.Fatalf("bob brand link: %v", err)
	}

	rating := 18
	if _, _, err := h.server.Checkins.Create(ctx, alice, checkins.CreateInput{
		DrinkID: drink.ID, VenueID: &bar.ID, Rating: &rating, Visibility: "private",
	}); err != nil {
		t.Fatalf("alice checkin: %v", err)
	}
	if _, _, err := h.server.Checkins.Create(ctx, alice, checkins.CreateInput{
		DrinkID: drink2.ID, Visibility: "private",
	}); err != nil {
		t.Fatalf("alice checkin 2: %v", err)
	}
	// A check-in of a drink that was later merged must not appear.
	merged := newDrink(t, h, "Old Cola", "old-cola", &style.ID)
	if _, _, err := h.server.Checkins.Create(ctx, alice, checkins.CreateInput{
		DrinkID: merged.ID, Visibility: "private",
	}); err != nil {
		t.Fatalf("merged checkin: %v", err)
	}
	if _, err := merged.Update().SetStatus("merged").Save(ctx); err != nil {
		t.Fatalf("merge drink: %v", err)
	}
	if _, _, err := h.server.Checkins.Create(ctx, bob, checkins.CreateInput{
		DrinkID: bobDrink.ID, Visibility: "private",
	}); err != nil {
		t.Fatalf("bob checkin: %v", err)
	}
	// A soft-deleted check-in must not be counted.
	deleted, _, err := h.server.Checkins.Create(ctx, alice, checkins.CreateInput{
		DrinkID: drink.ID, Visibility: "private",
	})
	if err != nil {
		t.Fatalf("deleted checkin: %v", err)
	}
	if _, err := h.server.DB.Client.Checkin.UpdateOneID(deleted.ID).SetDeletedAt(db.Now()).Save(ctx); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/passport", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("passport = %d: %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	if _, ok := data["stats"]; !ok {
		t.Fatalf("stats key lost: %+v", data)
	}
	if _, ok := data["badges"]; !ok {
		t.Fatalf("badges key lost: %+v", data)
	}

	totals, _ := data["totals"].(map[string]any)
	if totals["checkins"].(float64) != 2 || totals["styles"].(float64) != 1 ||
		totals["venue_kinds"].(float64) != 1 || totals["countries"].(float64) != 1 {
		t.Fatalf("totals = %+v", totals)
	}

	styles, _ := data["styles"].([]any)
	if len(styles) != 1 {
		t.Fatalf("styles = %+v", styles)
	}
	styleRow := styles[0].(map[string]any)
	if styleRow["name"] != "Cola" || styleRow["count"].(float64) != 2 || styleRow["last_at"] == "" {
		t.Fatalf("style row = %+v", styleRow)
	}

	kinds, _ := data["venue_kinds"].([]any)
	if len(kinds) != 1 || kinds[0].(map[string]any)["kind"] != "bar" {
		t.Fatalf("venue kinds = %+v", kinds)
	}
	countries, _ := data["countries"].([]any)
	if len(countries) != 1 || countries[0].(map[string]any)["country"] != "US" {
		t.Fatalf("countries leak or mismatch: %+v", countries)
	}

	recent, _ := data["recent"].([]any)
	if len(recent) != 2 {
		t.Fatalf("recent = %+v", recent)
	}
	sawRating := false
	sawNull := false
	for _, raw := range recent {
		row := raw.(map[string]any)
		switch v := row["rating"].(type) {
		case float64:
			if v != 18 {
				t.Fatalf("rating = %v", v)
			}
			sawRating = true
		case nil:
			sawNull = true
		default:
			t.Fatalf("rating type = %T", v)
		}
	}
	if !sawRating || !sawNull {
		t.Fatalf("recent ratings = %+v", recent)
	}
}
