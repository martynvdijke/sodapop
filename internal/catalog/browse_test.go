package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

// openMemory returns a fresh migrated database for one test. SQLite shared
// memory is process-global, so a temp file keeps tests isolated.
func openMemory(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "browse.db"))
	if err != nil {
		t.Fatalf("init database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func newBrowseService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	database := openMemory(t)
	return New(&config.Config{}, database), database
}

func mkDrink(t *testing.T, c *ent.Client, name, slug string, status drink.Status) *ent.Drink {
	t.Helper()
	d, err := c.Drink.Create().SetName(name).SetSlug(slug).SetStatus(status).Save(context.Background())
	if err != nil {
		t.Fatalf("create drink %s: %v", name, err)
	}
	return d
}

func mkUser(t *testing.T, c *ent.Client, handle string) *ent.User {
	t.Helper()
	u, err := c.User.Create().
		SetHandle(handle).SetEmail(handle + "@example.com").Save(context.Background())
	if err != nil {
		t.Fatalf("create user %s: %v", handle, err)
	}
	return u
}

func rate(t *testing.T, c *ent.Client, userID string, drinkID, rating int) {
	t.Helper()
	if _, err := c.Checkin.Create().
		SetUserID(userID).SetDrinkID(drinkID).SetRating(rating).
		Save(context.Background()); err != nil {
		t.Fatalf("create checkin: %v", err)
	}
}

func names(drinks []BrowseDrink) []string {
	out := make([]string, len(drinks))
	for i, d := range drinks {
		out[i] = d.Name
	}
	return out
}

func assertNames(t *testing.T, got []BrowseDrink, want ...string) {
	t.Helper()
	g := names(got)
	if len(g) != len(want) {
		t.Fatalf("got %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("got %v, want %v", g, want)
		}
	}
}

func TestBrowseDrinksExcludesMergedAndProvisional(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	mkDrink(t, c, "Alpha", "alpha", drink.StatusActive)
	mkDrink(t, c, "Bravo", "bravo", drink.StatusLimited)
	mkDrink(t, c, "Charlie", "charlie", drink.StatusSeasonal)
	mkDrink(t, c, "Delta", "delta", drink.StatusMerged)
	mkDrink(t, c, "Echo", "echo", drink.StatusProvisional)
	mkDrink(t, c, "Foxtrot", "foxtrot", drink.StatusDiscontinued)

	res, err := svc.BrowseDrinks(ctx, BrowseFilter{})
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	assertNames(t, res.Drinks, "Alpha", "Bravo", "Charlie")

	res, err = svc.BrowseDrinks(ctx, BrowseFilter{Status: "discontinued"})
	if err != nil {
		t.Fatalf("browse discontinued: %v", err)
	}
	assertNames(t, res.Drinks, "Foxtrot")

	// A merged/provisional explicit status is never honoured.
	res, err = svc.BrowseDrinks(ctx, BrowseFilter{Status: "merged"})
	if err != nil {
		t.Fatalf("browse merged: %v", err)
	}
	assertNames(t, res.Drinks, "Alpha", "Bravo", "Charlie")
}

func TestBrowseDrinksFilters(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	brand, err := c.Brand.Create().SetName("Fizz Co").SetSlug("fizz-co").Save(ctx)
	if err != nil {
		t.Fatalf("create brand: %v", err)
	}
	root, err := c.Style.Create().SetName("Soda").SetSlug("soda").Save(ctx)
	if err != nil {
		t.Fatalf("create root style: %v", err)
	}
	child, err := c.Style.Create().SetName("Cola").SetSlug("cola").SetParentID(root.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create child style: %v", err)
	}
	citrus, err := c.Flavour.Create().SetName("Citrus").SetSlug("citrus").Save(ctx)
	if err != nil {
		t.Fatalf("create flavour: %v", err)
	}

	alpha, err := c.Drink.Create().
		SetName("Alpha").SetSlug("alpha").SetStatus(drink.StatusActive).
		SetBrandID(brand.ID).SetStyleID(root.ID).
		SetCaffeineMgPer100ml(0).SetSugarGPer100ml(0).Save(ctx)
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	bravo, err := c.Drink.Create().
		SetName("Bravo").SetSlug("bravo").SetStatus(drink.StatusActive).
		SetStyleID(child.ID).
		SetCaffeineMgPer100ml(34).SetSugarGPer100ml(10.5).Save(ctx)
	if err != nil {
		t.Fatalf("create bravo: %v", err)
	}
	if _, err := c.Drink.Create().
		SetName("Charlie").SetSlug("charlie").SetStatus(drink.StatusActive).
		SetCaffeineMgPer100ml(0).SetSugarGPer100ml(5).Save(ctx); err != nil {
		t.Fatalf("create charlie: %v", err)
	}
	if _, err := c.DrinkFlavour.Create().
		SetDrinkID(alpha.ID).SetFlavourID(citrus.ID).SetIntensity(4).Save(ctx); err != nil {
		t.Fatalf("create drink flavour: %v", err)
	}

	tests := []struct {
		name string
		f    BrowseFilter
		want []string
	}{
		{"brand", BrowseFilter{Brand: "fizz-co"}, []string{"Alpha"}},
		{"family style includes children", BrowseFilter{Style: "soda"}, []string{"Alpha", "Bravo"}},
		{"child style", BrowseFilter{Style: "cola"}, []string{"Bravo"}},
		{"flavour", BrowseFilter{Flavour: "citrus"}, []string{"Alpha"}},
		{"caffeine free", BrowseFilter{CaffeineFree: true}, []string{"Alpha", "Charlie"}},
		{"sugar free", BrowseFilter{SugarFree: true}, []string{"Alpha"}},
		{"combined", BrowseFilter{Style: "soda", CaffeineFree: true}, []string{"Alpha"}},
		{"unknown values ignored", BrowseFilter{Style: "nope", Flavour: "nope", Brand: "nope"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := svc.BrowseDrinks(ctx, tc.f)
			if err != nil {
				t.Fatalf("browse: %v", err)
			}
			if len(tc.want) == 0 {
				if len(res.Drinks) != 0 {
					t.Fatalf("got %v, want none", names(res.Drinks))
				}
				return
			}
			assertNames(t, res.Drinks, tc.want...)
		})
	}

	if bravo.BrandID != nil {
		t.Fatal("bravo should have no brand")
	}
}

func TestBrowseDrinksSortWhitelist(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	zulu, err := c.Drink.Create().
		SetName("Zulu").SetSlug("zulu").SetStatus(drink.StatusActive).
		SetCreatedAt("2024-01-04T00:00:00Z").Save(ctx)
	if err != nil {
		t.Fatalf("create zulu: %v", err)
	}
	alpha, err := c.Drink.Create().
		SetName("alpha").SetSlug("alpha").SetStatus(drink.StatusActive).
		SetCreatedAt("2024-01-02T00:00:00Z").Save(ctx)
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	mike, err := c.Drink.Create().
		SetName("Mike").SetSlug("mike").SetStatus(drink.StatusActive).
		SetCreatedAt("2024-01-03T00:00:00Z").Save(ctx)
	if err != nil {
		t.Fatalf("create mike: %v", err)
	}

	// Three raters on Mike, none elsewhere, so "rated" leads with Mike.
	u1 := mkUser(t, c, "rater1")
	u2 := mkUser(t, c, "rater2")
	u3 := mkUser(t, c, "rater3")
	for _, u := range []*ent.User{u1, u2, u3} {
		rate(t, c, u.ID, mike.ID, 20)
	}

	cases := map[string][]string{
		"":       {"alpha", "Mike", "Zulu"},
		"name":   {"alpha", "Mike", "Zulu"},
		"bogus":  {"alpha", "Mike", "Zulu"},
		"newest": {"Zulu", "Mike", "alpha"},
		"rated":  {"Mike", "alpha", "Zulu"},
		"NAME":   {"alpha", "Mike", "Zulu"},
	}
	for sort, want := range cases {
		res, err := svc.BrowseDrinks(ctx, BrowseFilter{Sort: sort})
		if err != nil {
			t.Fatalf("sort %q: %v", sort, err)
		}
		assertNames(t, res.Drinks, want...)
	}

	_ = zulu
	_ = alpha
}

func TestBrowseDrinksPagination(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()

	for i := 0; i < 30; i++ {
		name := "Drink " + string(rune('A'+i/10)) + string(rune('0'+i%10))
		slug := "drink-" + string(rune('a'+i/10)) + string(rune('0'+i%10))
		mkDrink(t, database.Client, name, slug, drink.StatusActive)
	}

	page1, err := svc.BrowseDrinks(ctx, BrowseFilter{Page: 1})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1.Drinks) != 24 || !page1.HasNext || page1.Page != 1 {
		t.Fatalf("page 1: len=%d hasNext=%v page=%d", len(page1.Drinks), page1.HasNext, page1.Page)
	}

	page2, err := svc.BrowseDrinks(ctx, BrowseFilter{Page: 2})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2.Drinks) != 6 || page2.HasNext {
		t.Fatalf("page 2: len=%d hasNext=%v", len(page2.Drinks), page2.HasNext)
	}

	page3, err := svc.BrowseDrinks(ctx, BrowseFilter{Page: 3})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page3.Drinks) != 0 || page3.HasNext {
		t.Fatalf("page 3: len=%d hasNext=%v", len(page3.Drinks), page3.HasNext)
	}

	// Zero and negative pages normalise to page 1.
	zero, err := svc.BrowseDrinks(ctx, BrowseFilter{Page: 0})
	if err != nil {
		t.Fatalf("page 0: %v", err)
	}
	if zero.Page != 1 || len(zero.Drinks) != 24 {
		t.Fatalf("page 0 normalised to %d with %d drinks", zero.Page, len(zero.Drinks))
	}
}

func TestBrowseDrinksRatingGate(t *testing.T) {
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

	res, err := svc.BrowseDrinks(ctx, BrowseFilter{})
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	byName := map[string]BrowseDrink{}
	for _, d := range res.Drinks {
		byName[d.Name] = d
	}
	h := byName["Hidden"]
	if h.AvgRating != nil || h.RatingShown || h.RaterCount != 2 {
		t.Fatalf("hidden: avg=%v shown=%v raters=%d", h.AvgRating, h.RatingShown, h.RaterCount)
	}
	s := byName["Shown"]
	if s.AvgRating == nil || !s.RatingShown || s.RaterCount != 3 {
		t.Fatalf("shown: avg=%v shown=%v raters=%d", s.AvgRating, s.RatingShown, s.RaterCount)
	}
	if *s.AvgRating != 16 {
		t.Fatalf("avg = %v, want 16", *s.AvgRating)
	}
}

func TestBrandsAndStyles(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	brand, err := c.Brand.Create().
		SetName("Fizz Co").SetSlug("fizz-co").SetCountry("NZ").
		SetDescription("Bubbles").SetLogoMediaID("media-1").Save(ctx)
	if err != nil {
		t.Fatalf("create brand: %v", err)
	}
	active := mkDrink(t, c, "Alpha", "alpha", drink.StatusActive)
	retired := mkDrink(t, c, "Bravo", "bravo", drink.StatusDiscontinued)
	mkDrink(t, c, "Charlie", "charlie", drink.StatusMerged)
	for _, d := range []*ent.Drink{active, retired} {
		if err := d.Update().SetBrandID(brand.ID).Exec(ctx); err != nil {
			t.Fatalf("attach brand: %v", err)
		}
	}

	brands, err := svc.ListBrands(ctx)
	if err != nil {
		t.Fatalf("list brands: %v", err)
	}
	if len(brands) != 1 {
		t.Fatalf("brands = %d, want 1", len(brands))
	}
	if brands[0].Name != "Fizz Co" || brands[0].Slug != "fizz-co" ||
		brands[0].Country != "NZ" || brands[0].LogoMediaID != "media-1" ||
		brands[0].DrinkCount != 2 {
		t.Fatalf("brand summary = %+v", brands[0])
	}

	page, err := svc.GetBrand(ctx, "fizz-co")
	if err != nil {
		t.Fatalf("get brand: %v", err)
	}
	if page.Name != "Fizz Co" || page.Description != "Bubbles" || page.Count != 2 || len(page.Drinks) != 2 {
		t.Fatalf("brand page = %+v", page)
	}
	if _, err := svc.GetBrand(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing brand err = %v, want ErrNotFound", err)
	}

	root, err := c.Style.Create().SetName("Soda").SetSlug("soda").Save(ctx)
	if err != nil {
		t.Fatalf("create root style: %v", err)
	}
	child, err := c.Style.Create().
		SetName("Cola").SetSlug("cola").SetParentID(root.ID).Save(ctx)
	if err != nil {
		t.Fatalf("create child style: %v", err)
	}
	if err := active.Update().SetStyleID(child.ID).Exec(ctx); err != nil {
		t.Fatalf("attach style: %v", err)
	}

	styles, err := svc.ListStyles(ctx)
	if err != nil {
		t.Fatalf("list styles: %v", err)
	}
	if len(styles) != 1 {
		t.Fatalf("families = %d, want 1", len(styles))
	}
	fam := styles[0]
	if fam.Name != "Soda" || len(fam.Children) != 1 || fam.Children[0].Name != "Cola" {
		t.Fatalf("family = %+v", fam)
	}
	if fam.DrinkCount != 1 || fam.Children[0].DrinkCount != 1 {
		t.Fatalf("family counts = %d/%d", fam.DrinkCount, fam.Children[0].DrinkCount)
	}

	rootPage, err := svc.GetStyle(ctx, "soda")
	if err != nil {
		t.Fatalf("get root style: %v", err)
	}
	if rootPage.ParentName != "" || len(rootPage.Children) != 1 || rootPage.Count != 1 {
		t.Fatalf("root style page = %+v", rootPage)
	}
	assertNames(t, rootPage.Drinks, "Alpha")

	childPage, err := svc.GetStyle(ctx, "cola")
	if err != nil {
		t.Fatalf("get child style: %v", err)
	}
	if childPage.ParentName != "Soda" || len(childPage.Children) != 0 || childPage.Count != 1 {
		t.Fatalf("child style page = %+v", childPage)
	}
	if _, err := svc.GetStyle(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing style err = %v, want ErrNotFound", err)
	}
}

func TestBrowseDrops(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	limitedDated, err := c.Drink.Create().
		SetName("Limited Dated").SetSlug("limited-dated").
		SetStatus(drink.StatusLimited).SetLimitedUntil("2026-12-31T00:00:00Z").Save(ctx)
	if err != nil {
		t.Fatalf("create limited: %v", err)
	}
	if _, err := c.Drink.Create().
		SetName("Limited Undated").SetSlug("limited-undated").
		SetStatus(drink.StatusLimited).Save(ctx); err != nil {
		t.Fatalf("create undated limited: %v", err)
	}
	mkDrink(t, c, "Seasonal", "seasonal", drink.StatusSeasonal)

	discDated, err := c.Drink.Create().
		SetName("Discontinued Dated").SetSlug("discontinued-dated").
		SetStatus(drink.StatusDiscontinued).SetDiscontinuedAt("2025-01-01T00:00:00Z").Save(ctx)
	if err != nil {
		t.Fatalf("create discontinued: %v", err)
	}
	if _, err := c.Drink.Create().
		SetName("Discontinued Undated").SetSlug("discontinued-undated").
		SetStatus(drink.StatusDiscontinued).Save(ctx); err != nil {
		t.Fatalf("create undated discontinued: %v", err)
	}
	mkDrink(t, c, "Merged", "merged", drink.StatusMerged)

	drops, err := svc.BrowseDrops(ctx)
	if err != nil {
		t.Fatalf("drops: %v", err)
	}
	assertNames(t, drops.Limited, "Limited Dated")
	assertNames(t, drops.Seasonal, "Seasonal")
	assertNames(t, drops.Discontinued, "Discontinued Dated")

	if drops.Limited[0].ID != limitedDated.ID || drops.Discontinued[0].ID != discDated.ID {
		t.Fatal("drops returned unexpected ids")
	}
	if drops.Limited[0].AvgRating != nil || drops.Limited[0].RatingShown {
		t.Fatal("drops must not show an ungated rating")
	}
}

func TestDetailReturnsAliases(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	c := database.Client

	d := mkDrink(t, c, "Cherry Cola", "cherry-cola", drink.StatusActive)
	for _, alias := range []string{"Cherry Coke", "Cheerwine Cola"} {
		if _, err := c.DrinkAlias.Create().SetDrinkID(d.ID).SetAlias(alias).Save(ctx); err != nil {
			t.Fatalf("create alias: %v", err)
		}
	}

	detail, err := svc.GetBySlug(ctx, "cherry-cola")
	if err != nil {
		t.Fatalf("get by slug: %v", err)
	}
	if len(detail.Aliases) != 2 {
		t.Fatalf("aliases = %v, want 2", detail.Aliases)
	}
	if detail.Aliases[0] != "Cheerwine Cola" {
		t.Fatalf("aliases = %v, want sorted", detail.Aliases)
	}
}
