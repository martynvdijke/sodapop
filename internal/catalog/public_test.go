package catalog

import (
	"context"
	"errors"
	"testing"

	"sodapop/ent/drink"
)

func TestPublicBySlugReturnsSafeFacts(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	b, err := c.Brand.Create().SetName("Fizz Co").SetSlug("fizz-co").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.Style.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := c.Drink.Create().
		SetName("Cherry Cola").
		SetSlug("cherry-cola").
		SetDescription("A cherry cola.").
		SetBrandID(b.ID).
		SetStyleID(st.ID).
		SetStatus(drink.StatusActive).
		SetPublished(true).
		SetContainer(drink.ContainerGlass).
		SetVolumeMl(330).
		SetCaffeineMgPer100ml(34).
		SetSugarGPer100ml(10.5).
		SetCaloriesKcalPer100ml(42).
		SetSweetener(drink.SweetenerSugar).
		SetCarbonation(drink.CarbonationStandard).
		SetAcid(drink.AcidCitric).
		SetImageURL("https://example.com/cherry.png").
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := c.Flavour.Create().SetName("Cherry").SetSlug("cherry").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DrinkFlavour.Create().
		SetDrinkID(d.ID).SetFlavourID(f.ID).SetIntensity(4).Save(ctx); err != nil {
		t.Fatal(err)
	}

	u1 := mkUser(t, c, "public-u1")
	u2 := mkUser(t, c, "public-u2")
	rate(t, c, u1.ID, d.ID, 20)
	rate(t, c, u2.ID, d.ID, 16)

	got, err := svc.PublicBySlug(ctx, "cherry-cola")
	if err != nil {
		t.Fatalf("PublicBySlug: %v", err)
	}
	if got.Slug != "cherry-cola" || got.Name != "Cherry Cola" || got.Description != "A cherry cola." {
		t.Errorf("basic fields = %+v", got)
	}
	if got.Brand == nil || got.Brand.Name != "Fizz Co" || got.Brand.Slug != "fizz-co" {
		t.Errorf("brand = %+v", got.Brand)
	}
	if got.Style == nil || got.Style.Name != "Cola" || got.Style.Slug != "cola" {
		t.Errorf("style = %+v", got.Style)
	}
	if got.Container != "glass" || got.VolumeML != 330 || got.CaffeineMgPer100ml != 34 ||
		got.SugarGPer100ml != 10.5 || got.CaloriesKcalPer100ml != 42 ||
		got.Sweetener != "sugar" || got.Carbonation != "standard" || got.Acid != "citric" {
		t.Errorf("facts = %+v", got)
	}
	if got.Status != "active" {
		t.Errorf("status = %q", got.Status)
	}
	if got.ImageURL == nil || *got.ImageURL != "https://example.com/cherry.png" {
		t.Errorf("image_url = %v", got.ImageURL)
	}
	if len(got.Flavours) != 1 || got.Flavours[0].Name != "Cherry" || got.Flavours[0].Intensity != 4 {
		t.Errorf("flavours = %+v", got.Flavours)
	}
	if got.Rating != nil {
		t.Errorf("rating shown with only 2 raters: %+v", got.Rating)
	}

	u3 := mkUser(t, c, "public-u3")
	rate(t, c, u3.ID, d.ID, 12)
	got, err = svc.PublicBySlug(ctx, "cherry-cola")
	if err != nil {
		t.Fatalf("PublicBySlug after third rater: %v", err)
	}
	if got.Rating == nil || got.Rating.RaterCount != 3 || got.Rating.Average != 4.0 {
		t.Errorf("rating = %+v, want average 4.0 from 3 raters", got.Rating)
	}
}

func TestPublicBySlugHidesNonPublicDrinks(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	unpublished := mkDrink(t, c, "Unpublished", "unpublished", drink.StatusActive)
	provisional := mkDrink(t, c, "Provisional", "provisional", drink.StatusProvisional)
	if _, err := c.Drink.UpdateOneID(provisional.ID).SetPublished(true).Save(ctx); err != nil {
		t.Fatal(err)
	}
	merged := mkDrink(t, c, "Merged", "merged", drink.StatusActive)
	target := mkDrink(t, c, "Target", "target", drink.StatusActive)
	if _, err := c.Drink.UpdateOneID(merged.ID).
		SetPublished(true).SetMergedIntoID(target.ID).Save(ctx); err != nil {
		t.Fatal(err)
	}
	mergedStatus := mkDrink(t, c, "Merged Status", "merged-status", drink.StatusMerged)
	if _, err := c.Drink.UpdateOneID(mergedStatus.ID).SetPublished(true).Save(ctx); err != nil {
		t.Fatal(err)
	}
	discontinued := mkDrink(t, c, "Discontinued", "discontinued", drink.StatusDiscontinued)
	if _, err := c.Drink.UpdateOneID(discontinued.ID).SetPublished(true).Save(ctx); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		slug string
	}{
		{"unpublished", unpublished.Slug},
		{"provisional", provisional.Slug},
		{"merged", merged.Slug},
		{"merged status", mergedStatus.Slug},
		{"discontinued", discontinued.Slug},
		{"unknown", "does-not-exist"},
		{"empty", ""},
		{"upper case", "Upper-Case"},
		{"underscore", "under_score"},
		{"space", "with space"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.PublicBySlug(ctx, tc.slug); !errors.Is(err, ErrNotFound) {
				t.Fatalf("PublicBySlug(%q) err = %v, want ErrNotFound", tc.slug, err)
			}
		})
	}
}

func TestPublicBySlugAcceptsLimitedAndSeasonal(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	for _, status := range []drink.Status{drink.StatusLimited, drink.StatusSeasonal} {
		d := mkDrink(t, database.Client, string(status), string(status), status)
		if _, err := database.Client.Drink.UpdateOneID(d.ID).SetPublished(true).Save(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.PublicBySlug(ctx, string(status)); err != nil {
			t.Errorf("%s: %v", status, err)
		}
	}
}
