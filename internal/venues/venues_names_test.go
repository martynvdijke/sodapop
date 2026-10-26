package venues

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/internal/catalog"
	"sodapop/internal/db"
)

type venueHarness struct {
	svc  *Service
	db   *db.DB
	ctx  context.Context
	user *ent.User
}

func newVenueHarness(t *testing.T) *venueHarness {
	t.Helper()
	ctx := context.Background()
	database, err := db.Init(filepath.Join(t.TempDir(), "venues.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	u, err := database.Client.User.Create().
		SetHandle("submitter").
		SetEmail("submitter@example.com").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return &venueHarness{svc: New(database), db: database, ctx: ctx, user: u}
}

func (h *venueHarness) create(t *testing.T, name, city string, lat, lng float64) *ent.Venue {
	t.Helper()
	v, created, err := h.svc.Create(h.ctx, h.user.ID, CreateInput{
		Name: name, City: city, Lat: lat, Lng: lng,
	})
	if err != nil {
		t.Fatalf("create venue %q: %v", name, err)
	}
	if !created {
		t.Fatalf("create venue %q: expected created", name)
	}
	return v
}

func (h *venueHarness) venueCount(t *testing.T) int {
	t.Helper()
	n, err := h.db.Client.Venue.Query().Count(h.ctx)
	if err != nil {
		t.Fatalf("count venues: %v", err)
	}
	return n
}

func TestAddNameNormalizesDedupesAndSorts(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "The Crown", "Bristol", 0, 0)

	if err := h.svc.AddName(h.ctx, v.ID, "  The   Crown   Hotel  "); err != nil {
		t.Fatalf("add name: %v", err)
	}
	if err := h.svc.AddName(h.ctx, v.ID, "the crown hotel"); err != nil {
		t.Fatalf("add duplicate name: %v", err)
	}
	if err := h.svc.AddName(h.ctx, v.ID, "THE CROWN HOTEL"); err != nil {
		t.Fatalf("add case duplicate name: %v", err)
	}
	if err := h.svc.AddName(h.ctx, v.ID, "Crown Arms"); err != nil {
		t.Fatalf("add second name: %v", err)
	}

	names, err := h.svc.Names(h.ctx, v.ID)
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("names = %v, want 3 entries", names)
	}
	joined := strings.Join(names, "|")
	for _, expected := range []string{"The Crown", "Crown Arms", "The Crown Hotel"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("names %v missing %q", names, expected)
		}
	}
}

func TestAddNameRejectsInvalidNamesAndMergedVenues(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "The Crown", "Bristol", 0, 0)

	for _, name := range []string{"", "   ", "\t\n", strings.Repeat("x", 121)} {
		if err := h.svc.AddName(h.ctx, v.ID, name); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddName(%q) error = %v, want ErrInvalid", name, err)
		}
	}
	if err := h.svc.AddName(h.ctx, 99999, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("AddName(missing) error = %v, want ErrNotFound", err)
	}

	target := h.create(t, "The Red Lion", "Bristol", 0, 0)
	if err := h.svc.Merge(h.ctx, v.ID, target.ID, h.user.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := h.svc.AddName(h.ctx, v.ID, "Old Name"); !errors.Is(err, ErrNotFound) {
		t.Errorf("AddName(merged venue) error = %v, want ErrNotFound", err)
	}
}

func TestCreateWritesCanonicalAliasTransactionally(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "  The   Crown  ", "Bristol", 0, 0)

	if v.Name != "The Crown" {
		t.Fatalf("venue name = %q, want normalized", v.Name)
	}
	names, err := h.svc.Names(h.ctx, v.ID)
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	if len(names) != 1 || names[0] != "The Crown" {
		t.Fatalf("names = %v, want [The Crown]", names)
	}
}

func TestCreateAlternateNameDedupesToExistingVenue(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "The Crown Hotel", "Bristol", 0, 0)
	if err := h.svc.AddName(h.ctx, v.ID, "The Crown"); err != nil {
		t.Fatalf("add alias: %v", err)
	}

	got, created, err := h.svc.Create(h.ctx, h.user.ID, CreateInput{Name: "The Crown", City: "Bristol"})
	if err != nil {
		t.Fatalf("create alternate: %v", err)
	}
	if created || got.ID != v.ID {
		t.Fatalf("alternate name created new venue: created=%v id=%d want id=%d", created, got.ID, v.ID)
	}
	if n := h.venueCount(t); n != 1 {
		t.Fatalf("venue count = %d, want 1", n)
	}
}

func TestCreateProximityDedupeAddsSubmittedName(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "The Crown", "Bristol", 51.4545, -2.5879)

	got, created, err := h.svc.Create(h.ctx, h.user.ID, CreateInput{
		Name: "Crown Tavern", City: "Bristol", Lat: 51.4546, Lng: -2.5878,
	})
	if err != nil {
		t.Fatalf("create nearby: %v", err)
	}
	if created || got.ID != v.ID {
		t.Fatalf("nearby name created new venue: created=%v id=%d want id=%d", created, got.ID, v.ID)
	}
	if n := h.venueCount(t); n != 1 {
		t.Fatalf("venue count = %d, want 1", n)
	}
	names, err := h.svc.Names(h.ctx, v.ID)
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	if !containsFold(names, "Crown Tavern") {
		t.Fatalf("names = %v, want submitted alternate name", names)
	}
}

func TestSearchMatchesAliasesWithoutDuplicates(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "The Crown", "Bristol", 0, 0)
	if err := h.svc.AddName(h.ctx, v.ID, "Crown Hotel"); err != nil {
		t.Fatalf("add alias: %v", err)
	}

	results, err := h.svc.Search(h.ctx, "Crown Hotel", 10)
	if err != nil {
		t.Fatalf("search alias: %v", err)
	}
	if len(results) != 1 || results[0].ID != v.ID {
		t.Fatalf("alias search = %+v, want one result id=%d", results, v.ID)
	}

	// Same query matches the canonical name and the canonical alias row.
	results, err = h.svc.Search(h.ctx, "crown", 10)
	if err != nil {
		t.Fatalf("search canonical: %v", err)
	}
	if len(results) != 1 || results[0].ID != v.ID {
		t.Fatalf("canonical search = %+v, want one result id=%d", results, v.ID)
	}

	if _, err := h.svc.Search(h.ctx, "c", 10); !errors.Is(err, catalog.ErrQueryTooShort) {
		t.Fatalf("short query error = %v, want ErrQueryTooShort", err)
	}
}

func TestSearchExcludesMergedVenues(t *testing.T) {
	h := newVenueHarness(t)
	from := h.create(t, "Old Crown", "Bristol", 0, 0)
	into := h.create(t, "New Crown", "Bristol", 0, 0)

	if err := h.svc.Merge(h.ctx, from.ID, into.ID, h.user.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	results, err := h.svc.Search(h.ctx, "Old Crown", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != into.ID {
		t.Fatalf("search after merge = %+v, want only target id=%d", results, into.ID)
	}
}

func TestMergeMovesNamesIsIdempotentAndRejectsCycles(t *testing.T) {
	h := newVenueHarness(t)
	from := h.create(t, "Old Crown", "Bristol", 0, 0)
	into := h.create(t, "New Crown", "Bristol", 0, 0)
	if err := h.svc.AddName(h.ctx, from.ID, "Crown Tavern"); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	if err := h.svc.AddName(h.ctx, into.ID, "crown tavern"); err != nil {
		t.Fatalf("add duplicate alias: %v", err)
	}

	if err := h.svc.Merge(h.ctx, from.ID, into.ID, h.user.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	merged, err := h.db.Client.Venue.Get(h.ctx, from.ID)
	if err != nil {
		t.Fatalf("get merged: %v", err)
	}
	if merged.MergedIntoID == nil || *merged.MergedIntoID != into.ID {
		t.Fatalf("merged_into_id = %v, want %d", merged.MergedIntoID, into.ID)
	}
	names, err := h.svc.Names(h.ctx, into.ID)
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	for _, want := range []string{"New Crown", "Old Crown", "Crown Tavern"} {
		if !containsFold(names, want) {
			t.Errorf("names = %v, missing %q", names, want)
		}
	}
	if countFold(names, "Crown Tavern") != 1 {
		t.Errorf("names = %v, duplicate alias after merge", names)
	}

	if err := h.svc.Merge(h.ctx, from.ID, into.ID, h.user.ID); err != nil {
		t.Fatalf("idempotent merge: %v", err)
	}
	if err := h.svc.Merge(h.ctx, from.ID, from.ID, h.user.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("self merge error = %v, want ErrInvalid", err)
	}
	if err := h.svc.Merge(h.ctx, into.ID, from.ID, h.user.ID); err == nil {
		t.Error("merge into a merged venue succeeded, want error")
	}
}

func containsFold(names []string, want string) bool {
	return countFold(names, want) > 0
}

func countFold(names []string, want string) int {
	n := 0
	for _, name := range names {
		if strings.EqualFold(name, want) {
			n++
		}
	}
	return n
}
