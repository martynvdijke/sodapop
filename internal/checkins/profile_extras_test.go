package checkins_test

import (
	"testing"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
)

func (e *testEnv) createStyle(t *testing.T, name, slug string) *ent.Style {
	t.Helper()
	st, err := e.db.Client.Style.Create().SetName(name).SetSlug(slug).Save(e.ctx)
	if err != nil {
		t.Fatalf("style %s: %v", name, err)
	}
	return st
}

func (e *testEnv) createStyledDrink(t *testing.T, name, slug string, style *ent.Style) *ent.Drink {
	t.Helper()
	d, err := e.db.Client.Drink.Create().
		SetName(name).SetSlug(slug).SetStyleID(style.ID).Save(e.ctx)
	if err != nil {
		t.Fatalf("drink %s: %v", name, err)
	}
	return d
}

func (e *testEnv) createStyleCheckin(t *testing.T, owner *ent.User, style *ent.Style, visibility string) *ent.Checkin {
	t.Helper()
	drink := e.createStyledDrink(t, style.Name+" Soda", style.Slug+"-soda", style)
	return e.createDrinkCheckin(t, owner, drink, visibility)
}

func (e *testEnv) createDrinkCheckin(t *testing.T, owner *ent.User, drink *ent.Drink, visibility string) *ent.Checkin {
	t.Helper()
	c, _, err := e.svc.Create(e.ctx, owner, checkins.CreateInput{
		DrinkID: drink.ID, Visibility: visibility,
	})
	if err != nil {
		t.Fatalf("checkin %s/%s: %v", drink.Slug, visibility, err)
	}
	return c
}

func styleCounts(items []checkins.TopStyle) map[string]int {
	out := make(map[string]int, len(items))
	for _, it := range items {
		out[it.Slug] = it.Count
	}
	return out
}

func TestProfileTopStylesAccess(t *testing.T) {
	e := newTestEnv(t)
	e.befriend(t, e.alice, e.bob)

	cola := e.createStyle(t, "Cola", "cola")
	lemonade := e.createStyle(t, "Lemonade", "lemonade")
	rootBeer := e.createStyle(t, "Root Beer", "root-beer")

	// Two public colas so count ordering is observable.
	colaDrink := e.createStyledDrink(t, "Cola Soda", "cola-soda", cola)
	e.createDrinkCheckin(t, e.alice, colaDrink, "public")
	e.createDrinkCheckin(t, e.alice, colaDrink, "public")
	e.createStyleCheckin(t, e.alice, lemonade, "friends")
	e.createStyleCheckin(t, e.alice, rootBeer, "private")
	// A drink with no style must never appear.
	if _, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
		DrinkID: e.drink.ID, Visibility: "public",
	}); err != nil {
		t.Fatalf("unstyled checkin: %v", err)
	}

	cases := []struct {
		name   string
		viewer string
		want   map[string]int
	}{
		{"owner", e.alice.ID, map[string]int{"cola": 2, "lemonade": 1, "root-beer": 1}},
		{"friend", e.bob.ID, map[string]int{"cola": 2, "lemonade": 1}},
		{"stranger", e.carol.ID, map[string]int{"cola": 2}},
		{"anonymous", "", map[string]int{"cola": 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.svc.ProfileTopStyles(e.ctx, tc.viewer, e.alice.ID, 3)
			if err != nil {
				t.Fatalf("top styles: %v", err)
			}
			if got == nil {
				t.Fatal("top styles = nil, want non-nil slice")
			}
			counts := styleCounts(got)
			if len(counts) != len(tc.want) {
				t.Fatalf("styles = %+v, want %+v", got, tc.want)
			}
			for slug, want := range tc.want {
				if counts[slug] != want {
					t.Fatalf("style %s count = %d, want %d (all: %+v)", slug, counts[slug], want, got)
				}
			}
		})
	}

	t.Run("order count desc then name asc", func(t *testing.T) {
		got, err := e.svc.ProfileTopStyles(e.ctx, e.alice.ID, e.alice.ID, 3)
		if err != nil {
			t.Fatalf("top styles: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("styles = %+v, want 3", got)
		}
		if got[0].Slug != "cola" {
			t.Fatalf("first = %+v, want cola (highest count)", got[0])
		}
		if got[1].Name != "Lemonade" || got[2].Name != "Root Beer" {
			t.Fatalf("tie order = %+v, want Lemonade before Root Beer", got)
		}
	})
}

func TestProfileTopStylesLimit(t *testing.T) {
	e := newTestEnv(t)
	for _, def := range []struct{ name, slug string }{
		{"Alpha", "alpha"}, {"Bravo", "bravo"}, {"Charlie", "charlie"},
		{"Delta", "delta"}, {"Echo", "echo"}, {"Foxtrot", "foxtrot"},
	} {
		st := e.createStyle(t, def.name, def.slug)
		e.createStyleCheckin(t, e.alice, st, "public")
	}

	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero defaults to 3", 0, 3},
		{"negative defaults to 3", -4, 3},
		{"explicit 2", 2, 2},
		{"above cap clamps to 5", 99, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.svc.ProfileTopStyles(e.ctx, "", e.alice.ID, tc.limit)
			if err != nil {
				t.Fatalf("top styles: %v", err)
			}
			if len(got) != tc.want {
				t.Fatalf("len = %d, want %d (%+v)", len(got), tc.want, got)
			}
			// Equal counts order by name, so the prefix is deterministic.
			for i := 1; i < len(got); i++ {
				if got[i-1].Name >= got[i].Name {
					t.Fatalf("order = %+v, want name ASC on ties", got)
				}
			}
		})
	}

	t.Run("empty target", func(t *testing.T) {
		got, err := e.svc.ProfileTopStyles(e.ctx, "", "", 3)
		if err != nil {
			t.Fatalf("top styles: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("styles = %+v, want non-nil empty", got)
		}
	})
}

func TestProfileTopStylesExcludesDeleted(t *testing.T) {
	t.Run("deleted check-in", func(t *testing.T) {
		e := newTestEnv(t)
		cola := e.createStyle(t, "Cola", "cola")
		lemonade := e.createStyle(t, "Lemonade", "lemonade")
		gone := e.createStyleCheckin(t, e.alice, cola, "public")
		e.createStyleCheckin(t, e.alice, lemonade, "public")
		if err := e.svc.Delete(e.ctx, e.alice.ID, gone.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		got, err := e.svc.ProfileTopStyles(e.ctx, "", e.alice.ID, 3)
		if err != nil {
			t.Fatalf("top styles: %v", err)
		}
		if len(got) != 1 || got[0].Slug != "lemonade" {
			t.Fatalf("styles = %+v, want only lemonade", got)
		}
	})

	t.Run("deleted user", func(t *testing.T) {
		e := newTestEnv(t)
		st := e.createStyle(t, "Cola", "cola")
		e.createStyleCheckin(t, e.alice, st, "public")
		if err := e.db.Client.User.UpdateOneID(e.alice.ID).
			SetStatus(user.StatusDeleted).Exec(e.ctx); err != nil {
			t.Fatalf("delete user: %v", err)
		}
		got, err := e.svc.ProfileTopStyles(e.ctx, "", e.alice.ID, 3)
		if err != nil {
			t.Fatalf("top styles: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("styles = %+v, want non-nil empty", got)
		}
	})

	t.Run("soft-deleted user", func(t *testing.T) {
		e := newTestEnv(t)
		st := e.createStyle(t, "Cola", "cola")
		e.createStyleCheckin(t, e.alice, st, "public")
		if err := e.db.Client.User.UpdateOneID(e.alice.ID).
			SetDeletedAt("2026-01-01T00:00:00Z").Exec(e.ctx); err != nil {
			t.Fatalf("soft delete user: %v", err)
		}
		got, err := e.svc.ProfileTopStyles(e.ctx, "", e.alice.ID, 3)
		if err != nil {
			t.Fatalf("top styles: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("styles = %+v, want non-nil empty", got)
		}
	})
}
