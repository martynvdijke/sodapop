package mix

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/db"
)

// newTestDB opens a fresh SQLite database in a temp dir (unique DSN per test)
// with the Ent schema and migrations applied.
func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "mix.db"))
	if err != nil {
		t.Fatalf("init database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func seedUser(t *testing.T, database *db.DB, handle string) *ent.User {
	t.Helper()
	u, err := database.Client.User.Create().
		SetHandle(handle).
		SetEmail(handle + "@example.com").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func seedDrink(t *testing.T, database *db.DB, name, slug string, volume int) int {
	t.Helper()
	d, err := database.Client.Drink.Create().
		SetName(name).
		SetSlug(slug).
		SetVolumeMl(volume).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create drink %s: %v", name, err)
	}
	return d.ID
}

func TestCreateAndGet(t *testing.T) {
	database := newTestDB(t)
	svc := New(database)
	ctx := context.Background()

	u := seedUser(t, database, "mixer")
	cola := seedDrink(t, database, "Cola", "cola", 330)
	tonic := seedDrink(t, database, "Tonic", "tonic", 200)

	created, err := svc.Create(ctx, Input{
		UserID:     u.ID,
		Name:       "Cola Tonic",
		Notes:      "Bright and bitter",
		Visibility: "public",
		Components: []Component{
			{DrinkID: cola, Parts: 3},
			{DrinkID: tonic, Parts: 1},
		},
	})
	if err != nil {
		t.Fatalf("create mix: %v", err)
	}

	if !strings.HasPrefix(created.Slug, "mix-cola-tonic-") {
		t.Errorf("slug = %q, want prefix %q", created.Slug, "mix-cola-tonic-")
	}
	if created.Visibility != "public" {
		t.Errorf("visibility = %q, want public", created.Visibility)
	}
	if len(created.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(created.Components))
	}

	d, err := database.Client.Drink.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get drink row: %v", err)
	}
	if got := string(d.Kind); got != "mix" {
		t.Errorf("kind = %q, want mix", got)
	}
	if got := string(d.Source); got != "user" {
		t.Errorf("source = %q, want user", got)
	}
	if got := string(d.Status); got != "active" {
		t.Errorf("status = %q, want active", got)
	}
	// (330*3 + 200*1) / 4 = 297.5 -> 297
	if d.VolumeMl != 297 {
		t.Errorf("volume_ml = %d, want 297", d.VolumeMl)
	}

	fetched, err := svc.Get(ctx, created.Slug)
	if err != nil {
		t.Fatalf("get mix: %v", err)
	}
	if fetched.Name != "Cola Tonic" || fetched.Notes != "Bright and bitter" {
		t.Errorf("fetched = %+v", fetched)
	}
	if len(fetched.Components) != 2 {
		t.Fatalf("fetched components = %d, want 2", len(fetched.Components))
	}
	parts := map[int]int{}
	names := map[int]string{}
	for _, c := range fetched.Components {
		parts[c.DrinkID] = c.Parts
		names[c.DrinkID] = c.Name
	}
	if parts[cola] != 3 || parts[tonic] != 1 {
		t.Errorf("parts = %v, want cola:3 tonic:1", parts)
	}
	if names[cola] != "Cola" || names[tonic] != "Tonic" {
		t.Errorf("component names = %v", names)
	}

	// Mine and List both surface the mix for its owner.
	mine, err := svc.Mine(ctx, u.ID)
	if err != nil {
		t.Fatalf("mine: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != created.ID {
		t.Errorf("mine = %+v, want the created mix", mine)
	}
	list, err := svc.List(ctx, "", 30)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list = %+v, want the public mix", list)
	}
}

func TestCreateDefaultsVisibility(t *testing.T) {
	database := newTestDB(t)
	svc := New(database)
	ctx := context.Background()

	u := seedUser(t, database, "privateer")
	if _, err := database.Client.User.UpdateOneID(u.ID).
		SetDefaultVisibility(user.DefaultVisibilityPrivate).
		Save(ctx); err != nil {
		t.Fatalf("set default visibility: %v", err)
	}
	a := seedDrink(t, database, "Soda A", "soda-a", 355)
	b := seedDrink(t, database, "Soda B", "soda-b", 355)

	created, err := svc.Create(ctx, Input{
		UserID: u.ID,
		Name:   "Secret Mix",
		Components: []Component{
			{DrinkID: a, Parts: 1},
			{DrinkID: b, Parts: 1},
		},
	})
	if err != nil {
		t.Fatalf("create mix: %v", err)
	}
	if created.Visibility != "private" {
		t.Errorf("visibility = %q, want private from user default", created.Visibility)
	}

	// Private mixes stay hidden from anonymous reads and public listings.
	if _, err := svc.Get(ctx, created.Slug); err != ErrNotFound {
		t.Errorf("anonymous get err = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetForViewer(ctx, created.Slug, u.ID); err != nil {
		t.Errorf("owner get: %v", err)
	}
	list, err := svc.List(ctx, "", 30)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("public list = %d, want 0", len(list))
	}
}

func TestCreateRejectsInvalidRecipes(t *testing.T) {
	database := newTestDB(t)
	svc := New(database)
	ctx := context.Background()

	u := seedUser(t, database, "validator")
	a := seedDrink(t, database, "One", "one", 355)
	b := seedDrink(t, database, "Two", "two", 355)

	tests := []struct {
		name string
		in   Input
	}{
		{
			name: "one component",
			in:   Input{UserID: u.ID, Name: "Lonely", Components: []Component{{DrinkID: a, Parts: 1}}},
		},
		{
			name: "duplicate components",
			in: Input{UserID: u.ID, Name: "Twice", Components: []Component{
				{DrinkID: a, Parts: 1}, {DrinkID: a, Parts: 2},
			}},
		},
		{
			name: "parts out of range",
			in: Input{UserID: u.ID, Name: "Strong", Components: []Component{
				{DrinkID: a, Parts: 11}, {DrinkID: b, Parts: 1},
			}},
		},
		{
			name: "unknown drink",
			in: Input{UserID: u.ID, Name: "Ghost", Components: []Component{
				{DrinkID: a, Parts: 1}, {DrinkID: 99999, Parts: 1},
			}},
		},
		{
			name: "short name",
			in: Input{UserID: u.ID, Name: "x", Components: []Component{
				{DrinkID: a, Parts: 1}, {DrinkID: b, Parts: 1},
			}},
		},
		{
			name: "bad visibility",
			in: Input{UserID: u.ID, Name: "Shady", Visibility: "world", Components: []Component{
				{DrinkID: a, Parts: 1}, {DrinkID: b, Parts: 1},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(ctx, tc.in); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	n, err := database.Client.Drink.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count drinks: %v", err)
	}
	if n != 2 {
		t.Errorf("drink rows = %d, want 2 (no mix written)", n)
	}
}

func TestParseRecipeTolerant(t *testing.T) {
	for _, raw := range []string{"", "{}", "null", "not json", `{"components":"nope"}`, `[1,2,3]`} {
		r := parseRecipe(raw)
		if r.Components != nil {
			t.Errorf("parseRecipe(%q) components = %v, want nil", raw, r.Components)
		}
		if r.Visibility != "public" {
			t.Errorf("parseRecipe(%q) visibility = %q, want public", raw, r.Visibility)
		}
	}
}
