package overview_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/drink"
	"sodapop/ent/report"
	"sodapop/ent/venue"
	"sodapop/internal/db"
	"sodapop/internal/overview"
)

func newFixture(t *testing.T) (context.Context, *db.DB, *overview.Service, *ent.User, *ent.Drink, *ent.Checkin, *ent.Comment) {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "overview.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	svc := overview.New(database.SQL)

	u, err := database.Client.User.Create().
		SetHandle("alice").SetEmail("alice@example.com").SetDisplayName("Alice").
		SetCreatedAt("2026-01-01T00:00:01Z").Save(ctx)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	d, err := database.Client.Drink.Create().
		SetName("Cola").SetSlug("cola").SetStatus(drink.StatusActive).
		SetCreatedBy(u.ID).SetCreatedAt("2026-01-01T00:00:02Z").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	if _, err := database.Client.Venue.Create().
		SetName("Corner Shop").SetSlug("corner-shop").SetKind(venue.KindShop).SetCity("Portland").
		SetCreatedBy(u.ID).SetCreatedAt("2026-01-01T00:00:03Z").Save(ctx); err != nil {
		t.Fatalf("venue: %v", err)
	}
	c, err := database.Client.Checkin.Create().
		SetUserID(u.ID).SetDrinkID(d.ID).SetVisibility(checkin.VisibilityPublic).
		SetCreatedAt("2026-01-01T00:00:04Z").Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if _, err := database.Client.Report.Create().
		SetEntity(report.EntityDrink).SetEntityID(d.ID).SetUserID(u.ID).
		SetReason("spam").SetStatus(report.StatusOpen).
		SetCreatedAt("2026-01-01T00:00:05Z").Save(ctx); err != nil {
		t.Fatalf("report: %v", err)
	}
	cm, err := database.Client.Comment.Create().
		SetCheckinID(c.ID).SetUserID(u.ID).SetBody("great soda").
		SetCreatedAt("2026-01-01T00:00:06Z").Save(ctx)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, err := database.Client.CheckinLike.Create().
		SetCheckinID(c.ID).SetUserID(u.ID).
		SetCreatedAt("2026-01-01T00:00:07Z").Save(ctx); err != nil {
		t.Fatalf("toast: %v", err)
	}
	return ctx, database, svc, u, d, c, cm
}

func TestListReturnsAllSevenKindsNewestFirst(t *testing.T) {
	ctx, _, svc, u, d, c, cm := newFixture(t)

	items, next, err := svc.List(ctx, overview.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if next != "" {
		t.Fatalf("next = %q, want empty", next)
	}
	want := []string{
		"toast:" + c.ID + ":" + u.ID,
		"comment:" + cm.ID,
		"report:1",
		"checkin:" + c.ID,
		"venue:1",
		"drink:" + fmt.Sprint(d.ID),
		"user:" + u.ID,
	}
	if len(items) != len(want) {
		t.Fatalf("items = %d, want %d (%+v)", len(items), len(want), items)
	}
	seen := map[string]bool{}
	for i, wantID := range want {
		if items[i].ID != wantID {
			t.Errorf("items[%d].ID = %q, want %q", i, items[i].ID, wantID)
		}
		seen[items[i].Kind] = true
	}
	for _, kind := range []string{"checkin", "drink", "user", "venue", "report", "comment", "toast"} {
		if !seen[kind] {
			t.Errorf("missing kind %q", kind)
		}
	}
	if items[2].Detail != "spam" || items[2].Status != "open" || items[2].Href != "/admin" {
		t.Errorf("report event = %+v", items[2])
	}
	if items[3].ActorHandle != "alice" || items[3].Visibility != "public" || items[3].SubjectID != c.ID {
		t.Errorf("checkin event = %+v", items[3])
	}
	if items[4].Detail != "shop, Portland" {
		t.Errorf("venue detail = %q", items[4].Detail)
	}
}

func TestListKeysetPagingHasNoGapsOrDuplicates(t *testing.T) {
	ctx, database, svc, u, _, c, _ := newFixture(t)

	// Five comments on the same second force the id tie-breaker to carry the
	// page boundary across equal created_at values.
	same := "2026-01-01T00:00:10Z"
	for i := 0; i < 5; i++ {
		if _, err := database.Client.Comment.Create().
			SetCheckinID(c.ID).SetUserID(u.ID).SetBody(fmt.Sprintf("comment %d", i)).
			SetCreatedAt(same).Save(ctx); err != nil {
			t.Fatalf("comment %d: %v", i, err)
		}
	}

	all, _, err := svc.List(ctx, overview.Filter{Limit: 100})
	if err != nil {
		t.Fatalf("full list: %v", err)
	}
	if len(all) != 7+5 {
		t.Fatalf("full list = %d, want 12", len(all))
	}

	var paged []overview.Event
	before := ""
	for page := 0; page < 10; page++ {
		items, next, err := svc.List(ctx, overview.Filter{Limit: 2, Before: before})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		paged = append(paged, items...)
		if next == "" {
			break
		}
		before = next
	}
	if len(paged) != len(all) {
		t.Fatalf("paged = %d rows, want %d", len(paged), len(all))
	}
	seen := map[string]bool{}
	for i := range all {
		if paged[i].ID != all[i].ID {
			t.Fatalf("position %d: paged %q, full %q", i, paged[i].ID, all[i].ID)
		}
		if seen[paged[i].ID] {
			t.Fatalf("duplicate id %q", paged[i].ID)
		}
		seen[paged[i].ID] = true
	}
}

func TestListKindFilterAndAllUnknown(t *testing.T) {
	ctx, _, svc, _, _, _, _ := newFixture(t)

	items, _, err := svc.List(ctx, overview.Filter{Kinds: []string{"drink", "nope"}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].Kind != "drink" {
		t.Fatalf("items = %+v, want exactly one drink", items)
	}

	items, next, err := svc.List(ctx, overview.Filter{Kinds: []string{"nope", "also-nope"}})
	if err != nil {
		t.Fatalf("all-unknown list: %v", err)
	}
	if items == nil {
		t.Fatal("all-unknown items is nil, want empty slice")
	}
	if len(items) != 0 || next != "" {
		t.Fatalf("all-unknown = %d items next %q, want 0 and empty", len(items), next)
	}
}

func TestListExcludesSoftDeletedAndMergedRows(t *testing.T) {
	ctx, database, svc, u, d, c, _ := newFixture(t)

	if _, err := database.Client.Checkin.Create().
		SetUserID(u.ID).SetDrinkID(d.ID).SetVisibility(checkin.VisibilityPublic).
		SetDeletedAt("2026-01-02T00:00:00Z").
		SetCreatedAt("2026-01-01T00:00:20Z").Save(ctx); err != nil {
		t.Fatalf("deleted checkin: %v", err)
	}
	if _, err := database.Client.Comment.Create().
		SetCheckinID(c.ID).SetUserID(u.ID).SetBody("removed").
		SetDeletedAt("2026-01-02T00:00:00Z").
		SetCreatedAt("2026-01-01T00:00:21Z").Save(ctx); err != nil {
		t.Fatalf("deleted comment: %v", err)
	}
	if _, err := database.Client.Venue.Create().
		SetName("Merged Shop").SetSlug("merged-shop").
		SetMergedIntoID(1).
		SetCreatedAt("2026-01-01T00:00:22Z").Save(ctx); err != nil {
		t.Fatalf("merged venue: %v", err)
	}

	items, _, err := svc.List(ctx, overview.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, item := range items {
		if item.ID == "comment:"+c.ID && item.Title == "removed" {
			t.Fatalf("soft-deleted comment was returned: %+v", item)
		}
		if item.Kind == "venue" && item.Title == "Merged Shop" {
			t.Fatalf("merged venue was returned: %+v", item)
		}
		if item.Kind == "checkin" && item.CreatedAt == "2026-01-01T00:00:20Z" {
			t.Fatalf("soft-deleted checkin was returned: %+v", item)
		}
	}
}

func TestCounts(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "counts.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }

	uRecent, err := database.Client.User.Create().
		SetHandle("recent").SetEmail("recent@example.com").SetCreatedAt(at(-time.Hour)).Save(ctx)
	if err != nil {
		t.Fatalf("recent user: %v", err)
	}
	if _, err := database.Client.User.Create().
		SetHandle("old").SetEmail("old@example.com").SetCreatedAt(at(-10 * 24 * time.Hour)).Save(ctx); err != nil {
		t.Fatalf("old user: %v", err)
	}
	dRecent, err := database.Client.Drink.Create().
		SetName("Recent Cola").SetSlug("recent-cola").SetStatus(drink.StatusActive).
		SetCreatedAt(at(-time.Hour)).Save(ctx)
	if err != nil {
		t.Fatalf("recent drink: %v", err)
	}
	if _, err := database.Client.Drink.Create().
		SetName("Provisional Cola").SetSlug("provisional-cola").SetStatus(drink.StatusProvisional).
		SetCreatedAt(at(-2 * time.Hour)).Save(ctx); err != nil {
		t.Fatalf("provisional drink: %v", err)
	}
	dOld, err := database.Client.Drink.Create().
		SetName("Old Cola").SetSlug("old-cola").SetStatus(drink.StatusActive).
		SetCreatedAt(at(-10 * 24 * time.Hour)).Save(ctx)
	if err != nil {
		t.Fatalf("old drink: %v", err)
	}
	if _, err := database.Client.Checkin.Create().
		SetUserID(uRecent.ID).SetDrinkID(dRecent.ID).
		SetCreatedAt(at(-time.Hour)).Save(ctx); err != nil {
		t.Fatalf("recent checkin: %v", err)
	}
	if _, err := database.Client.Checkin.Create().
		SetUserID(uRecent.ID).SetDrinkID(dOld.ID).
		SetCreatedAt(at(-10 * 24 * time.Hour)).Save(ctx); err != nil {
		t.Fatalf("old checkin: %v", err)
	}
	if _, err := database.Client.Report.Create().
		SetEntity(report.EntityDrink).SetEntityID(dRecent.ID).SetStatus(report.StatusOpen).
		Save(ctx); err != nil {
		t.Fatalf("open report: %v", err)
	}
	if _, err := database.Client.Report.Create().
		SetEntity(report.EntityDrink).SetEntityID(dRecent.ID).SetStatus(report.StatusResolved).
		Save(ctx); err != nil {
		t.Fatalf("resolved report: %v", err)
	}

	counts, err := overview.New(database.SQL).Counts(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	want := overview.Counts{Checkins24h: 1, Drinks24h: 2, Users7d: 1, OpenReports: 1, PendingDrinks: 1}
	if counts != want {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
	if _, _, err := overview.New(database.SQL).List(ctx, overview.Filter{Before: "not-a-cursor"}); !errors.Is(err, overview.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v, want ErrInvalidCursor", err)
	}
}
