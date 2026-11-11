package checkins_test

import (
	"context"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/internal/badges"
)

// recapDrinkFor inserts a drink with an optional brand and style.
func recapDrinkFor(t *testing.T, ctx context.Context, client *ent.Client,
	slug, name string, brandID, styleID *int) *ent.Drink {
	t.Helper()
	b := client.Drink.Create().SetName(name).SetSlug(slug)
	if brandID != nil {
		b.SetBrandID(*brandID)
	}
	if styleID != nil {
		b.SetStyleID(*styleID)
	}
	d, err := b.Save(ctx)
	if err != nil {
		t.Fatalf("drink %s: %v", slug, err)
	}
	return d
}

// recapCheckin writes a check-in at a pinned UTC timestamp with an optional
// venue and rating, bypassing Create so tests control created_at.
func recapCheckin(t *testing.T, ctx context.Context, client *ent.Client, owner *ent.User,
	d *ent.Drink, venueID *int, rating *int, at time.Time) *ent.Checkin {
	t.Helper()
	b := client.Checkin.Create().
		SetUserID(owner.ID).
		SetDrinkID(d.ID).
		SetCreatedAt(at.UTC().Format(time.RFC3339))
	if venueID != nil {
		b.SetVenueID(*venueID)
	}
	if rating != nil {
		b.SetRating(*rating)
	}
	c, err := b.Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func intp(v int) *int { return &v }

func recapStyleFor(t *testing.T, ctx context.Context, client *ent.Client, name, slug string) *ent.Style {
	t.Helper()
	s, err := client.Style.Create().SetName(name).SetSlug(slug).Save(ctx)
	if err != nil {
		t.Fatalf("style %s: %v", slug, err)
	}
	return s
}

func recapBrandFor(t *testing.T, ctx context.Context, client *ent.Client, name, slug string) *ent.Brand {
	t.Helper()
	b, err := client.Brand.Create().SetName(name).SetSlug(slug).Save(ctx)
	if err != nil {
		t.Fatalf("brand %s: %v", slug, err)
	}
	return b
}

func TestRecapEmptyYear(t *testing.T) {
	e := newTestEnv(t)

	got, err := e.svc.Recap(e.ctx, e.alice.ID, 2026)
	if err != nil {
		t.Fatalf("recap: %v", err)
	}
	if got.HasData {
		t.Fatalf("has_data = true for empty year")
	}
	if got.Sparse {
		t.Fatalf("sparse = true for empty year")
	}
	if got.Year != 2026 {
		t.Fatalf("year = %d, want 2026", got.Year)
	}
	if got.Totals.Checkins != 0 || got.Totals.AvgRating != nil {
		t.Fatalf("unexpected totals: %+v", got.Totals)
	}
	if len(got.RatingBuckets) != 5 {
		t.Fatalf("rating buckets = %d, want 5", len(got.RatingBuckets))
	}
	for i, b := range got.RatingBuckets {
		if b.Count != 0 || b.Label != []string{"0–4", "5–8", "9–12", "13–16", "17–20"}[i] {
			t.Fatalf("bucket %d = %+v", i, b)
		}
	}
	if got.TopDrink != nil || got.TopBrand != nil || got.TopVenue != nil {
		t.Fatalf("expected nil leaders, got %+v %+v %+v", got.TopDrink, got.TopBrand, got.TopVenue)
	}
	if got.Fingerprint == nil || got.Fingerprint.HasData {
		t.Fatalf("fingerprint = %+v, want no data", got.Fingerprint)
	}
	if len(got.Palate) != 0 {
		t.Fatalf("palate = %+v, want empty", got.Palate)
	}
}

func TestRecapYearTotals(t *testing.T) {
	e := newTestEnv(t)
	ctx := e.ctx
	style := recapStyleFor(t, ctx, e.db.Client, "Cola", "cola")
	brand := recapBrandFor(t, ctx, e.db.Client, "Brand A", "brand-a")
	venue, err := e.db.Client.Venue.Create().
		SetName("Corner Shop").SetSlug("corner-shop").
		SetCity("Rotterdam").SetCountry("Netherlands").Save(ctx)
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	d1 := recapDrinkFor(t, ctx, e.db.Client, "cola-a", "Cola A", &brand.ID, &style.ID)
	d2 := recapDrinkFor(t, ctx, e.db.Client, "cola-b", "Cola B", &brand.ID, &style.ID)
	d3 := recapDrinkFor(t, ctx, e.db.Client, "cola-c", "Cola C", &brand.ID, &style.ID)

	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	recapCheckin(t, ctx, e.db.Client, e.alice, d1, &venue.ID, intp(18), base)
	recapCheckin(t, ctx, e.db.Client, e.alice, d2, &venue.ID, intp(12), base.AddDate(0, 0, 1))
	recapCheckin(t, ctx, e.db.Client, e.alice, d3, nil, nil, base.AddDate(0, 0, 2))
	// Outside the window, must not count.
	recapCheckin(t, ctx, e.db.Client, e.alice, d1, &venue.ID, nil, time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC))

	got, err := e.svc.Recap(ctx, e.alice.ID, 2026)
	if err != nil {
		t.Fatalf("recap: %v", err)
	}
	if !got.HasData || !got.Sparse {
		t.Fatalf("has_data/sparse = %v/%v, want true/true", got.HasData, got.Sparse)
	}
	if got.Totals.Checkins != 3 {
		t.Fatalf("checkins = %d, want 3", got.Totals.Checkins)
	}
	if got.Totals.UniqueDrinks != 3 {
		t.Fatalf("unique drinks = %d, want 3", got.Totals.UniqueDrinks)
	}
	if got.Totals.Styles != 1 {
		t.Fatalf("styles = %d, want 1", got.Totals.Styles)
	}
	if got.Totals.Venues != 1 || got.Totals.Cities != 1 || got.Totals.Countries != 1 {
		t.Fatalf("venue/city/country = %d/%d/%d, want 1/1/1",
			got.Totals.Venues, got.Totals.Cities, got.Totals.Countries)
	}
	if got.Totals.AvgRating == nil || *got.Totals.AvgRating != 15 {
		t.Fatalf("avg rating = %v, want 15", got.Totals.AvgRating)
	}
	if got.TopVenue == nil || got.TopVenue.Name != "Corner Shop" || got.TopVenue.Count != 2 {
		t.Fatalf("top venue = %+v", got.TopVenue)
	}
	if got.TopBrand == nil || got.TopBrand.Slug != "brand-a" || got.TopBrand.Count != 3 {
		t.Fatalf("top brand = %+v", got.TopBrand)
	}
	if len(got.TopStyles) != 1 || got.TopStyles[0].Name != "Cola" || got.TopStyles[0].Count != 3 {
		t.Fatalf("top styles = %+v", got.TopStyles)
	}
	// Two rated check-ins: 18 in "17–20", 12 in "9–12".
	if got.RatingBuckets[2].Count != 1 || got.RatingBuckets[4].Count != 1 {
		t.Fatalf("rating buckets = %+v", got.RatingBuckets)
	}
	if len(got.Palate) != 0 {
		t.Fatalf("palate = %+v, want empty without a rated previous year", got.Palate)
	}
}

func TestRecapLongestStreak(t *testing.T) {
	cases := []struct {
		name string
		days []time.Time
		want int
	}{
		{
			name: "two consecutive days",
			days: []time.Time{
				time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
				time.Date(2026, 1, 2, 21, 0, 0, 0, time.UTC),
			},
			want: 2,
		},
		{
			name: "gap resets the run",
			days: []time.Time{
				time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
				time.Date(2026, 2, 2, 9, 0, 0, 0, time.UTC),
				time.Date(2026, 2, 5, 9, 0, 0, 0, time.UTC),
			},
			want: 2,
		},
		{
			name: "multiple pours on one day count once",
			days: []time.Time{
				time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
				time.Date(2026, 3, 1, 19, 0, 0, 0, time.UTC),
			},
			want: 1,
		},
		{
			name: "run crossing year boundary is clipped",
			days: []time.Time{
				time.Date(2025, 12, 31, 9, 0, 0, 0, time.UTC),
				time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
			},
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			d := e.drink
			for _, at := range tc.days {
				year := at.Year()
				if year != 2026 && year != 2025 {
					t.Fatalf("unexpected year in case")
				}
				recapCheckin(t, e.ctx, e.db.Client, e.alice, d, nil, nil, at)
			}
			got, err := e.svc.Recap(e.ctx, e.alice.ID, 2026)
			if err != nil {
				t.Fatalf("recap: %v", err)
			}
			if got.Totals.LongestStreak != tc.want {
				t.Fatalf("streak = %d, want %d", got.Totals.LongestStreak, tc.want)
			}
		})
	}
}

func TestRecapNewStyles(t *testing.T) {
	e := newTestEnv(t)
	ctx := e.ctx
	old := recapStyleFor(t, ctx, e.db.Client, "Ginger Ale", "ginger-ale")
	fresh := recapStyleFor(t, ctx, e.db.Client, "Root Beer", "root-beer")
	dOld := recapDrinkFor(t, ctx, e.db.Client, "old-drink", "Old Drink", nil, &old.ID)
	dFresh := recapDrinkFor(t, ctx, e.db.Client, "fresh-drink", "Fresh Drink", nil, &fresh.ID)

	// First tried in 2025, again in 2026: not new.
	recapCheckin(t, ctx, e.db.Client, e.alice, dOld, nil, nil, time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC))
	recapCheckin(t, ctx, e.db.Client, e.alice, dOld, nil, nil, time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	// First tried in 2026: new.
	recapCheckin(t, ctx, e.db.Client, e.alice, dFresh, nil, nil, time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC))

	got, err := e.svc.Recap(ctx, e.alice.ID, 2026)
	if err != nil {
		t.Fatalf("recap: %v", err)
	}
	if got.Totals.NewStyles != 1 {
		t.Fatalf("new styles = %d, want 1", got.Totals.NewStyles)
	}
	if got.Totals.Styles != 2 {
		t.Fatalf("styles = %d, want 2", got.Totals.Styles)
	}
}

func TestRecapPalateComparesYears(t *testing.T) {
	e := newTestEnv(t)
	ctx := e.ctx
	d := e.drink

	recapCheckin(t, ctx, e.db.Client, e.alice, d, nil, intp(10), time.Date(2025, 5, 1, 12, 0, 0, 0, time.UTC))
	recapCheckin(t, ctx, e.db.Client, e.alice, d, nil, intp(16), time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))

	got, err := e.svc.Recap(ctx, e.alice.ID, 2026)
	if err != nil {
		t.Fatalf("recap: %v", err)
	}
	if len(got.Palate) != 1 {
		t.Fatalf("palate = %+v, want one overall row", got.Palate)
	}
	row := got.Palate[0]
	if row.Label != "Overall" || row.ThisYear == nil || row.LastYear == nil {
		t.Fatalf("palate row = %+v", row)
	}
	if *row.ThisYear != 16 || *row.LastYear != 10 {
		t.Fatalf("palate values = %v/%v, want 16/10", *row.ThisYear, *row.LastYear)
	}
}

func TestRecapTopDrinkTieBreaksByName(t *testing.T) {
	e := newTestEnv(t)
	ctx := e.ctx
	banana := recapDrinkFor(t, ctx, e.db.Client, "banana-soda", "Banana Soda", nil, nil)
	apple := recapDrinkFor(t, ctx, e.db.Client, "apple-soda", "Apple Soda", nil, nil)

	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	recapCheckin(t, ctx, e.db.Client, e.alice, banana, nil, nil, at)
	recapCheckin(t, ctx, e.db.Client, e.alice, apple, nil, nil, at.Add(time.Hour))

	got, err := e.svc.Recap(ctx, e.alice.ID, 2026)
	if err != nil {
		t.Fatalf("recap: %v", err)
	}
	if got.TopDrink == nil || got.TopDrink.Name != "Apple Soda" {
		t.Fatalf("top drink = %+v, want Apple Soda", got.TopDrink)
	}
}

func TestFingerprintNormalisation(t *testing.T) {
	e := newTestEnv(t)
	ctx := e.ctx

	fruit, err := e.db.Client.Flavour.Create().SetName("Citrus").SetSlug("citrus").SetCategory("fruit").Save(ctx)
	if err != nil {
		t.Fatalf("flavour fruit: %v", err)
	}
	sweet, err := e.db.Client.Flavour.Create().SetName("Vanilla").SetSlug("vanilla").SetCategory("sweet").Save(ctx)
	if err != nil {
		t.Fatalf("flavour sweet: %v", err)
	}
	d := recapDrinkFor(t, ctx, e.db.Client, "fizzy", "Fizzy", nil, nil)
	for _, f := range []struct {
		id        int
		intensity int
	}{{fruit.ID, 5}, {sweet.ID, 1}} {
		if _, err := e.db.Client.DrinkFlavour.Create().
			SetDrinkID(d.ID).SetFlavourID(f.id).SetIntensity(f.intensity).Save(ctx); err != nil {
			t.Fatalf("drink flavour: %v", err)
		}
	}
	// Unrated check-in counts as 10/10, so sums are fruit=1.0, sweet=0.2.
	recapCheckin(t, ctx, e.db.Client, e.alice, d, nil, nil, time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))

	got, err := e.svc.Fingerprint(ctx, e.alice.ID)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if !got.HasData {
		t.Fatalf("has_data = false, want true")
	}
	if len(got.Axes) != 2 {
		t.Fatalf("axes = %d, want 2", len(got.Axes))
	}
	if got.Axes[0].Key != "fruit" || got.Axes[0].Score != 100 {
		t.Fatalf("top axis = %+v, want fruit at 100", got.Axes[0])
	}
	if got.Axes[1].Key != "sweet" || got.Axes[1].Score != 20 {
		t.Fatalf("second axis = %+v, want sweet at 20", got.Axes[1])
	}
	for _, ax := range got.Axes {
		if ax.Samples != 1 || ax.Confident {
			t.Fatalf("axis %+v: samples/confident = %d/%v, want 1/false", ax, ax.Samples, ax.Confident)
		}
	}
	if got.Confident {
		t.Fatalf("fingerprint confident = true with 1 sample per axis")
	}

	// Confidence needs five contributing check-ins on an axis.
	for i := 0; i < 4; i++ {
		recapCheckin(t, ctx, e.db.Client, e.alice, d, nil, nil,
			time.Date(2026, 7, 2+i, 12, 0, 0, 0, time.UTC))
	}
	got, err = e.svc.Fingerprint(ctx, e.alice.ID)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if got.Axes[0].Samples != 5 || !got.Axes[0].Confident {
		t.Fatalf("top axis after 5 checkins = %+v, want confident with 5 samples", got.Axes[0])
	}
	// Two confident axes is still below the three-axis threshold.
	if got.Confident {
		t.Fatalf("fingerprint confident = true with only 2 axes")
	}
}

func TestFingerprintEmpty(t *testing.T) {
	e := newTestEnv(t)
	got, err := e.svc.Fingerprint(e.ctx, e.alice.ID)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if got.HasData || got.Confident {
		t.Fatalf("empty fingerprint = %+v, want no data", got)
	}
	if len(got.Axes) != 0 {
		t.Fatalf("axes = %d, want 0", len(got.Axes))
	}
}

func TestRecapRejectsOutOfRangeYear(t *testing.T) {
	e := newTestEnv(t)
	for _, year := range []int{1999, 2101, 0, -1} {
		if _, err := e.svc.Recap(e.ctx, e.alice.ID, year); err == nil {
			t.Fatalf("year %d: expected an error", year)
		}
	}
	if _, err := e.svc.Recap(e.ctx, e.alice.ID, 2000); err != nil {
		t.Fatalf("year 2000 should be accepted: %v", err)
	}
	if _, err := e.svc.Recap(e.ctx, e.alice.ID, 2100); err != nil {
		t.Fatalf("year 2100 should be accepted: %v", err)
	}
}

func TestRecapYearsWithData(t *testing.T) {
	e := newTestEnv(t)
	ctx := e.ctx
	d := e.drink

	recapCheckin(t, ctx, e.db.Client, e.alice, d, nil, nil, time.Date(2023, 5, 1, 12, 0, 0, 0, time.UTC))
	recapCheckin(t, ctx, e.db.Client, e.alice, d, nil, nil, time.Date(2025, 5, 1, 12, 0, 0, 0, time.UTC))

	// A soft-deleted row and a merged drink are not recap data, so their years
	// must not appear either.
	deleted := recapCheckin(t, ctx, e.db.Client, e.alice, d, nil, nil, time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC))
	if err := e.db.Client.Checkin.UpdateOneID(deleted.ID).SetDeletedAt("2024-05-02T00:00:00Z").Exec(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	merged := recapDrinkFor(t, ctx, e.db.Client, "merged-recap", "Merged Recap", nil, nil)
	if err := e.db.Client.Drink.UpdateOneID(merged.ID).SetStatus("merged").Exec(ctx); err != nil {
		t.Fatalf("merge: %v", err)
	}
	recapCheckin(t, ctx, e.db.Client, e.alice, merged, nil, nil, time.Date(2022, 5, 1, 12, 0, 0, 0, time.UTC))

	got, err := e.svc.Recap(ctx, e.alice.ID, 2025)
	if err != nil {
		t.Fatalf("recap: %v", err)
	}
	if len(got.YearsWithData) != 2 || got.YearsWithData[0] != 2023 || got.YearsWithData[1] != 2025 {
		t.Fatalf("years_with_data = %v, want [2023 2025]", got.YearsWithData)
	}
	if got.Fingerprint == nil || !got.Fingerprint.WholeHistory {
		t.Fatalf("fingerprint.whole_history = %+v, want true", got.Fingerprint)
	}

	// Another member's history is never mixed in.
	bobRecap, err := e.svc.Recap(ctx, e.bob.ID, 2025)
	if err != nil {
		t.Fatalf("bob recap: %v", err)
	}
	if len(bobRecap.YearsWithData) != 0 {
		t.Fatalf("bob years_with_data = %v, want empty", bobRecap.YearsWithData)
	}
}

func TestRecapBadgesFilterByAwardYear(t *testing.T) {
	e := newTestEnv(t)
	ctx := e.ctx
	client := e.db.Client

	mkBadge := func(slug, name string) *ent.Badge {
		t.Helper()
		b, err := client.Badge.Create().SetSlug(slug).SetName(name).Save(ctx)
		if err != nil {
			t.Fatalf("badge %s: %v", slug, err)
		}
		return b
	}
	// Two earned badges, one inside the year and one outside, plus one that was
	// never earned. Slug order is the badge endpoint's order.
	inYear := mkBadge("recap-in", "Recap In")
	outside := mkBadge("recap-out", "Recap Out")
	mkBadge("recap-locked", "Recap Locked")

	award := func(b *ent.Badge, at string) {
		t.Helper()
		if _, err := client.UserBadge.Create().
			SetUserID(e.alice.ID).SetBadgeID(b.ID).SetLevel(1).
			SetAwardedAt(at).Save(ctx); err != nil {
			t.Fatalf("award %s: %v", b.Slug, err)
		}
	}
	award(inYear, "2025-03-02T09:00:00Z")
	award(outside, "2024-03-02T09:00:00Z")

	e.svc.Badges = badges.New(e.db)
	got, err := e.svc.Recap(ctx, e.alice.ID, 2025)
	if err != nil {
		t.Fatalf("recap: %v", err)
	}
	if len(got.Badges) != 1 {
		t.Fatalf("badges = %+v, want the one awarded in 2025", got.Badges)
	}
	badge := got.Badges[0]
	if badge.Slug != "recap-in" || badge.Name != "Recap In" || badge.AwardedAt != "2025-03-02T09:00:00Z" {
		t.Fatalf("badge = %+v", badge)
	}

	// A recap for a year with no awards carries an empty list, not null.
	empty, err := e.svc.Recap(ctx, e.alice.ID, 2023)
	if err != nil {
		t.Fatalf("recap 2023: %v", err)
	}
	if empty.Badges == nil || len(empty.Badges) != 0 {
		t.Fatalf("badges for 2023 = %+v, want an empty slice", empty.Badges)
	}
}
