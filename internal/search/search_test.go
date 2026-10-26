package search

import (
	"context"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/internal/catalog"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

func newTestService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return New(catalog.New(&config.Config{}, database)), database
}

func mkBrand(t *testing.T, database *db.DB, name, slug string) *ent.Brand {
	t.Helper()
	b, err := database.Client.Brand.Create().SetName(name).SetSlug(slug).Save(context.Background())
	if err != nil {
		t.Fatalf("brand %s: %v", name, err)
	}
	return b
}

func mkStyle(t *testing.T, database *db.DB, name, slug string) *ent.Style {
	t.Helper()
	s, err := database.Client.Style.Create().SetName(name).SetSlug(slug).Save(context.Background())
	if err != nil {
		t.Fatalf("style %s: %v", name, err)
	}
	return s
}

func mkDrink(t *testing.T, database *db.DB, name, slug string, status drink.Status, brandID, styleID int) *ent.Drink {
	t.Helper()
	create := database.Client.Drink.Create().
		SetName(name).SetSlug(slug).SetStatus(status)
	if brandID != 0 {
		create.SetBrandID(brandID)
	}
	if styleID != 0 {
		create.SetStyleID(styleID)
	}
	d, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("drink %s: %v", name, err)
	}
	return d
}

func TestSearchCapsPerTypeAndTotal(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	b := mkBrand(t, database, "Cola House", "cola-house")
	st := mkStyle(t, database, "Cola Style", "cola-style")
	for i := 0; i < 6; i++ {
		mkDrink(t, database, "Cola "+string(rune('A'+i)), "cola-"+string(rune('a'+i)), drink.StatusActive, b.ID, st.ID)
	}
	for i := 0; i < 5; i++ {
		mkBrand(t, database, "Cola Brand "+string(rune('A'+i)), "cola-brand-"+string(rune('a'+i)))
		mkStyle(t, database, "Cola Family "+string(rune('A'+i)), "cola-family-"+string(rune('a'+i)))
	}

	res, err := svc.Search(ctx, "cola", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Drinks) != MaxDrinks {
		t.Errorf("drinks = %d, want %d", len(res.Drinks), MaxDrinks)
	}
	if len(res.Brands) != MaxBrands {
		t.Errorf("brands = %d, want %d", len(res.Brands), MaxBrands)
	}
	if len(res.Styles) != MaxStyles {
		t.Errorf("styles = %d, want %d", len(res.Styles), MaxStyles)
	}
	if res.Total != MaxTotal {
		t.Errorf("total = %d, want %d", res.Total, MaxTotal)
	}
	if res.Total != len(res.Drinks)+len(res.Brands)+len(res.Styles) {
		t.Errorf("total = %d, want sum of groups", res.Total)
	}
}

func TestSearchLimitPerType(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	b := mkBrand(t, database, "Cola House", "cola-house")
	st := mkStyle(t, database, "Cola Style", "cola-style")
	for i := 0; i < 6; i++ {
		mkDrink(t, database, "Cola "+string(rune('A'+i)), "cola-"+string(rune('a'+i)), drink.StatusActive, b.ID, st.ID)
		mkBrand(t, database, "Cola Brand "+string(rune('A'+i)), "cola-brand-"+string(rune('a'+i)))
		mkStyle(t, database, "Cola Family "+string(rune('A'+i)), "cola-family-"+string(rune('a'+i)))
	}

	res, err := svc.Search(ctx, "cola", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Drinks) > 2 || len(res.Brands) > 2 || len(res.Styles) > 2 {
		t.Fatalf("limit not applied: drinks=%d brands=%d styles=%d",
			len(res.Drinks), len(res.Brands), len(res.Styles))
	}
	if res.Total > MaxTotal {
		t.Errorf("total = %d, want <= %d", res.Total, MaxTotal)
	}
}

func TestSearchPrefixRanksFirst(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	mkBrand(t, database, "Big Cola", "big-cola")
	mkBrand(t, database, "Cola Company", "cola-company")
	mkStyle(t, database, "Fizzy Cola", "fizzy-cola")
	mkStyle(t, database, "Cola Family", "cola-family")

	res, err := svc.Search(ctx, "cola", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Brands) < 2 || res.Brands[0].Name != "Cola Company" {
		t.Fatalf("brands = %+v, want prefix match first", res.Brands)
	}
	if len(res.Styles) < 2 || res.Styles[0].Name != "Cola Family" {
		t.Fatalf("styles = %+v, want prefix match first", res.Styles)
	}
}

func TestSearchDrinkAliasAndRanking(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	mkDrink(t, database, "Cola", "cola", drink.StatusActive, 0, 0)
	long := mkDrink(t, database, "Super Extra Cola Cola Cola", "super-extra-cola", drink.StatusActive, 0, 0)
	if _, err := database.Client.DrinkAlias.Create().
		SetDrinkID(long.ID).SetAlias("bubbly pop").Save(ctx); err != nil {
		t.Fatalf("alias: %v", err)
	}

	res, err := svc.Search(ctx, "cola", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Drinks) < 2 {
		t.Fatalf("drinks = %+v, want both cola drinks", res.Drinks)
	}
	if res.Drinks[0].Name != "Cola" {
		t.Errorf("first drink = %q, want exact short match first", res.Drinks[0].Name)
	}

	alias, err := svc.Search(ctx, "bubbly", 0)
	if err != nil {
		t.Fatalf("alias search: %v", err)
	}
	if len(alias.Drinks) != 1 || alias.Drinks[0].Slug != "super-extra-cola" {
		t.Fatalf("alias results = %+v, want super-extra-cola", alias.Drinks)
	}
}

func TestSearchExcludesMergedAndDeleted(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	mkDrink(t, database, "Cola Active", "cola-active", drink.StatusActive, 0, 0)
	mkDrink(t, database, "Cola Merged", "cola-merged", drink.StatusMerged, 0, 0)
	gone := mkDrink(t, database, "Cola Deleted", "cola-deleted", drink.StatusActive, 0, 0)
	if err := database.Client.Drink.DeleteOne(gone).Exec(ctx); err != nil {
		t.Fatalf("delete drink: %v", err)
	}

	res, err := svc.Search(ctx, "cola", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, d := range res.Drinks {
		if d.Slug == "cola-merged" || d.Slug == "cola-deleted" {
			t.Errorf("excluded drink surfaced: %+v", d)
		}
	}
	if len(res.Drinks) != 1 || res.Drinks[0].Slug != "cola-active" {
		t.Fatalf("drinks = %+v, want only cola-active", res.Drinks)
	}
}

func TestSearchShortQueryDoesNoWork(t *testing.T) {
	svc := New(nil) // no catalog: a short query must return before any DB access.
	for _, q := range []string{"", " ", "a", " a "} {
		res, err := svc.Search(context.Background(), q, 0)
		if err != nil {
			t.Fatalf("short query %q: %v", q, err)
		}
		if res.Total != 0 || len(res.Drinks)+len(res.Brands)+len(res.Styles) != 0 {
			t.Errorf("short query %q returned results: %+v", q, res)
		}
	}
}

func TestSearchNoDuplicateItemsAcrossGroups(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	b := mkBrand(t, database, "Cola Company", "cola-company")
	st := mkStyle(t, database, "Cola Family", "cola-family")
	mkDrink(t, database, "Cola Classic", "cola-classic", drink.StatusActive, b.ID, st.ID)

	res, err := svc.Search(ctx, "cola", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	seen := map[string]bool{}
	for _, group := range [][]Item{res.Drinks, res.Brands, res.Styles} {
		for _, item := range group {
			key := item.Type + ":" + item.ID
			if seen[key] {
				t.Errorf("duplicate item %s across groups", key)
			}
			seen[key] = true
		}
	}
	if len(seen) != res.Total {
		t.Errorf("unique items = %d, total = %d", len(seen), res.Total)
	}
}

func TestSearchEscapesLikeWildcards(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	mkBrand(t, database, "100% Cola", "100-cola")
	mkBrand(t, database, "Plain Cola", "plain-cola")

	res, err := svc.Search(ctx, "100%", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Brands) != 1 || res.Brands[0].Slug != "100-cola" {
		t.Fatalf("brands = %+v, want only 100-cola", res.Brands)
	}
}
