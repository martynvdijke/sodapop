package integrations

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

func newTestService(t *testing.T) (*Service, *db.DB, context.Context) {
	t.Helper()
	// db.Init appends its own "?_pragma=..." suffix, so end the URI with "&"
	// to keep the shared-cache in-memory parameters intact.
	dsn := fmt.Sprintf("integrations-%d?mode=memory&cache=shared&", time.Now().UnixNano())
	database, err := db.Init(dsn)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	svc := New(database, &config.Config{BaseURL: "https://sodapop.test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc, database, context.Background()
}

func createUser(t *testing.T, database *db.DB, handle string) *ent.User {
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

func createDrink(t *testing.T, database *db.DB, name, slug string) *ent.Drink {
	t.Helper()
	d, err := database.Client.Drink.Create().
		SetName(name).
		SetSlug(slug).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create drink: %v", err)
	}
	return d
}

func TestRSSEscapesCheckinContent(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	d := createDrink(t, database, "Fizz & Co <Zero>", "fizz-and-co-zero")

	_, err := database.Client.Checkin.Create().
		SetUserID(u.ID).
		SetDrinkID(d.ID).
		SetRating(17).
		SetNote(`<script>alert("x & y")</script>`).
		SetVisibility(checkin.VisibilityPublic).
		SetCreatedAt("2026-10-20T10:00:00Z").
		SetUpdatedAt("2026-10-20T10:00:00Z").
		Save(ctx)
	if err != nil {
		t.Fatalf("create checkin: %v", err)
	}

	feedURL, err := svc.RSSURL(ctx, u.ID)
	if err != nil {
		t.Fatalf("rss url: %v", err)
	}
	const prefix = "https://sodapop.test/feeds/"
	if !strings.HasPrefix(feedURL, prefix) || !strings.HasSuffix(feedURL, ".xml") {
		t.Fatalf("unexpected feed url %q", feedURL)
	}
	token := strings.TrimSuffix(strings.TrimPrefix(feedURL, prefix), ".xml")

	raw, err := svc.RSS(ctx, token)
	if err != nil {
		t.Fatalf("rss: %v", err)
	}
	got := string(raw)

	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<rss version="2.0">`,
		`<title>@ada rated Fizz &amp; Co &lt;Zero&gt; 4.25/5</title>`,
		`<pubDate>Tue, 20 Oct 2026 10:00:00 +0000</pubDate>`,
		`&lt;script&gt;alert(&#34;x &amp; y&#34;)&lt;/script&gt;`,
		`https://sodapop.test/drinks/fizz-and-co-zero`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RSS output is missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{
		`Fizz & Co`,
		`<script>`,
		`alert("x & y")`,
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("RSS output contains unescaped %q:\n%s", unwanted, got)
		}
	}

	if _, err := svc.RSS(ctx, "not-a-token"); err == nil {
		t.Fatal("expected an error for an unknown feed token")
	}
}

func TestImportCSVIsIdempotent(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	cola := createDrink(t, database, "Fizzy Cola", "fizzy-cola")
	createDrink(t, database, "Root Beer", "root-beer")

	const csvData = `drink_slug,rating,serving,note,created_at
fizzy-cola,4.25,can,"crisp & cold",2026-01-02T03:04:05Z
root-beer,3,bottle,,2026-01-03T04:05:06Z
`
	inserted, err := svc.ImportCSV(ctx, u.ID, strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("import csv: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("inserted = %d, want 2", inserted)
	}

	rows, err := database.Client.Checkin.Query().
		Where(checkin.UserID(u.ID)).
		Order(ent.Asc(checkin.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		t.Fatalf("query checkins: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("stored %d checkins, want 2", len(rows))
	}
	if rows[0].DrinkID != cola.ID {
		t.Fatalf("first drink = %d, want %d", rows[0].DrinkID, cola.ID)
	}
	if rows[0].Rating == nil || *rows[0].Rating != 17 {
		t.Fatalf("first rating = %v, want 17", rows[0].Rating)
	}
	if rows[1].Rating == nil || *rows[1].Rating != 12 {
		t.Fatalf("second rating = %v, want 12", rows[1].Rating)
	}
	if rows[0].Note != "crisp & cold" {
		t.Fatalf("note = %q", rows[0].Note)
	}
	if rows[0].CreatedAt != "2026-01-02T03:04:05Z" {
		t.Fatalf("created_at = %q", rows[0].CreatedAt)
	}
	if rows[0].IdempotencyKey == nil || !strings.HasPrefix(*rows[0].IdempotencyKey, "import-") {
		t.Fatalf("unexpected idempotency key %v", rows[0].IdempotencyKey)
	}

	again, err := svc.ImportCSV(ctx, u.ID, strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("re-import csv: %v", err)
	}
	if again != 0 {
		t.Fatalf("re-import inserted = %d, want 0", again)
	}
	count, err := database.Client.Checkin.Query().Where(checkin.UserID(u.ID)).Count(ctx)
	if err != nil {
		t.Fatalf("count checkins: %v", err)
	}
	if count != 2 {
		t.Fatalf("checkin count after re-import = %d, want 2", count)
	}

	bad := `drink_slug,rating,serving,note,created_at
fizzy-cola,9,can,too high,2026-01-04T00:00:00Z
`
	if _, err := svc.ImportCSV(ctx, u.ID, strings.NewReader(bad)); err == nil {
		t.Fatal("expected an error for an out-of-range rating")
	}
	count, err = database.Client.Checkin.Query().Where(checkin.UserID(u.ID)).Count(ctx)
	if err != nil {
		t.Fatalf("count checkins: %v", err)
	}
	if count != 2 {
		t.Fatalf("bad import changed checkin count to %d", count)
	}
}

func TestImportCSVRejectsUnknownDrink(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	const csvData = `drink_slug,rating,serving,note,created_at
missing,4,can,,2026-01-02T03:04:05Z
`
	if _, err := svc.ImportCSV(ctx, u.ID, strings.NewReader(csvData)); err == nil {
		t.Fatal("expected an error for an unknown drink slug")
	}
}

func TestDispatchDoesNotBlockWithoutNetworkConfig(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	done := make(chan struct{})
	go func() {
		svc.Dispatch(ctx, Event{UserID: u.ID, Kind: EventCheckin, Text: "drank a soda", URL: "https://sodapop.test/drinks/x"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Dispatch blocked the caller")
	}
	// Give the delivery goroutine a moment to finish and make sure it did not
	// panic or leave the test process in a bad state.
	time.Sleep(50 * time.Millisecond)
}

func TestSaveGetListAndPushSubscription(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	if err := svc.Save(ctx, u.ID, "home_assistant", "https://ha.example/webhook", "shh", true); err != nil {
		t.Fatalf("save: %v", err)
	}
	row, err := svc.Get(ctx, u.ID, "home_assistant")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Target != "https://ha.example/webhook" || row.Secret != "shh" || !row.Enabled {
		t.Fatalf("unexpected row: %+v", row)
	}
	if err := svc.Save(ctx, u.ID, "home_assistant", "https://ha.example/v2", "shh", false); err != nil {
		t.Fatalf("update: %v", err)
	}
	rows, err := svc.List(ctx, u.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Target != "https://ha.example/v2" || rows[0].Enabled {
		t.Fatalf("unexpected list: %+v", rows)
	}
	if err := svc.Save(ctx, u.ID, "bogus", "", "", true); err == nil {
		t.Fatal("expected an error for an unknown kind")
	}

	if err := svc.SubscribePush(ctx, u.ID, "https://push.example/1", "p256dh", "auth"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := svc.SubscribePush(ctx, u.ID, "https://push.example/1", "p256dh2", "auth2"); err != nil {
		t.Fatalf("refresh subscription: %v", err)
	}
	subs, err := database.Client.PushSubscription.Query().All(ctx)
	if err != nil {
		t.Fatalf("query subscriptions: %v", err)
	}
	if len(subs) != 1 || subs[0].P256dh != "p256dh2" {
		t.Fatalf("unexpected subscriptions: %+v", subs)
	}
	if err := svc.UnsubscribePush(ctx, u.ID, "https://push.example/1"); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	n, err := database.Client.PushSubscription.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if n != 0 {
		t.Fatalf("subscriptions after delete = %d", n)
	}
}

func TestWeeklyDigestQueuesEmail(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	friend := createUser(t, database, "bob")
	if _, err := database.Client.Friendship.Create().
		SetUserA(minString(u.ID, friend.ID)).
		SetUserB(maxString(u.ID, friend.ID)).
		SetRequestedBy(friend.ID).
		SetStatus("accepted").
		Save(ctx); err != nil {
		t.Fatalf("create friendship: %v", err)
	}
	if _, err := database.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetEmailWeekly(true).
		Save(ctx); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	d := createDrink(t, database, "Fizzy Cola", "fizzy-cola")
	if _, err := database.Client.Checkin.Create().
		SetUserID(friend.ID).
		SetDrinkID(d.ID).
		SetRating(16).
		SetVisibility(checkin.VisibilityPublic).
		SetCreatedAt(time.Now().UTC().Format(time.RFC3339)).
		Save(ctx); err != nil {
		t.Fatalf("create friend checkin: %v", err)
	}

	if err := svc.WeeklyDigest(ctx); err != nil {
		t.Fatalf("weekly digest: %v", err)
	}
	rows, err := database.Client.EmailOutbox.Query().All(ctx)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("outbox rows = %d, want 1", len(rows))
	}
	if rows[0].ToEmail != u.Email || rows[0].Template != "weekly_digest" {
		t.Fatalf("unexpected outbox row: %+v", rows[0])
	}
	if !strings.Contains(rows[0].PayloadJSON, "bob") || !strings.Contains(rows[0].PayloadJSON, "Fizzy Cola") {
		t.Fatalf("payload missing friend activity: %s", rows[0].PayloadJSON)
	}
}

func TestExportJSONContainsOnlyOwnData(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	other := createUser(t, database, "eve")
	d := createDrink(t, database, "Fizzy Cola", "fizzy-cola")
	if _, err := database.Client.Checkin.Create().
		SetUserID(u.ID).SetDrinkID(d.ID).SetVisibility(checkin.VisibilityPublic).
		Save(ctx); err != nil {
		t.Fatalf("create checkin: %v", err)
	}
	if _, err := database.Client.Checkin.Create().
		SetUserID(other.ID).SetDrinkID(d.ID).SetVisibility(checkin.VisibilityPublic).
		Save(ctx); err != nil {
		t.Fatalf("create other checkin: %v", err)
	}

	raw, err := svc.ExportJSON(ctx, u.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"handle": "ada"`) {
		t.Fatalf("export missing profile: %s", got)
	}
	if strings.Contains(got, `"handle": "eve"`) {
		t.Fatalf("export leaked another user: %s", got)
	}
	if strings.Contains(strings.ToLower(got), "password") {
		t.Fatalf("export leaked password data: %s", got)
	}
}

func minString(a, b string) string {
	if a < b {
		return a
	}
	return b
}

func maxString(a, b string) string {
	if a > b {
		return a
	}
	return b
}
