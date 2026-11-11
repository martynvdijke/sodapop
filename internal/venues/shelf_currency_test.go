package venues

import (
	"errors"
	"testing"
	"time"

	"sodapop/ent/drink"
)

func TestShelfReportCurrency(t *testing.T) {
	tests := []struct {
		name     string
		currency string
		want     string
		wantErr  error
	}{
		{"lowercase is normalized", "eur", "EUR", nil},
		{"already uppercase", "GBP", "GBP", nil},
		{"blank is allowed", "", "", nil},
		{"too long", "EURO", "", ErrInvalid},
		{"not letters", "E1R", "", ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newVenueHarness(t)
			v := h.create(t, "Corner Shop", "Bristol", 0, 0)
			h.addDrink(t, "Ginger Ale", "ginger-ale", drink.StatusActive)

			price := 199
			entry, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, ShelfReportInput{
				DrinkSlug:  "ginger-ale",
				PriceCents: &price,
				Currency:   tt.currency,
				SeenNow:    true,
			})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReportShelf: %v", err)
			}
			if entry.Currency != tt.want {
				t.Fatalf("currency = %q, want %q", entry.Currency, tt.want)
			}

			// The merged shelf read must carry the same currency, not just the
			// value ReportShelf happened to return.
			shelf, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, time.Now())
			if err != nil {
				t.Fatalf("Shelf: %v", err)
			}
			got := shelfByDrink(shelf, entry.DrinkID)
			if got == nil {
				t.Fatal("drink missing from shelf")
			}
			if got.Currency != tt.want {
				t.Fatalf("shelf currency = %q, want %q", got.Currency, tt.want)
			}
		})
	}
}
