package integrations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/integration"
)

func TestTRMNLPostsMergeVariables(t *testing.T) {
	svc, database, ctx := newTestService(t)
	svc.Cfg.Version = "1.2.3"
	if svc.HTTP.Timeout != 10*time.Second {
		t.Fatalf("http client timeout = %v, want 10s", svc.HTTP.Timeout)
	}
	u := createUser(t, database, "ada")
	d := createDrink(t, database, "Fizzy Cola", "fizzy-cola")
	if _, err := database.Client.Checkin.Create().
		SetUserID(u.ID).
		SetDrinkID(d.ID).
		SetRating(17).
		SetVisibility(checkin.VisibilityPublic).
		SetCreatedAt("2026-10-20T10:00:00Z").
		Save(ctx); err != nil {
		t.Fatalf("create checkin: %v", err)
	}

	type captured struct {
		method string
		ua     string
		ctype  string
		body   trmnlPayload
	}
	got := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var p trmnlPayload
		_ = json.Unmarshal(raw, &p)
		got <- captured{method: r.Method, ua: r.Header.Get("User-Agent"), ctype: r.Header.Get("Content-Type"), body: p}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := database.Client.Integration.Create().
		SetUserID(u.ID).
		SetKind(integration.KindTrmnl).
		SetTarget(srv.URL).
		SetEnabled(true).
		Save(ctx); err != nil {
		t.Fatalf("create integration: %v", err)
	}

	if err := svc.TRMNL(ctx, u.ID); err != nil {
		t.Fatalf("trmnl: %v", err)
	}
	select {
	case c := <-got:
		if c.method != http.MethodPost {
			t.Errorf("method = %q, want POST", c.method)
		}
		if c.ua != "sodapop/1.2.3" {
			t.Errorf("user-agent = %q", c.ua)
		}
		if c.ctype != "application/json" {
			t.Errorf("content-type = %q", c.ctype)
		}
		if c.body.MergeVariables.Title != "Sodapop" {
			t.Errorf("title = %q", c.body.MergeVariables.Title)
		}
		if c.body.MergeVariables.Text != "@ada rated Fizzy Cola 4.25/5" {
			t.Errorf("text = %q", c.body.MergeVariables.Text)
		}
		if !strings.HasPrefix(c.body.MergeVariables.URL, "https://sodapop.test/checkins/") {
			t.Errorf("url = %q", c.body.MergeVariables.URL)
		}
		if c.body.MergeVariables.UpdatedAt != "2026-10-20T10:00:00Z" {
			t.Errorf("updated_at = %q", c.body.MergeVariables.UpdatedAt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TRMNL did not POST to the target")
	}
}

func TestTRMNLPostsQuietPayloadWithoutCheckins(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	got := make(chan trmnlPayload, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var p trmnlPayload
		_ = json.Unmarshal(raw, &p)
		got <- p
	}))
	defer srv.Close()

	if _, err := database.Client.Integration.Create().
		SetUserID(u.ID).
		SetKind(integration.KindTrmnl).
		SetTarget(srv.URL).
		SetEnabled(true).
		Save(ctx); err != nil {
		t.Fatalf("create integration: %v", err)
	}
	if err := svc.TRMNL(ctx, u.ID); err != nil {
		t.Fatalf("trmnl: %v", err)
	}
	select {
	case p := <-got:
		if p.MergeVariables.Text != "No pours yet." {
			t.Fatalf("text = %q, want quiet payload", p.MergeVariables.Text)
		}
		if p.MergeVariables.URL != "https://sodapop.test" {
			t.Fatalf("url = %q", p.MergeVariables.URL)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TRMNL did not POST the quiet payload")
	}
}

func TestTRMNLDisabledIsNoOp(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	if _, err := database.Client.Integration.Create().
		SetUserID(u.ID).
		SetKind(integration.KindTrmnl).
		SetTarget(srv.URL).
		SetEnabled(false).
		Save(ctx); err != nil {
		t.Fatalf("create integration: %v", err)
	}
	if err := svc.TRMNL(ctx, u.ID); err != nil {
		t.Fatalf("trmnl: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("disabled integration made %d requests", hits.Load())
	}
}

func TestDispatchPushHonoursSetting(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	if _, err := database.Client.PushSubscription.Create().
		SetUserID(u.ID).
		SetEndpoint("https://push.example/1").
		SetP256dh("p256dh").
		SetAuth("auth").
		Save(ctx); err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	calls := make(chan string, 4)
	pushDeliveryHook = func(_ context.Context, _ []*ent.PushSubscription, title, _, _ string) error {
		calls <- title
		return nil
	}
	defer func() { pushDeliveryHook = nil }()

	// Push disabled (the default) => no delivery.
	svc.Dispatch(ctx, Event{UserID: u.ID, Kind: EventCheckin, Text: "drank", URL: "https://sodapop.test/x"})
	select {
	case <-calls:
		t.Fatal("push was sent while push_enabled is false")
	case <-time.After(300 * time.Millisecond):
	}

	if _, err := database.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetPushEnabled(true).
		Save(ctx); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	svc.Dispatch(ctx, Event{UserID: u.ID, Kind: EventCheckin, Text: "drank", URL: "https://sodapop.test/x"})
	select {
	case title := <-calls:
		if title != "New check-in" {
			t.Fatalf("push title = %q", title)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("push was not sent while push_enabled is true")
	}
}
