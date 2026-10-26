package catalog

import (
	"context"
	"testing"

	"sodapop/ent/drink"
)

func TestSearchFiltersByStyleSlug(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()

	colaStyle := database.Client.Style.Create().SetName("Cola").SetSlug("cola").SaveX(ctx)
	rootStyle := database.Client.Style.Create().SetName("Soda").SetSlug("soda").SaveX(ctx)

	withStyle := func(name, slug string, styleID int) {
		if _, err := database.Client.Drink.Create().
			SetName(name).SetSlug(slug).SetStatus(drink.StatusActive).SetStyleID(styleID).Save(ctx); err != nil {
			t.Fatalf("drink %s: %v", name, err)
		}
	}
	withStyle("Test Cola", "test-cola", colaStyle.ID)
	withStyle("Test Root Beer", "test-root-beer", rootStyle.ID)

	all, err := svc.Search(ctx, "test", "", 10)
	if err != nil {
		t.Fatalf("search all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered results = %d, want 2", len(all))
	}

	filtered, err := svc.Search(ctx, "test", "cola", 10)
	if err != nil {
		t.Fatalf("search style: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Slug != "test-cola" {
		t.Fatalf("style-filtered results = %+v, want only test-cola", filtered)
	}
}
