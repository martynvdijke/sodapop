package catalog

import (
	"context"
	"errors"
	"testing"

	"sodapop/ent/drink"
)

func TestCompareValidation(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	slugs := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf"}
	for _, slug := range slugs {
		mkDrink(t, c, slug, slug, drink.StatusActive)
	}

	tests := []struct {
		name    string
		slugs   []string
		wantErr error
		wantN   int
	}{
		{"two", []string{"alpha", "bravo"}, nil, 2},
		{"six", slugs[:6], nil, 6},
		{"none", nil, ErrCompareCount, 0},
		{"one", []string{"alpha"}, ErrCompareCount, 0},
		{"seven", slugs, ErrCompareCount, 0},
		{"empty entries ignored", []string{"alpha", "", "  ", "bravo"}, nil, 2},
		{"slugs trimmed", []string{" alpha ", "bravo\t"}, nil, 2},
		{"duplicate", []string{"alpha", "bravo", "alpha"}, ErrCompareDuplicate, 0},
		{"duplicate after trim", []string{"alpha", " alpha "}, ErrCompareDuplicate, 0},
		{"unknown", []string{"alpha", "nope"}, ErrNotFound, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.Compare(ctx, tc.slugs)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if got != nil {
					t.Fatalf("got %d details on error, want nil", len(got))
				}
				return
			}
			if len(got) != tc.wantN {
				t.Fatalf("got %d details, want %d", len(got), tc.wantN)
			}
			for i, slug := range []string{"alpha", "bravo"} {
				if i < len(got) && got[i].Drink.Slug != slug {
					t.Fatalf("detail %d slug = %q, want %q", i, got[i].Drink.Slug, slug)
				}
			}
		})
	}
}

func TestCompareExcludesMergedAndProvisional(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	mkDrink(t, c, "Alpha", "alpha", drink.StatusActive)
	mkDrink(t, c, "Merged", "merged", drink.StatusMerged)
	mkDrink(t, c, "Provisional", "provisional", drink.StatusProvisional)

	for _, slug := range []string{"merged", "provisional"} {
		if _, err := svc.Compare(ctx, []string{"alpha", slug}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("compare %q err = %v, want ErrNotFound", slug, err)
		}
	}
}

func TestComparePopulatesDetail(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	brand, err := c.Brand.Create().SetName("Fizz Co").SetSlug("fizz-co").Save(ctx)
	if err != nil {
		t.Fatalf("create brand: %v", err)
	}
	style, err := c.Style.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("create style: %v", err)
	}
	flavour, err := c.Flavour.Create().SetName("Citrus").SetSlug("citrus").Save(ctx)
	if err != nil {
		t.Fatalf("create flavour: %v", err)
	}

	alpha, err := c.Drink.Create().
		SetName("Alpha").SetSlug("alpha").SetStatus(drink.StatusActive).
		SetBrandID(brand.ID).SetStyleID(style.ID).
		SetContainer(drink.ContainerGlass).SetVolumeMl(330).
		SetSugarGPer100ml(10.5).SetCaffeineMgPer100ml(34).
		SetCaloriesKcalPer100ml(42).SetSweetener(drink.SweetenerSugar).
		SetCarbonation(drink.CarbonationStandard).SetAcid(drink.AcidCitric).
		Save(ctx)
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if _, err := c.DrinkFlavour.Create().
		SetDrinkID(alpha.ID).SetFlavourID(flavour.ID).SetIntensity(4).Save(ctx); err != nil {
		t.Fatalf("create drink flavour: %v", err)
	}
	mkDrink(t, c, "Bravo", "bravo", drink.StatusActive)

	got, err := svc.Compare(ctx, []string{"alpha", "bravo"})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d details, want 2", len(got))
	}

	a := got[0]
	if a.BrandName != "Fizz Co" || a.StyleName != "Cola" {
		t.Fatalf("brand/style = %q/%q", a.BrandName, a.StyleName)
	}
	if len(a.Flavours) != 1 || a.Flavours[0].Name != "Citrus" || a.Flavours[0].Intensity != 4 {
		t.Fatalf("flavours = %+v", a.Flavours)
	}
	d := a.Drink
	if d.Container != drink.ContainerGlass || d.VolumeMl != 330 ||
		d.SugarGPer100ml != 10.5 || d.CaffeineMgPer100ml != 34 ||
		d.CaloriesKcalPer100ml != 42 || d.Sweetener != drink.SweetenerSugar ||
		d.Carbonation != drink.CarbonationStandard || d.Acid != drink.AcidCitric {
		t.Fatalf("drink fields = %+v", d)
	}
}

func TestCompareRatingShownOnlyAtThreeRaters(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	hidden := mkDrink(t, c, "Hidden", "hidden", drink.StatusActive)
	shown := mkDrink(t, c, "Shown", "shown", drink.StatusActive)

	u1 := mkUser(t, c, "u1")
	u2 := mkUser(t, c, "u2")
	u3 := mkUser(t, c, "u3")

	rate(t, c, u1.ID, hidden.ID, 20)
	rate(t, c, u2.ID, hidden.ID, 16)

	rate(t, c, u1.ID, shown.ID, 20)
	rate(t, c, u2.ID, shown.ID, 16)
	rate(t, c, u3.ID, shown.ID, 12)

	got, err := svc.Compare(ctx, []string{"hidden", "shown"})
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d details, want 2", len(got))
	}

	h := got[0].Rating
	if h.Shown || h.Avg != nil || h.RaterCount != 2 {
		t.Fatalf("hidden rating = %+v, want hidden with 2 raters", h)
	}
	s := got[1].Rating
	if !s.Shown || s.Avg == nil || s.RaterCount != 3 {
		t.Fatalf("shown rating = %+v, want shown with 3 raters", s)
	}
	if *s.Avg != 16 {
		t.Fatalf("avg = %v, want 16", *s.Avg)
	}
}
