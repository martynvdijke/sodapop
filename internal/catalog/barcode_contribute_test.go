package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sodapop/ent/catalogrevision"
	"sodapop/ent/drink"
	"sodapop/ent/drinkbarcode"
)

func TestContributeBarcodeNormalization(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		want    string
		wantErr error
	}{
		{"ean13 pads to gtin14", "5449000000996", "05449000000996", nil},
		{"upca pads to gtin14", "036000291452", "00036000291452", nil},
		{"ean8 pads to gtin14", "96385074", "00000096385074", nil},
		{"already gtin14", "00000096385074", "00000096385074", nil},
		{"spaces are ignored", "5449 0000 00996", "05449000000996", nil},
		{"bad check digit", "5449000000995", "", ErrInvalidBarcode},
		{"too short", "123", "", ErrInvalidBarcode},
		{"letters", "54490000ABC", "", ErrInvalidBarcode},
		{"empty", "", "", ErrInvalidBarcode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, database := newBrowseService(t)
			d := mkDrink(t, database.Client, "Norm Cola", "norm-cola", drink.StatusActive)

			gtin, created, err := svc.ContributeBarcode(context.Background(), "user-1", d.Slug, tt.code, "")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if n, _ := database.Client.DrinkBarcode.Query().Count(context.Background()); n != 0 {
					t.Fatalf("invalid code stored %d rows", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("contribute: %v", err)
			}
			if gtin != tt.want || !created {
				t.Fatalf("gtin/created = %q/%v, want %q/true", gtin, created, tt.want)
			}
		})
	}
}

func TestContributeBarcodeIdempotent(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	d := mkDrink(t, database.Client, "Idem Cola", "idem-cola", drink.StatusActive)
	u := mkUser(t, database.Client, "contrib")

	gtin, created, err := svc.ContributeBarcode(ctx, u.ID, d.Slug, "5449000000996", "EAN-13")
	if err != nil || !created {
		t.Fatalf("first = %q/%v/%v", gtin, created, err)
	}

	gtin2, created2, err := svc.ContributeBarcode(ctx, u.ID, d.Slug, "5449 0000 00996", "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if gtin2 != gtin || created2 {
		t.Fatalf("second = %q/%v, want %q/false", gtin2, created2, gtin)
	}
	if n, _ := database.Client.DrinkBarcode.Query().Count(ctx); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}

	row, err := database.Client.DrinkBarcode.Query().Only(ctx)
	if err != nil {
		t.Fatalf("barcode: %v", err)
	}
	if row.Source != drinkbarcode.SourceUser {
		t.Fatalf("source = %q, want user", row.Source)
	}
	if row.Symbology != "ean-13" {
		t.Fatalf("symbology = %q", row.Symbology)
	}
	if row.RawCode == "" {
		t.Fatal("raw code was not stored")
	}
}

func TestContributeBarcodeConflict(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	mine := mkDrink(t, database.Client, "Mine Cola", "mine-cola", drink.StatusActive)
	theirs := mkDrink(t, database.Client, "Theirs Cola", "theirs-cola", drink.StatusActive)

	if _, _, err := svc.ContributeBarcode(ctx, "u1", theirs.Slug, "5449000000996", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, created, err := svc.ContributeBarcode(ctx, "u2", mine.Slug, "5449000000996", "")
	if created {
		t.Fatal("conflict reported a create")
	}
	if !errors.Is(err, ErrBarcodeTaken) {
		t.Fatalf("err = %v, want ErrBarcodeTaken", err)
	}
	var conflict *BarcodeConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err is not a BarcodeConflictError: %v", err)
	}
	if conflict.DrinkID != theirs.ID || conflict.DrinkName != "Theirs Cola" || conflict.DrinkSlug != "theirs-cola" {
		t.Fatalf("conflict = %+v", conflict)
	}
}

func TestContributeBarcodeKeepsExistingSource(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	d := mkDrink(t, database.Client, "Off Cola", "off-cola", drink.StatusActive)

	// Pretend the row arrived from Open Food Facts before the member typed the
	// same digits. A contribution must not re-label it.
	if _, err := database.Client.DrinkBarcode.Create().
		SetDrinkID(d.ID).SetGtin14("05449000000996").SetSource(drinkbarcode.SourceOff).
		Save(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, created, err := svc.ContributeBarcode(ctx, "u1", d.Slug, "5449000000996", "")
	if err != nil {
		t.Fatalf("contribute: %v", err)
	}
	if created {
		t.Fatal("existing row reported as created")
	}
	row, err := database.Client.DrinkBarcode.Query().Only(ctx)
	if err != nil {
		t.Fatalf("barcode: %v", err)
	}
	if row.Source != drinkbarcode.SourceOff {
		t.Fatalf("source was rewritten to %q", row.Source)
	}
}

func TestContributeBarcodeRecordsRevision(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	d := mkDrink(t, database.Client, "Rev Cola", "rev-cola", drink.StatusActive)
	u := mkUser(t, database.Client, "reviser")

	if _, _, err := svc.ContributeBarcode(ctx, u.ID, d.Slug, "5449000000996", ""); err != nil {
		t.Fatalf("contribute: %v", err)
	}
	row, err := database.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityDrink), catalogrevision.EntityID(d.ID)).
		Only(ctx)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	if row.Action != "barcode" {
		t.Fatalf("action = %q", row.Action)
	}
	if row.UserID == nil || *row.UserID != u.ID {
		t.Fatalf("revision user = %v, want %s", row.UserID, u.ID)
	}
	if !strings.Contains(row.DiffJSON, "05449000000996") {
		t.Fatalf("diff missing gtin: %s", row.DiffJSON)
	}
}

func TestContributeBarcodeUnknownDrink(t *testing.T) {
	svc, _ := newBrowseService(t)
	_, _, err := svc.ContributeBarcode(context.Background(), "u1", "nope", "5449000000996", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A merged drink is not a contribution target: resolution would never reach it.
func TestContributeBarcodeRejectsMergedDrink(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	d := mkDrink(t, database.Client, "Merged Cola", "merged-cola", drink.StatusMerged)
	_, _, err := svc.ContributeBarcode(ctx, "u1", d.Slug, "5449000000996", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
