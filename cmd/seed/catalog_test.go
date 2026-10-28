package main

import (
	"context"
	"path/filepath"
	"testing"

	"sodapop/ent/drink"
	"sodapop/internal/db"
)

func TestSeedStarterCatalogIsIdempotentAndComplete(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()

	// Run twice: the second run must add nothing.
	for run := 1; run <= 2; run++ {
		if err := seedStarterCatalog(ctx, database); err != nil {
			t.Fatalf("run %d: seed starter catalog: %v", run, err)
		}
	}

	brandCount, err := database.Client.Brand.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count brands: %v", err)
	}
	if brandCount != len(starterBrands) {
		t.Fatalf("brands = %d, want %d", brandCount, len(starterBrands))
	}

	drinks, err := database.Client.Drink.Query().All(ctx)
	if err != nil {
		t.Fatalf("query drinks: %v", err)
	}
	if len(drinks) != len(starterDrinks) {
		t.Fatalf("drinks = %d, want %d", len(drinks), len(starterDrinks))
	}
	if len(drinks) < 24 || len(drinks) > 30 {
		t.Fatalf("starter catalog has %d drinks, want 24-30", len(drinks))
	}

	barcodeCount, err := database.Client.DrinkBarcode.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count barcodes: %v", err)
	}
	if barcodeCount != 0 {
		t.Fatalf("starter catalog has %d barcodes, want 0", barcodeCount)
	}

	allowedContainers := map[drink.Container]bool{
		drink.ContainerCan:      true,
		drink.ContainerGlass:    true,
		drink.ContainerPet:      true,
		drink.ContainerFountain: true,
	}
	seenSlugs := make(map[string]bool, len(drinks))
	for _, d := range drinks {
		if seenSlugs[d.Slug] {
			t.Errorf("duplicate slug %q", d.Slug)
		}
		seenSlugs[d.Slug] = true

		if d.BrandID == nil {
			t.Errorf("%s: missing brand", d.Slug)
		}
		if d.StyleID == nil {
			t.Errorf("%s: missing style", d.Slug)
		}
		if d.VolumeMl <= 0 {
			t.Errorf("%s: volume_ml = %d, want > 0", d.Slug, d.VolumeMl)
		}
		if !allowedContainers[d.Container] {
			t.Errorf("%s: unexpected container %q", d.Slug, d.Container)
		}
		if d.Status != drink.StatusActive {
			t.Errorf("%s: status = %q, want active", d.Slug, d.Status)
		}
	}

	for _, want := range starterDrinks {
		if !seenSlugs[want.Slug] {
			t.Errorf("missing seeded drink %q", want.Slug)
		}
	}
}
