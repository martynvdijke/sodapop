package checkins_test

import (
	"context"
	"math"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/internal/checkins"
	"sodapop/internal/db"
)

// insightDrink inserts a drink with explicit per-100 ml nutrition. An empty
// status leaves the Ent default (active).
func insightDrink(t *testing.T, ctx context.Context, client *ent.Client, slug, name string,
	volumeML, caffeinePer100, kcalPer100 int, sugarPer100 float64, status string) *ent.Drink {
	t.Helper()
	b := client.Drink.Create().
		SetName(name).
		SetSlug(slug).
		SetVolumeMl(volumeML).
		SetCaffeineMgPer100ml(caffeinePer100).
		SetCaloriesKcalPer100ml(kcalPer100).
		SetSugarGPer100ml(sugarPer100)
	if status != "" {
		b.SetStatus(drink.Status(status))
	}
	d, err := b.Save(ctx)
	if err != nil {
		t.Fatalf("drink %s: %v", slug, err)
	}
	return d
}

// logCheckin writes a check-in at a pinned timestamp, bypassing Create so tests
// control created_at.
func logCheckin(t *testing.T, ctx context.Context, database *db.DB, owner *ent.User, d *ent.Drink,
	at time.Time, deleted bool) *ent.Checkin {
	t.Helper()
	b := database.Client.Checkin.Create().
		SetUserID(owner.ID).
		SetDrinkID(d.ID).
		SetCreatedAt(at.UTC().Format(time.RFC3339))
	if deleted {
		b.SetDeletedAt(at.UTC().Format(time.RFC3339))
	}
	c, err := b.Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func TestInsightsNutritionMath(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	d := insightDrink(t, e.ctx, e.db.Client, "cola", "Cola", 355, 10, 40, 10.6, "")
	logCheckin(t, e.ctx, e.db, e.alice, d, now.Add(-2*time.Hour), false)

	got, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 30, Now: now})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	if math.Abs(got.SugarG-37.63) > 0.01 {
		t.Fatalf("sugar = %v, want 37.63", got.SugarG)
	}
	if math.Abs(got.CaffeineMg-35.5) > 0.01 {
		t.Fatalf("caffeine = %v, want 35.5", got.CaffeineMg)
	}
	if got.CaloriesKcal != 142 { // 40 kcal/100 ml * 3.55
		t.Fatalf("calories = %d, want 142", got.CaloriesKcal)
	}
	if got.Pours != 1 {
		t.Fatalf("pours = %d, want 1", got.Pours)
	}
	if math.Abs(got.AvgSugarPerDay-37.63/30) > 0.01 {
		t.Fatalf("avg sugar = %v, want %v", got.AvgSugarPerDay, 37.63/30)
	}
}

func TestInsightsExcludesDeletedAndOtherUsers(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	d := insightDrink(t, e.ctx, e.db.Client, "cola", "Cola", 355, 10, 40, 10.6, "")

	logCheckin(t, e.ctx, e.db, e.alice, d, now.Add(-time.Hour), false)
	logCheckin(t, e.ctx, e.db, e.alice, d, now.Add(-2*time.Hour), true) // deleted
	logCheckin(t, e.ctx, e.db, e.bob, d, now.Add(-3*time.Hour), false)  // someone else

	got, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 30, Now: now})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	if got.Pours != 1 {
		t.Fatalf("pours = %d, want 1", got.Pours)
	}
	if math.Abs(got.SugarG-37.63) > 0.01 {
		t.Fatalf("sugar = %v, want 37.63", got.SugarG)
	}
}

func TestInsightsRangeWindow(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	d := insightDrink(t, e.ctx, e.db.Client, "cola", "Cola", 355, 10, 40, 10.6, "")
	logCheckin(t, e.ctx, e.db, e.alice, d, now.Add(-45*24*time.Hour), false)

	short, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 30, Now: now})
	if err != nil {
		t.Fatalf("30d: %v", err)
	}
	if short.Pours != 0 {
		t.Fatalf("30d pours = %d, want 0", short.Pours)
	}

	long, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 90, Now: now})
	if err != nil {
		t.Fatalf("90d: %v", err)
	}
	if long.Pours != 1 {
		t.Fatalf("90d pours = %d, want 1", long.Pours)
	}

	// Unsupported windows normalise to 30.
	odd, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 12, Now: now})
	if err != nil {
		t.Fatalf("12d: %v", err)
	}
	if odd.Days != 30 || len(odd.Series) != 30 {
		t.Fatalf("unsupported range = %d days, %d points", odd.Days, len(odd.Series))
	}
}

func TestInsightsSeriesZeroFilled(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	d := insightDrink(t, e.ctx, e.db.Client, "cola", "Cola", 355, 10, 40, 10.6, "")
	logCheckin(t, e.ctx, e.db, e.alice, d, time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC), false)
	logCheckin(t, e.ctx, e.db, e.alice, d, time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC), false)
	logCheckin(t, e.ctx, e.db, e.alice, d, time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC), false)

	got, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 30, Now: now})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	if len(got.Series) != 30 {
		t.Fatalf("series points = %d, want 30", len(got.Series))
	}
	zeroDays := 0
	for _, p := range got.Series {
		if p.Pours == 0 {
			zeroDays++
		}
	}
	if zeroDays != 28 {
		t.Fatalf("zero days = %d, want 28", zeroDays)
	}
	var d19, d20 *checkins.DayPoint
	for i := range got.Series {
		switch got.Series[i].Date {
		case "2026-10-19":
			d19 = &got.Series[i]
		case "2026-10-20":
			d20 = &got.Series[i]
		}
	}
	if d19 == nil || d20 == nil {
		t.Fatalf("missing expected dates in series: %+v", got.Series)
	}
	if d19.Pours != 2 || d20.Pours != 1 {
		t.Fatalf("daily pours = %d/%d, want 2/1", d19.Pours, d20.Pours)
	}
	if math.Abs(d19.SugarG-2*37.63) > 0.01 {
		t.Fatalf("19th sugar = %v, want %v", d19.SugarG, 2*37.63)
	}
	if got.Series[len(got.Series)-1].Date != "2026-10-20" {
		t.Fatalf("series must be oldest first, last = %s", got.Series[len(got.Series)-1].Date)
	}
}

func TestInsightsCaffeineClearanceAndBedtime(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 10, 20, 23, 59, 0, 0, time.UTC)
	// 100 ml at 100 mg/100 ml = a 100 mg dose.
	d := insightDrink(t, e.ctx, e.db.Client, "espresso", "Espresso", 100, 100, 0, 0, "")
	doseAt := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	logCheckin(t, e.ctx, e.db, e.alice, d, doseAt, false)

	got, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 7, Now: now})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	if got.LastCaffeineAt != doseAt.Format(time.RFC3339) {
		t.Fatalf("last caffeine = %q, want %q", got.LastCaffeineAt, doseAt.Format(time.RFC3339))
	}
	// 100 mg decays to 20 mg after 5*log2(5) ~= 11.61 h, i.e. 21:36.
	wantClear := time.Date(2026, 10, 20, 21, 36, 35, 0, time.UTC)
	gotClear, err := time.Parse(time.RFC3339, got.CaffeineClearBy)
	if err != nil {
		t.Fatalf("parse clear by %q: %v", got.CaffeineClearBy, err)
	}
	if d := gotClear.Sub(wantClear); d < -time.Minute || d > time.Minute {
		t.Fatalf("clear by = %q, want ~%q (off by %v)", got.CaffeineClearBy, wantClear.Format(time.RFC3339), d)
	}
	if got.LateCaffeine {
		t.Fatal("no bedtime should not flag late caffeine")
	}

	// Bedtime 23:00 local: a 20:00 dose (clears the 8 h window at 04:00) is
	// late; a 09:00 dose (17:00) is not.
	lateEnv := newInsightBedtimeCase(t, 20)
	if lateEnv.LateCaffeine != true {
		t.Fatalf("20:00 dose with 23:00 bedtime: late = %v, want true", lateEnv.LateCaffeine)
	}
	earlyEnv := newInsightBedtimeCase(t, 9)
	if earlyEnv.LateCaffeine != false {
		t.Fatalf("09:00 dose with 23:00 bedtime: late = %v, want false", earlyEnv.LateCaffeine)
	}
}

func TestInsightsCaffeineClearanceCombinesDoses(t *testing.T) {
	// A single 60 mg dose clears below 20 mg after 5*log2(3) ~= 7.92 h.
	single := newTestEnv(t)
	d60 := insightDrink(t, single.ctx, single.db.Client, "sixty", "Sixty", 100, 60, 0, 0, "")
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	logCheckin(t, single.ctx, single.db, single.alice, d60, at, false)
	got, err := single.svc.Insights(single.ctx, single.alice.ID, checkins.InsightsOptions{
		Days: 7, Now: time.Date(2026, 10, 20, 23, 59, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	want := at.Add(time.Duration(5 * float64(time.Hour) * math.Log(3) / math.Log(2)))
	clear, err := time.Parse(time.RFC3339, got.CaffeineClearBy)
	if err != nil {
		t.Fatalf("parse clear by %q: %v", got.CaffeineClearBy, err)
	}
	if d := clear.Sub(want); d < -time.Minute || d > time.Minute {
		t.Fatalf("60 mg clear by = %q, want ~%q (off by %v)", got.CaffeineClearBy, want.Format(time.RFC3339), d)
	}

	// 30 mg at 15:00 plus 25 mg at 20:00 leaves 40 mg at 20:00, so clearing
	// takes a full half-life and lands at 01:00 — later than the 25 mg dose
	// would clear on its own.
	stacked := newTestEnv(t)
	d30 := insightDrink(t, stacked.ctx, stacked.db.Client, "thirty", "Thirty", 100, 30, 0, 0, "")
	d25 := insightDrink(t, stacked.ctx, stacked.db.Client, "twentyfive", "Twenty Five", 100, 25, 0, 0, "")
	logCheckin(t, stacked.ctx, stacked.db, stacked.alice, d30, time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC), false)
	last := time.Date(2026, 10, 20, 20, 0, 0, 0, time.UTC)
	logCheckin(t, stacked.ctx, stacked.db, stacked.alice, d25, last, false)
	got, err = stacked.svc.Insights(stacked.ctx, stacked.alice.ID, checkins.InsightsOptions{
		Days: 7, Now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	clear, err = time.Parse(time.RFC3339, got.CaffeineClearBy)
	if err != nil {
		t.Fatalf("parse clear by %q: %v", got.CaffeineClearBy, err)
	}
	want = last.Add(5 * time.Hour)
	if d := clear.Sub(want); d < -10*time.Minute || d > 10*time.Minute {
		t.Fatalf("stacked clear by = %q, want ~%q (off by %v)", got.CaffeineClearBy, want.Format(time.RFC3339), d)
	}
	solo := last.Add(time.Duration(5 * float64(time.Hour) * math.Log(1.25) / math.Log(2)))
	if !clear.After(solo) {
		t.Fatalf("stacked clear by %q should be later than 25 mg alone %q", clear.Format(time.RFC3339), solo.Format(time.RFC3339))
	}
}

func newInsightBedtimeCase(t *testing.T, hour int) *checkins.Insights {
	t.Helper()
	e := newTestEnv(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	d := insightDrink(t, e.ctx, e.db.Client, "cola", "Cola", 355, 10, 40, 10.6, "")
	logCheckin(t, e.ctx, e.db, e.alice, d, time.Date(2026, 10, 20, hour, 0, 0, 0, time.UTC), false)
	got, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{
		Days: 7, Bedtime: "23:00", Now: now,
	})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	return got
}

func TestInsightsEmpty(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	got, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 30, Now: now})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	if got.Pours != 0 || got.SugarG != 0 || got.CaffeineMg != 0 || got.CaloriesKcal != 0 {
		t.Fatalf("empty totals: %+v", got)
	}
	if len(got.Series) != 30 {
		t.Fatalf("series points = %d, want 30", len(got.Series))
	}
	for _, p := range got.Series {
		if p.Pours != 0 || p.SugarG != 0 || p.CaffeineMg != 0 || p.CaloriesKcal != 0 {
			t.Fatalf("empty series point: %+v", p)
		}
	}
	if got.LastCaffeineAt != "" || got.CaffeineClearBy != "" || got.LateCaffeine {
		t.Fatalf("empty caffeine fields: %+v", got)
	}
	if len(got.TopDrinks) != 0 {
		t.Fatalf("top drinks = %v, want empty", got.TopDrinks)
	}
}

func TestInsightsTopDrinks(t *testing.T) {
	e := newTestEnv(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cola := insightDrink(t, e.ctx, e.db.Client, "cola", "Cola", 355, 10, 40, 10.6, "")
	lemon := insightDrink(t, e.ctx, e.db.Client, "lemonade", "Lemonade", 200, 0, 30, 8, "")
	proto := insightDrink(t, e.ctx, e.db.Client, "mystery", "Mystery", 355, 0, 0, 5, "provisional")

	logCheckin(t, e.ctx, e.db, e.alice, cola, now.Add(-1*time.Hour), false)
	logCheckin(t, e.ctx, e.db, e.alice, cola, now.Add(-2*time.Hour), false)
	logCheckin(t, e.ctx, e.db, e.alice, lemon, now.Add(-3*time.Hour), false)
	logCheckin(t, e.ctx, e.db, e.alice, proto, now.Add(-4*time.Hour), false)

	got, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 30, Now: now})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	if got.Pours != 4 {
		t.Fatalf("pours = %d, want 4 (provisional still counts)", got.Pours)
	}
	if len(got.TopDrinks) != 2 {
		t.Fatalf("top drinks = %d, want 2 (provisional excluded): %+v", len(got.TopDrinks), got.TopDrinks)
	}
	first, second := got.TopDrinks[0], got.TopDrinks[1]
	if first.Slug != "cola" || first.Pours != 2 {
		t.Fatalf("first = %+v, want cola with 2 pours", first)
	}
	if math.Abs(first.SugarG-2*37.63) > 0.01 || math.Abs(first.CaffeineMg-2*35.5) > 0.01 {
		t.Fatalf("cola sums = %v g / %v mg", first.SugarG, first.CaffeineMg)
	}
	if second.Slug != "lemonade" || second.Pours != 1 {
		t.Fatalf("second = %+v, want lemonade with 1 pour", second)
	}
	if math.Abs(second.SugarG-16) > 0.01 {
		t.Fatalf("lemonade sugar = %v, want 16", second.SugarG)
	}
}
