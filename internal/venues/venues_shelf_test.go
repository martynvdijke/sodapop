package venues

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/drink"
)

func (h *venueHarness) addUser(t *testing.T, handle string) *ent.User {
	t.Helper()
	u, err := h.db.Client.User.Create().
		SetHandle(handle).
		SetEmail(handle + "@example.com").
		Save(h.ctx)
	if err != nil {
		t.Fatalf("create user %q: %v", handle, err)
	}
	return u
}

func (h *venueHarness) addCheckin(t *testing.T, userID string, venueID, drinkID int, at time.Time) {
	t.Helper()
	if _, err := h.db.Client.Checkin.Create().
		SetUserID(userID).
		SetDrinkID(drinkID).
		SetVenueID(venueID).
		SetCreatedAt(at.UTC().Format(time.RFC3339)).
		Save(h.ctx); err != nil {
		t.Fatalf("create checkin: %v", err)
	}
}

func shelfByDrink(entries []ShelfEntry, drinkID int) *ShelfEntry {
	for i := range entries {
		if entries[i].DrinkID == drinkID {
			return &entries[i]
		}
	}
	return nil
}

func TestShelfReportAppearsFreshAndMine(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Shelf Bar", "Bristol", 0, 0)
	drinkID := h.addDrink(t, "Cherry Cola", "cherry-cola", drink.StatusActive)

	price := 150
	entry, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, ShelfReportInput{
		DrinkSlug:  "cherry-cola",
		PriceCents: &price,
		Note:       "Bottom shelf, cold",
		SeenNow:    true,
	})
	if err != nil {
		t.Fatalf("ReportShelf: %v", err)
	}
	if entry == nil {
		t.Fatal("ReportShelf returned nil entry")
	}
	if entry.Band != "fresh" || entry.Source != "report" || !entry.Mine {
		t.Fatalf("entry = %+v, want fresh/report/mine", entry)
	}
	if entry.ReportID == nil || *entry.ReportID == "" {
		t.Fatalf("report id = %v, want set", entry.ReportID)
	}
	if entry.PriceCents == nil || *entry.PriceCents != 150 {
		t.Fatalf("price = %v, want 150", entry.PriceCents)
	}
	if entry.Reporters != 1 {
		t.Fatalf("reporters = %d, want 1", entry.Reporters)
	}
	if entry.DrinkID != drinkID {
		t.Fatalf("drink id = %d, want %d", entry.DrinkID, drinkID)
	}

	entries, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Shelf: %v", err)
	}
	if len(entries) != 1 || entries[0].Band != "fresh" {
		t.Fatalf("entries = %+v, want one fresh", entries)
	}
}

func TestShelfIncludesRecentCheckin(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Checkin Cafe", "Bristol", 0, 0)
	drinkID := h.addDrink(t, "Lemon Fizz", "lemon-fizz", drink.StatusActive)
	h.addCheckin(t, h.user.ID, v.ID, drinkID, time.Now().Add(-2*24*time.Hour))

	entries, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Shelf: %v", err)
	}
	got := shelfByDrink(entries, drinkID)
	if got == nil {
		t.Fatal("check-in drink missing from shelf")
	}
	if got.Source != "checkin" {
		t.Fatalf("source = %q, want checkin", got.Source)
	}
	if got.Reporters != 1 {
		t.Fatalf("reporters = %d, want 1", got.Reporters)
	}
	if got.Mine || got.ReportID != nil {
		t.Fatalf("checkin-only row should not be mine: %+v", got)
	}
}

func TestShelfMergesReportAndCheckinFromDifferentUsers(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Merged Corner", "Bristol", 0, 0)
	other := h.addUser(t, "other")
	drinkID := h.addDrink(t, "Ginger Beer", "ginger-beer", drink.StatusActive)

	h.addCheckin(t, other.ID, v.ID, drinkID, time.Now().Add(-1*24*time.Hour))
	if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, ShelfReportInput{
		DrinkSlug: "ginger-beer", SeenNow: true,
	}); err != nil {
		t.Fatalf("ReportShelf: %v", err)
	}

	entries, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Shelf: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1 merged row", entries)
	}
	got := entries[0]
	if got.Source != "both" {
		t.Fatalf("source = %q, want both", got.Source)
	}
	if got.Reporters != 2 {
		t.Fatalf("reporters = %d, want 2", got.Reporters)
	}
}

func TestShelfExcludesOldCheckin(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Old News", "Bristol", 0, 0)
	drinkID := h.addDrink(t, "Root Beer", "root-beer", drink.StatusActive)
	h.addCheckin(t, h.user.ID, v.ID, drinkID, time.Now().Add(-45*24*time.Hour))

	entries, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Shelf: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want none for a 45-day-old check-in", entries)
	}
}

func TestShelfBandBoundaries(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Band Hall", "Bristol", 0, 0)
	seven := h.addDrink(t, "Seven Day", "seven-day", drink.StatusActive)
	eight := h.addDrink(t, "Eight Day", "eight-day", drink.StatusActive)

	now := time.Now().UTC().Truncate(time.Second)
	h.addCheckin(t, h.user.ID, v.ID, seven, now.Add(-7*24*time.Hour))
	h.addCheckin(t, h.user.ID, v.ID, eight, now.Add(-8*24*time.Hour))

	entries, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, now)
	if err != nil {
		t.Fatalf("Shelf: %v", err)
	}
	fresh := shelfByDrink(entries, seven)
	stale := shelfByDrink(entries, eight)
	if fresh == nil || fresh.Band != "fresh" || fresh.AgeDays != 7 {
		t.Fatalf("7d entry = %+v, want fresh age 7", fresh)
	}
	if stale == nil || stale.Band != "stale" || stale.AgeDays != 8 {
		t.Fatalf("8d entry = %+v, want stale age 8", stale)
	}
}

func TestShelfPriceAndNoteComeFromNewestReport(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Price Point", "Bristol", 0, 0)
	other := h.addUser(t, "other")
	h.addDrink(t, "Yuzu Soda", "yuzu-soda", drink.StatusActive)

	oldPrice, newPrice := 100, 175
	now := time.Now().UTC()
	if _, err := h.svc.ReportShelf(h.ctx, other.ID, v.ID, ShelfReportInput{
		DrinkSlug: "yuzu-soda", PriceCents: &oldPrice, Note: "old note",
		SeenAt: now.Add(-10 * 24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("old report: %v", err)
	}
	if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, ShelfReportInput{
		DrinkSlug: "yuzu-soda", PriceCents: &newPrice, Note: "new note",
		SeenAt: now.Add(-2 * 24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("new report: %v", err)
	}

	entries, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, now)
	if err != nil {
		t.Fatalf("Shelf: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	if entries[0].PriceCents == nil || *entries[0].PriceCents != 175 {
		t.Fatalf("price = %v, want 175", entries[0].PriceCents)
	}
	if entries[0].Note != "new note" {
		t.Fatalf("note = %q, want new note", entries[0].Note)
	}
}

func TestReportShelfValidation(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Validation Bar", "Bristol", 0, 0)
	h.addDrink(t, "Cola", "cola", drink.StatusActive)

	longNote := strings.Repeat("a", 141)
	tests := []struct {
		name string
		in   ShelfReportInput
		want error
	}{
		{name: "unknown drink", in: ShelfReportInput{DrinkSlug: "nope", SeenNow: true}, want: ErrDrinkNotFound},
		{name: "long note", in: ShelfReportInput{DrinkSlug: "cola", Note: longNote, SeenNow: true}, want: ErrInvalid},
		{name: "future seen", in: ShelfReportInput{DrinkSlug: "cola", SeenAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, want: ErrSeenAtFuture},
		{name: "too old seen", in: ShelfReportInput{DrinkSlug: "cola", SeenAt: time.Now().Add(-31 * 24 * time.Hour).UTC().Format(time.RFC3339)}, want: ErrSeenAtTooOld},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	negative := -1
	if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, ShelfReportInput{
		DrinkSlug: "cola", PriceCents: &negative, SeenNow: true,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative price err = %v, want ErrInvalid", err)
	}
}

func TestReportShelfUpsertsOneRow(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Upsert Bar", "Bristol", 0, 0)
	h.addDrink(t, "Cream Soda", "cream-soda", drink.StatusActive)

	first, second := 120, 220
	if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, ShelfReportInput{
		DrinkSlug: "cream-soda", PriceCents: &first, SeenNow: true,
	}); err != nil {
		t.Fatalf("first report: %v", err)
	}
	if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, ShelfReportInput{
		DrinkSlug: "cream-soda", PriceCents: &second, Note: "restocked", SeenNow: true,
	}); err != nil {
		t.Fatalf("second report: %v", err)
	}

	rows, err := h.db.Client.VenueStockReport.Query().All(h.ctx)
	if err != nil {
		t.Fatalf("query reports: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("report rows = %d, want 1", len(rows))
	}
	entries, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Shelf: %v", err)
	}
	if len(entries) != 1 || entries[0].PriceCents == nil || *entries[0].PriceCents != 220 {
		t.Fatalf("entries = %+v, want one at 220", entries)
	}
	if entries[0].Note != "restocked" {
		t.Fatalf("note = %q, want restocked", entries[0].Note)
	}
}

func TestRetractShelfHidesOwnReport(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Retract Bar", "Bristol", 0, 0)
	h.addDrink(t, "Birch Beer", "birch-beer", drink.StatusActive)

	entry, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, ShelfReportInput{
		DrinkSlug: "birch-beer", SeenNow: true,
	})
	if err != nil || entry == nil || entry.ReportID == nil {
		t.Fatalf("ReportShelf = %+v, %v", entry, err)
	}

	retracted, err := h.svc.RetractShelf(h.ctx, h.user.ID, *entry.ReportID)
	if err != nil {
		t.Fatalf("RetractShelf: %v", err)
	}
	if !retracted {
		t.Fatal("first retract = false, want true")
	}
	entries, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Shelf after retract: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want hidden", entries)
	}

	retracted, err = h.svc.RetractShelf(h.ctx, h.user.ID, *entry.ReportID)
	if err != nil {
		t.Fatalf("second RetractShelf: %v", err)
	}
	if retracted {
		t.Fatal("second retract = true, want false")
	}
}

func TestShelfOrdering(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Order Bar", "Bristol", 0, 0)
	u2 := h.addUser(t, "u2")
	u3 := h.addUser(t, "u3")
	freshOne := h.addDrink(t, "Alpha Fresh", "alpha-fresh", drink.StatusActive)
	staleMany := h.addDrink(t, "Beta Stale", "beta-stale", drink.StatusActive)
	staleFew := h.addDrink(t, "Gamma Stale", "gamma-stale", drink.StatusActive)

	now := time.Now().UTC()
	h.addCheckin(t, h.user.ID, v.ID, freshOne, now.Add(-1*24*time.Hour))
	for _, uid := range []string{h.user.ID, u2.ID, u3.ID} {
		h.addCheckin(t, uid, v.ID, staleMany, now.Add(-10*24*time.Hour))
	}
	h.addCheckin(t, h.user.ID, v.ID, staleFew, now.Add(-10*24*time.Hour))

	entries, err := h.svc.Shelf(h.ctx, v.ID, h.user.ID, now)
	if err != nil {
		t.Fatalf("Shelf: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want 3", entries)
	}
	want := []int{freshOne, staleMany, staleFew}
	for i, id := range want {
		if entries[i].DrinkID != id {
			t.Fatalf("order = %v, want %v", fmt.Sprint(entries), want)
		}
	}
	if entries[1].Reporters != 3 || entries[2].Reporters != 1 {
		t.Fatalf("reporters = %d,%d want 3,1", entries[1].Reporters, entries[2].Reporters)
	}
}

func TestNearbyShelfReturnsFreshVenuesByDistance(t *testing.T) {
	h := newVenueHarness(t)
	near := h.create(t, "Near Bar", "Bristol", 51.4545, -2.5879)
	far := h.create(t, "Far Bar", "Bristol", 51.4700, -2.5879)
	stale := h.create(t, "Stale Bar", "Bristol", 51.4560, -2.5800)
	drinkID := h.addDrink(t, "Near Soda", "near-soda", drink.StatusActive)

	if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, near.ID, ShelfReportInput{
		DrinkSlug: "near-soda", SeenNow: true,
	}); err != nil {
		t.Fatalf("report near: %v", err)
	}
	if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, far.ID, ShelfReportInput{
		DrinkSlug: "near-soda", SeenNow: true,
	}); err != nil {
		t.Fatalf("report far: %v", err)
	}
	h.addCheckin(t, h.user.ID, stale.ID, drinkID, time.Now().Add(-20*24*time.Hour))

	results, err := h.svc.NearbyShelf(h.ctx, h.user.ID, 51.4545, -2.5879, 5, 10)
	if err != nil {
		t.Fatalf("NearbyShelf: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2 fresh venues", results)
	}
	if results[0].Venue.ID != near.ID || results[1].Venue.ID != far.ID {
		t.Fatalf("order = %d,%d want %d,%d", results[0].Venue.ID, results[1].Venue.ID, near.ID, far.ID)
	}
	if results[0].DistanceM%100 != 0 {
		t.Fatalf("distance = %d, want a 100 m bucket", results[0].DistanceM)
	}
	for _, r := range results {
		for _, e := range r.Entries {
			if e.Band != "fresh" {
				t.Fatalf("nearby entry band = %q, want fresh", e.Band)
			}
		}
	}

	empty, err := h.svc.NearbyShelf(h.ctx, h.user.ID, 0, 0, 1, 10)
	if err != nil {
		t.Fatalf("NearbyShelf empty: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, want non-nil empty slice", empty)
	}
}
