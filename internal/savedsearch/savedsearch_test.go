package savedsearch_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/ent/notification"
	entsavedsearch "sodapop/ent/savedsearch"
	"sodapop/internal/db"
	"sodapop/internal/savedsearch"
	"sodapop/internal/social"
)

type testEnv struct {
	ctx    context.Context
	db     *db.DB
	svc    *savedsearch.Service
	alice  *ent.User
	bob    *ent.User
	carol  *ent.User
	drink  *ent.Drink
	water  *ent.Drink
	fanta  *ent.Brand
	cola   *ent.Style
	cherry *ent.Flavour
	sugar  *ent.Flavour
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "savedsearch.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()

	mkUser := func(handle string) *ent.User {
		u, err := database.Client.User.Create().
			SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(handle).
			Save(ctx)
		if err != nil {
			t.Fatalf("user %s: %v", handle, err)
		}
		return u
	}
	alice, bob, carol := mkUser("alice"), mkUser("bob"), mkUser("carol")

	fanta, err := database.Client.Brand.Create().SetName("Fanta").SetSlug("fanta").Save(ctx)
	if err != nil {
		t.Fatalf("brand: %v", err)
	}
	cola, err := database.Client.Style.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	cherry, err := database.Client.Flavour.Create().SetName("Cherry").SetSlug("cherry").Save(ctx)
	if err != nil {
		t.Fatalf("flavour: %v", err)
	}
	sugar, err := database.Client.Flavour.Create().SetName("Sugar").SetSlug("sugar").Save(ctx)
	if err != nil {
		t.Fatalf("flavour: %v", err)
	}

	d, err := database.Client.Drink.Create().
		SetName("Cherry Cola").SetSlug("cherry-cola").
		SetDescription("Sweet cherry soda").
		SetBrandID(fanta.ID).SetStyleID(cola.ID).
		SetBeverageType(drink.BeverageTypeSoda).
		SetCaffeineMgPer100ml(12).SetSugarGPer100ml(10.6).
		SetPublished(true).SetStatus(drink.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	for _, fl := range []*ent.Flavour{cherry, sugar} {
		if _, err := database.Client.DrinkFlavour.Create().
			SetDrinkID(d.ID).SetFlavourID(fl.ID).SetIntensity(3).Save(ctx); err != nil {
			t.Fatalf("drink flavour: %v", err)
		}
	}

	water, err := database.Client.Drink.Create().
		SetName("Still Water").SetSlug("still-water").
		SetDescription("Plain spring water").
		SetBeverageType(drink.BeverageTypeWater).
		SetCaffeineMgPer100ml(0).SetSugarGPer100ml(0).
		SetPublished(true).SetStatus(drink.StatusActive).
		Save(ctx)
	if err != nil {
		t.Fatalf("water: %v", err)
	}

	return &testEnv{
		ctx: ctx, db: database,
		svc:   savedsearch.New(database, social.New(database)),
		alice: alice, bob: bob, carol: carol,
		drink: d, water: water, fanta: fanta, cola: cola, cherry: cherry, sugar: sugar,
	}
}

// hydrated reloads a drink with the edges the matcher reads.
func (e *testEnv) hydrated(t *testing.T, id int) *ent.Drink {
	t.Helper()
	d, err := e.db.Client.Drink.Query().
		Where(drink.ID(id)).
		WithBrand().WithStyle().
		WithFlavourRows(func(q *ent.DrinkFlavourQuery) { q.WithFlavour() }).
		Only(e.ctx)
	if err != nil {
		t.Fatalf("hydrate drink: %v", err)
	}
	return d
}

func TestMatches(t *testing.T) {
	e := newTestEnv(t)
	cola := e.hydrated(t, e.drink.ID)
	water := e.hydrated(t, e.water.ID)

	tests := []struct {
		name   string
		search ent.SavedSearch
		which  *ent.Drink
		want   bool
	}{
		{"no filters matches anything", ent.SavedSearch{}, cola, true},
		{"query hits the name", ent.SavedSearch{Query: "cherry"}, cola, true},
		{"query is case-insensitive", ent.SavedSearch{Query: "CHERRY"}, cola, true},
		{"query hits the brand", ent.SavedSearch{Query: "fanta"}, cola, true},
		{"query hits the description", ent.SavedSearch{Query: "spring water"}, water, true},
		{"query misses", ent.SavedSearch{Query: "grape"}, cola, false},
		{"beverage type matches", ent.SavedSearch{BeverageType: "soda"}, cola, true},
		{"beverage type differs", ent.SavedSearch{BeverageType: "water"}, cola, false},
		{"style matches", ent.SavedSearch{StyleSlug: "cola"}, cola, true},
		{"style differs", ent.SavedSearch{StyleSlug: "root-beer"}, cola, false},
		{"brand matches", ent.SavedSearch{BrandSlug: "fanta"}, cola, true},
		{"brand differs", ent.SavedSearch{BrandSlug: "coca-cola"}, cola, false},
		{"flavour matches", ent.SavedSearch{FlavourSlug: "cherry"}, cola, true},
		{"flavour differs", ent.SavedSearch{FlavourSlug: "lime"}, cola, false},
		{"caffeine-free accepts zero", ent.SavedSearch{CaffeineFree: true}, water, true},
		{"caffeine-free rejects caffeine", ent.SavedSearch{CaffeineFree: true}, cola, false},
		{"sugar-free accepts zero", ent.SavedSearch{SugarFree: true}, water, true},
		{"sugar-free rejects sugar", ent.SavedSearch{SugarFree: true}, cola, false},
		{"combination matches", ent.SavedSearch{
			BeverageType: "soda", BrandSlug: "fanta", StyleSlug: "cola", FlavourSlug: "cherry",
		}, cola, true},
		{"combination fails on one filter", ent.SavedSearch{
			BeverageType: "soda", BrandSlug: "fanta", FlavourSlug: "lime",
		}, cola, false},
		{"style filter on a drink without a style", ent.SavedSearch{StyleSlug: "cola"}, water, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			search := tt.search
			if got := savedsearch.Matches(&search, tt.which); got != tt.want {
				t.Fatalf("Matches = %v, want %v", got, tt.want)
			}
			if got := e.svc.Matches(&search, tt.which); got != tt.want {
				t.Fatalf("Service.Matches = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name string
		in   savedsearch.Input
		want error
	}{
		{"no filters", savedsearch.Input{}, savedsearch.ErrInvalid},
		{"whitespace only filters", savedsearch.Input{Query: "  ", BeverageType: " "}, savedsearch.ErrInvalid},
		{"unknown beverage type", savedsearch.Input{BeverageType: "cider"}, savedsearch.ErrInvalid},
		{"name too long", savedsearch.Input{Query: "cola", Name: strings.Repeat("x", 81)}, savedsearch.ErrInvalid},
		{"query too long", savedsearch.Input{Query: strings.Repeat("x", 121)}, savedsearch.ErrInvalid},
		{"slug with spaces", savedsearch.Input{StyleSlug: "cold brew"}, savedsearch.ErrInvalid},
		{"slug with symbols", savedsearch.Input{BrandSlug: "coca/cola"}, savedsearch.ErrInvalid},
		{"query only", savedsearch.Input{Query: "cola"}, nil},
		{"flags only", savedsearch.Input{CaffeineFree: true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			_, err := e.svc.Create(e.ctx, e.alice.ID, tt.in)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("create: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCreateDefaultsAndCap(t *testing.T) {
	e := newTestEnv(t)

	t.Run("notify defaults on and can be switched off", func(t *testing.T) {
		on, err := e.svc.Create(e.ctx, e.alice.ID, savedsearch.Input{Query: "cola"})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if !on.NotifyDrops {
			t.Fatal("notify_drops should default to true")
		}
		off := false
		quiet, err := e.svc.Create(e.ctx, e.alice.ID, savedsearch.Input{Query: "lime", NotifyDrops: &off})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if quiet.NotifyDrops {
			t.Fatal("explicit false was not kept")
		}
	})

	t.Run("blank name is derived", func(t *testing.T) {
		row, err := e.svc.Create(e.ctx, e.bob.ID, savedsearch.Input{
			BeverageType: "water", SugarFree: true,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if row.Name != "water · sugar-free" {
			t.Fatalf("name = %q", row.Name)
		}
	})

	t.Run("per-user cap", func(t *testing.T) {
		carol := e.carol
		for i := 0; i < savedsearch.MaxPerUser; i++ {
			if _, err := e.svc.Create(e.ctx, carol.ID, savedsearch.Input{
				Name: fmt.Sprintf("search %d", i), Query: fmt.Sprintf("cola %d", i),
			}); err != nil {
				t.Fatalf("create %d: %v", i, err)
			}
		}
		_, err := e.svc.Create(e.ctx, carol.ID, savedsearch.Input{Query: "one too many"})
		if !errors.Is(err, savedsearch.ErrLimit) {
			t.Fatalf("err = %v, want ErrLimit", err)
		}
	})
}

func TestListAndDeleteScoping(t *testing.T) {
	e := newTestEnv(t)
	row, err := e.svc.Create(e.ctx, e.alice.ID, savedsearch.Input{Query: "cherry"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Run("list is scoped to the owner", func(t *testing.T) {
		mine, err := e.svc.List(e.ctx, e.alice.ID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(mine) != 1 || mine[0].ID != row.ID {
			t.Fatalf("alice list = %+v", mine)
		}
		theirs, err := e.svc.List(e.ctx, e.bob.ID)
		if err != nil {
			t.Fatalf("list bob: %v", err)
		}
		if len(theirs) != 0 {
			t.Fatalf("bob list = %+v", theirs)
		}
	})

	t.Run("another member's search is not found", func(t *testing.T) {
		if err := e.svc.Delete(e.ctx, e.bob.ID, row.ID); !errors.Is(err, savedsearch.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if n, _ := e.db.Client.SavedSearch.Query().Where(entsavedsearch.IDEQ(row.ID)).Count(e.ctx); n != 1 {
			t.Fatal("foreign delete removed the row")
		}
	})

	t.Run("missing id is not found", func(t *testing.T) {
		if err := e.svc.Delete(e.ctx, e.alice.ID, "nope"); !errors.Is(err, savedsearch.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("owner delete removes the row", func(t *testing.T) {
		if err := e.svc.Delete(e.ctx, e.alice.ID, row.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		rows, err := e.svc.List(e.ctx, e.alice.ID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("list after delete = %+v", rows)
		}
	})
}

func dropAlerts(t *testing.T, e *testEnv, userID string) []*ent.Notification {
	t.Helper()
	rows, err := e.db.Client.Notification.Query().
		Where(notification.UserID(userID), notification.KindEQ(notification.KindDropAlert)).
		Order(ent.Asc(notification.FieldCreatedAt)).
		All(e.ctx)
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	return rows
}

func TestEvaluateDropAlerts(t *testing.T) {
	e := newTestEnv(t)

	// alice: two matching searches, which must produce one alert.
	for _, q := range []string{"cherry", "fanta"} {
		if _, err := e.svc.Create(e.ctx, e.alice.ID, savedsearch.Input{Query: q}); err != nil {
			t.Fatalf("alice search: %v", err)
		}
	}
	// bob: a search that does not match this drink.
	if _, err := e.svc.Create(e.ctx, e.bob.ID, savedsearch.Input{Query: "grape"}); err != nil {
		t.Fatalf("bob search: %v", err)
	}
	// carol: matching but alerts are switched off.
	off := false
	if _, err := e.svc.Create(e.ctx, e.carol.ID, savedsearch.Input{Query: "cherry", NotifyDrops: &off}); err != nil {
		t.Fatalf("carol search: %v", err)
	}

	n, err := e.svc.Evaluate(e.ctx, e.drink)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if n != 1 {
		t.Fatalf("notified = %d, want 1", n)
	}
	if got := len(dropAlerts(t, e, e.alice.ID)); got != 1 {
		t.Fatalf("alice alerts = %d, want 1 (deduped across her two searches)", got)
	}
	if got := len(dropAlerts(t, e, e.bob.ID)); got != 0 {
		t.Fatalf("bob alerts = %d, want 0", got)
	}
	if got := len(dropAlerts(t, e, e.carol.ID)); got != 0 {
		t.Fatalf("carol alerts = %d, want 0", got)
	}

	// The alert carries the drink name and points at the drink id.
	rows := dropAlerts(t, e, e.alice.ID)
	if !strings.Contains(rows[0].Text, "Cherry Cola") {
		t.Fatalf("text = %q", rows[0].Text)
	}
	if rows[0].SubjectID != fmt.Sprint(e.drink.ID) {
		t.Fatalf("subject = %q", rows[0].SubjectID)
	}

	// Alice's searches now carry the debounce stamp.
	saved, err := e.svc.List(e.ctx, e.alice.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, row := range saved {
		if row.LastNotifiedAt == nil || *row.LastNotifiedAt == "" {
			t.Fatalf("search %q was not stamped", row.Name)
		}
	}
}

func TestEvaluateDebouncesAndSkipsUnmatched(t *testing.T) {
	e := newTestEnv(t)
	if _, err := e.svc.Create(e.ctx, e.alice.ID, savedsearch.Input{Query: "cherry"}); err != nil {
		t.Fatalf("search: %v", err)
	}

	if n, err := e.svc.Evaluate(e.ctx, e.drink); err != nil || n != 1 {
		t.Fatalf("first evaluate = %d, %v", n, err)
	}

	t.Run("second run inside the window is silent", func(t *testing.T) {
		n, err := e.svc.Evaluate(e.ctx, e.drink)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if n != 0 {
			t.Fatalf("notified = %d, want 0", n)
		}
		if got := len(dropAlerts(t, e, e.alice.ID)); got != 1 {
			t.Fatalf("alerts = %d, want 1", got)
		}
	})

	t.Run("a non-matching drink notifies nobody", func(t *testing.T) {
		other, err := e.db.Client.Drink.Create().
			SetName("Grape Soda").SetSlug("grape-soda").
			SetPublished(true).SetStatus(drink.StatusActive).
			Save(e.ctx)
		if err != nil {
			t.Fatalf("drink: %v", err)
		}
		n, err := e.svc.Evaluate(e.ctx, other)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if n != 0 {
			t.Fatalf("notified = %d, want 0", n)
		}
		if got := len(dropAlerts(t, e, e.alice.ID)); got != 1 {
			t.Fatalf("alerts = %d, want 1", got)
		}
	})

	t.Run("an unpublished drink notifies nobody", func(t *testing.T) {
		hidden, err := e.db.Client.Drink.Create().
			SetName("Secret Cherry").SetSlug("secret-cherry").
			SetPublished(false).SetStatus(drink.StatusActive).
			Save(e.ctx)
		if err != nil {
			t.Fatalf("drink: %v", err)
		}
		n, err := e.svc.Evaluate(e.ctx, hidden)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if n != 0 {
			t.Fatalf("notified = %d, want 0", n)
		}
	})

	t.Run("a fresh drink after the window notifies again", func(t *testing.T) {
		old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
		if _, err := e.db.Client.SavedSearch.Update().
			Where(entsavedsearch.UserID(e.alice.ID)).
			SetLastNotifiedAt(old).
			Save(e.ctx); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		if _, err := e.db.Client.Notification.Delete().
			Where(notification.UserID(e.alice.ID)).Exec(e.ctx); err != nil {
			t.Fatalf("clear notifications: %v", err)
		}

		fresh, err := e.db.Client.Drink.Create().
			SetName("Cherry Zero").SetSlug("cherry-zero").
			SetPublished(true).SetStatus(drink.StatusLimited).
			Save(e.ctx)
		if err != nil {
			t.Fatalf("drink: %v", err)
		}
		n, err := e.svc.Evaluate(e.ctx, fresh)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if n != 1 {
			t.Fatalf("notified = %d, want 1", n)
		}
	})
}

func TestVisible(t *testing.T) {
	tests := []struct {
		name string
		row  ent.Drink
		want bool
	}{
		{"published active", ent.Drink{Published: true, Status: drink.StatusActive}, true},
		{"published limited", ent.Drink{Published: true, Status: drink.StatusLimited}, true},
		{"published seasonal", ent.Drink{Published: true, Status: drink.StatusSeasonal}, true},
		{"published discontinued", ent.Drink{Published: true, Status: drink.StatusDiscontinued}, false},
		{"published provisional", ent.Drink{Published: true, Status: drink.StatusProvisional}, false},
		{"unpublished active", ent.Drink{Published: false, Status: drink.StatusActive}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := tt.row
			if got := savedsearch.Visible(&row); got != tt.want {
				t.Fatalf("Visible = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlavours(t *testing.T) {
	e := newTestEnv(t)
	rows, err := e.svc.Flavours(e.ctx)
	if err != nil {
		t.Fatalf("flavours: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("flavours = %+v", rows)
	}
	if rows[0].Slug != "cherry" || rows[1].Slug != "sugar" {
		t.Fatalf("flavours not name-ordered: %+v", rows)
	}
}
