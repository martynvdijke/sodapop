package catalog

import (
	"context"
	"math"
	"testing"

	"sodapop/ent/drink"
)

func TestBeverageTypeEnumFallback(t *testing.T) {
	cases := map[string]drink.BeverageType{
		"":        drink.BeverageTypeSoda,
		"bogus":   drink.BeverageTypeSoda,
		"tea":     drink.BeverageTypeTea,
		"COFFEE":  drink.BeverageTypeCoffee,
		"na_beer": drink.BeverageTypeNaBeer,
		"other":   drink.BeverageTypeOther,
	}
	for in, want := range cases {
		if got := beverageTypeEnum(in); got != want {
			t.Errorf("beverageTypeEnum(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClampABV(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{-1, 0},
		{0, 0},
		{5.5, 5.5},
		{100, 100},
		{101, 100},
		{math.NaN(), 0},
		{math.Inf(1), 0},
		{math.Inf(-1), 0},
	}
	for _, tc := range cases {
		if got := clampABV(tc.in); got != tc.want {
			t.Errorf("clampABV(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCreateProvisionalStoresBeverageAndABV(t *testing.T) {
	svc, database := newBrowseService(t)
	u := mkUser(t, database.Client, "beverage-owner")
	ctx := context.Background()

	d, created, err := svc.CreateProvisional(ctx, u.ID, CreateInput{
		Name:         "Test Espresso",
		BeverageType: "coffee",
		ABVPercent:   0,
		Container:    "glass",
		VolumeML:     250,
	})
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	if d.BeverageType != drink.BeverageTypeCoffee {
		t.Errorf("beverage_type = %q, want coffee", d.BeverageType)
	}
	if d.AbvPercent != 0 {
		t.Errorf("abv_percent = %v, want 0", d.AbvPercent)
	}

	// An unknown type falls back to soda and an out-of-range ABV is clamped.
	unknown, _, err := svc.CreateProvisional(ctx, u.ID, CreateInput{
		Name: "Mystery Drink", BeverageType: "nonsense", ABVPercent: 250,
	})
	if err != nil {
		t.Fatalf("create unknown: %v", err)
	}
	if unknown.BeverageType != drink.BeverageTypeSoda {
		t.Errorf("fallback beverage_type = %q, want soda", unknown.BeverageType)
	}
	if unknown.AbvPercent != 100 {
		t.Errorf("clamped abv_percent = %v, want 100", unknown.AbvPercent)
	}
}
