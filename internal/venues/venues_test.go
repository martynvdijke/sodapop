package venues

import (
	"testing"

	"sodapop/ent/drink"
)

func (h *venueHarness) addDrink(t *testing.T, name, slug string, status drink.Status) int {
	t.Helper()
	d, err := h.db.Client.Drink.Create().
		SetName(name).
		SetSlug(slug).
		SetStatus(status).
		Save(h.ctx)
	if err != nil {
		t.Fatalf("create drink %q: %v", name, err)
	}
	return d.ID
}

func (h *venueHarness) seenAt(t *testing.T, venueID, drinkID int) {
	t.Helper()
	if _, err := h.db.Client.VenueDrink.Create().
		SetVenueID(venueID).
		SetDrinkID(drinkID).
		Save(h.ctx); err != nil {
		t.Fatalf("create venue drink: %v", err)
	}
}

func TestSetOnOfferTogglesAndReportsMissingRows(t *testing.T) {
	h := newVenueHarness(t)
	venue := h.create(t, "Offer Hall", "Bristol", 0, 0)
	drinkID := h.addDrink(t, "Cola Classic", "cola-classic", drink.StatusActive)
	h.seenAt(t, venue.ID, drinkID)
	mergedDrinkID := h.addDrink(t, "Old Cola", "old-cola", drink.StatusMerged)
	h.seenAt(t, venue.ID, mergedDrinkID)

	tests := []struct {
		name      string
		slug      string
		onOffer   bool
		wantFound bool
		wantState bool
	}{
		{name: "mark on offer", slug: "cola-classic", onOffer: true, wantFound: true, wantState: true},
		{name: "remove offer", slug: "cola-classic", onOffer: false, wantFound: true, wantState: false},
		{name: "unknown drink", slug: "not-a-drink", onOffer: true, wantFound: false},
		{name: "merged drink", slug: "old-cola", onOffer: true, wantFound: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found, err := h.svc.SetOnOffer(h.ctx, venue.ID, tc.slug, tc.onOffer)
			if err != nil {
				t.Fatalf("SetOnOffer: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if !tc.wantFound {
				return
			}
			rows, err := h.db.Client.VenueDrink.Query().All(h.ctx)
			if err != nil {
				t.Fatalf("query venue drinks: %v", err)
			}
			foundRow := false
			var state bool
			for _, row := range rows {
				if row.VenueID == venue.ID && row.DrinkID == drinkID {
					state = row.OnOffer
					foundRow = true
				}
			}
			if !foundRow {
				t.Fatalf("venue drink row for %s missing", tc.slug)
			}
			if state != tc.wantState {
				t.Fatalf("on_offer = %v, want %v", state, tc.wantState)
			}
		})
	}
}

func TestDrinksForAdminListsEverySeenDrink(t *testing.T) {
	h := newVenueHarness(t)
	venue := h.create(t, "Busy Bar", "Bristol", 0, 0)
	firstID := h.addDrink(t, "Lemon Fizz", "lemon-fizz", drink.StatusActive)
	secondID := h.addDrink(t, "Cherry Fizz", "cherry-fizz", drink.StatusActive)
	h.seenAt(t, venue.ID, firstID)
	h.seenAt(t, venue.ID, secondID)

	drinks, err := h.svc.DrinksForAdmin(h.ctx, venue.ID, 0)
	if err != nil {
		t.Fatalf("DrinksForAdmin: %v", err)
	}
	if len(drinks) != 2 {
		t.Fatalf("drinks = %+v, want 2", drinks)
	}
}
