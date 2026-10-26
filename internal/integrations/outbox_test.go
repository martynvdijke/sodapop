package integrations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/deliveryoutbox"
	"sodapop/ent/integration"
	"sodapop/internal/db"
)

func TestDispatchEnqueuesOneRowPerTarget(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	for _, spec := range []struct{ kind, target, secret string }{
		{"home_assistant", "https://ha.example/hook", "ha-secret"},
		{"matrix", "https://matrix.example|!room:example.org", "matrix-secret"},
		{"trmnl", "https://trmnl.example/hook", ""},
	} {
		if err := svc.Save(ctx, u.ID, spec.kind, spec.target, spec.secret, true); err != nil {
			t.Fatalf("save %s: %v", spec.kind, err)
		}
	}
	if _, err := database.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetPushEnabled(true).
		Save(ctx); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	subscriptions := []*ent.PushSubscription{}
	for _, endpoint := range []string{"https://push.example/a", "https://push.example/b"} {
		sub, err := database.Client.PushSubscription.Create().
			SetUserID(u.ID).
			SetEndpoint(endpoint).
			SetP256dh("p256dh").
			SetAuth("auth").
			Save(ctx)
		if err != nil {
			t.Fatalf("create subscription: %v", err)
		}
		subscriptions = append(subscriptions, sub)
	}

	event := Event{UserID: u.ID, Kind: EventCheckin, Text: "Drank a cola", URL: "https://sodapop.test/you"}
	if err := svc.Dispatch(ctx, event); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	rows, err := database.Client.DeliveryOutbox.Query().
		Order(ent.Asc(deliveryoutbox.FieldID)).
		All(ctx)
	if err != nil {
		t.Fatalf("load outbox: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("outbox rows = %d, want 5", len(rows))
	}

	integrations, err := database.Client.Integration.Query().
		Where(integration.UserID(u.ID)).
		All(ctx)
	if err != nil {
		t.Fatalf("load integrations: %v", err)
	}
	integrationsByKind := map[integration.Kind]*ent.Integration{}
	for _, row := range integrations {
		integrationsByKind[row.Kind] = row
	}

	wantKinds := map[deliveryoutbox.Kind]int{
		deliveryoutbox.KindWebhook: 1,
		deliveryoutbox.KindMatrix:  1,
		deliveryoutbox.KindTrmnl:   1,
		deliveryoutbox.KindPush:    2,
	}
	gotKinds := map[deliveryoutbox.Kind]int{}
	seenTargets := map[string]bool{}
	for _, row := range rows {
		gotKinds[row.Kind]++
		if row.UserID != u.ID {
			t.Errorf("row %d user = %q, want %q", row.ID, row.UserID, u.ID)
		}
		if row.Attempts != 0 || row.LastError != "" || row.DeliveredAt != nil {
			t.Errorf("row %d has non-fresh state: attempts=%d last_error=%q delivered=%v",
				row.ID, row.Attempts, row.LastError, row.DeliveredAt)
		}
		if row.NextAttemptAt == "" {
			t.Errorf("row %d has empty next_attempt_at", row.ID)
		}
		if strings.Contains(row.PayloadJSON, "secret") {
			t.Errorf("payload leaked a secret: %s", row.PayloadJSON)
		}
		seenTargets[row.Kind.String()+":"+row.Target] = true

		var queued Event
		if err := json.Unmarshal([]byte(row.PayloadJSON), &queued); err != nil {
			t.Errorf("payload is not an event: %v", err)
			continue
		}
		if queued != event {
			t.Errorf("queued event = %+v, want %+v", queued, event)
		}
	}
	for kind, want := range wantKinds {
		if gotKinds[kind] != want {
			t.Errorf("kind %s rows = %d, want %d", kind, gotKinds[kind], want)
		}
	}

	for _, kind := range []integration.Kind{
		integration.KindHomeAssistant, integration.KindMatrix, integration.KindTrmnl,
	} {
		row := integrationsByKind[kind]
		outboxKind, _ := outboxKind(kind)
		key := outboxKind.String() + ":" + strconv.Itoa(row.ID)
		if !seenTargets[key] {
			t.Errorf("missing outbox row for %s target %s", kind, key)
		}
	}
	for _, sub := range subscriptions {
		key := deliveryoutbox.KindPush.String() + ":" + strconv.Itoa(sub.ID)
		if !seenTargets[key] {
			t.Errorf("missing outbox row for push subscription %s", key)
		}
	}
}

func TestDispatchWithoutTargetsIsNil(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	if err := svc.Dispatch(ctx, Event{UserID: u.ID, Kind: EventCheckin, Text: "quiet"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	depth, err := svc.OutboxDepth(ctx)
	if err != nil {
		t.Fatalf("depth: %v", err)
	}
	if depth != 0 {
		t.Fatalf("outbox depth = %d, want 0", depth)
	}
}

func TestPushRequiresEnabledPreference(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	if _, err := database.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetPushEnabled(false).
		Save(ctx); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if _, err := database.Client.PushSubscription.Create().
		SetUserID(u.ID).
		SetEndpoint("https://push.example/a").
		SetP256dh("p256dh").
		SetAuth("auth").
		Save(ctx); err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	if err := svc.Dispatch(ctx, Event{UserID: u.ID, Kind: EventToast, Text: "toast"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if depth, _ := svc.OutboxDepth(ctx); depth != 0 {
		t.Fatalf("outbox depth = %d, want 0", depth)
	}
}

func TestOutboxWorkerDeliversWebhook(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	var calls int32
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := svc.Save(ctx, u.ID, "home_assistant", server.URL, "hook-secret", true); err != nil {
		t.Fatalf("save integration: %v", err)
	}
	event := Event{UserID: u.ID, Kind: EventCheckin, Text: "Drank a soda", URL: "https://sodapop.test/you"}
	if err := svc.Dispatch(ctx, event); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	svc.processOutbox(ctx)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("webhook calls = %d, want 1", got)
	}
	var delivered homeAssistantPayload
	if err := json.Unmarshal(body, &delivered); err != nil {
		t.Fatalf("decode webhook body: %v", err)
	}
	if delivered.Event != event.Kind || delivered.Text != event.Text || delivered.URL != event.URL {
		t.Fatalf("webhook payload = %+v", delivered)
	}

	row := onlyOutbox(t, database, ctx)
	if row.DeliveredAt == nil {
		t.Fatal("row was not marked delivered")
	}
	if row.Attempts != 0 || row.LastError != "" {
		t.Fatalf("row = attempts %d last_error %q", row.Attempts, row.LastError)
	}
	if depth, err := svc.OutboxDepth(ctx); err != nil || depth != 0 {
		t.Fatalf("depth = %d, err = %v; want 0, nil", depth, err)
	}
}

func TestOutboxWorkerDeliversPush(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	if _, err := database.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetPushEnabled(true).
		Save(ctx); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	sub, err := database.Client.PushSubscription.Create().
		SetUserID(u.ID).
		SetEndpoint("https://push.example/a").
		SetP256dh("p256dh").
		SetAuth("auth").
		Save(ctx)
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	oldHook := pushDeliveryHook
	var gotSubs []*ent.PushSubscription
	var gotTitle, gotBody, gotURL string
	pushDeliveryHook = func(_ context.Context, subs []*ent.PushSubscription, title, body, url string) error {
		gotSubs = subs
		gotTitle, gotBody, gotURL = title, body, url
		return nil
	}
	t.Cleanup(func() { pushDeliveryHook = oldHook })

	event := Event{UserID: u.ID, Kind: EventBadge, Text: "Earned a badge", URL: "https://sodapop.test/badges"}
	if err := svc.Dispatch(ctx, event); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	svc.processOutbox(ctx)

	if len(gotSubs) != 1 || gotSubs[0].ID != sub.ID {
		t.Fatalf("push subscriptions = %+v, want only %d", gotSubs, sub.ID)
	}
	if gotTitle != eventTitle(event.Kind) || gotBody != event.Text || gotURL != event.URL {
		t.Fatalf("push payload = %q %q %q", gotTitle, gotBody, gotURL)
	}
	row := onlyOutbox(t, database, ctx)
	if row.DeliveredAt == nil {
		t.Fatal("push row was not marked delivered")
	}
}

func TestOutboxRetryBackoff(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()

	if err := svc.Save(ctx, u.ID, "home_assistant", server.URL, "", true); err != nil {
		t.Fatalf("save integration: %v", err)
	}
	if err := svc.Dispatch(ctx, Event{UserID: u.ID, Kind: EventCheckin, Text: "x"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	before := time.Now().UTC()
	svc.processOutbox(ctx)

	row := onlyOutbox(t, database, ctx)
	if row.DeliveredAt != nil {
		t.Fatal("failed row was marked delivered")
	}
	if row.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", row.Attempts)
	}
	if row.LastError == "" {
		t.Fatal("last_error is empty")
	}
	next, err := time.Parse(time.RFC3339, row.NextAttemptAt)
	if err != nil {
		t.Fatalf("parse next_attempt_at %q: %v", row.NextAttemptAt, err)
	}
	if next.Before(before.Add(time.Minute)) || next.After(before.Add(3*time.Minute)) {
		t.Fatalf("next_attempt_at = %s, want about 2 minutes after %s", next, before)
	}

	// A second poll before the backoff elapses must leave the row alone.
	svc.processOutbox(ctx)
	row = onlyOutbox(t, database, ctx)
	if row.Attempts != 1 {
		t.Fatalf("attempts after early poll = %d, want 1", row.Attempts)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("webhook calls = %d, want 1", got)
	}
}

func TestOutboxStopsAfterMaxAttempts(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()

	if err := svc.Save(ctx, u.ID, "home_assistant", server.URL, "", true); err != nil {
		t.Fatalf("save integration: %v", err)
	}
	target, err := database.Client.Integration.Query().
		Where(integration.UserID(u.ID), integration.KindEQ(integration.KindHomeAssistant)).
		Only(ctx)
	if err != nil {
		t.Fatalf("load integration: %v", err)
	}
	payload, _ := json.Marshal(Event{UserID: u.ID, Kind: EventCheckin, Text: "x"})
	row, err := database.Client.DeliveryOutbox.Create().
		SetUserID(u.ID).
		SetKind(deliveryoutbox.KindWebhook).
		SetTarget(strconv.Itoa(target.ID)).
		SetPayloadJSON(string(payload)).
		SetAttempts(outboxMaxAttempts - 1).
		SetNextAttemptAt(now()).
		SetCreatedAt(now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("seed outbox: %v", err)
	}

	svc.processOutbox(ctx)

	row = reloadOutbox(t, database, ctx, row.ID)
	if row.Attempts != outboxMaxAttempts {
		t.Fatalf("attempts = %d, want %d", row.Attempts, outboxMaxAttempts)
	}
	if row.DeliveredAt != nil {
		t.Fatal("exhausted row was marked delivered")
	}
	if row.LastError == "" {
		t.Fatal("exhausted row has no last_error")
	}

	// Even if the row is due again, the worker must not retry it.
	if _, err := database.Client.DeliveryOutbox.UpdateOneID(row.ID).
		SetNextAttemptAt(now()).Save(ctx); err != nil {
		t.Fatalf("reset next attempt: %v", err)
	}
	svc.processOutbox(ctx)
	row = reloadOutbox(t, database, ctx, row.ID)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("webhook calls = %d, want 1", got)
	}
	if row.Attempts != outboxMaxAttempts {
		t.Fatalf("attempts after exhausting = %d, want %d", row.Attempts, outboxMaxAttempts)
	}
}

func TestOutboxMissingTargetIsPermanent(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	payload, _ := json.Marshal(Event{UserID: u.ID, Kind: EventCheckin, Text: "x"})
	row, err := database.Client.DeliveryOutbox.Create().
		SetUserID(u.ID).
		SetKind(deliveryoutbox.KindWebhook).
		SetTarget("424242").
		SetPayloadJSON(string(payload)).
		SetNextAttemptAt(now()).
		SetCreatedAt(now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("seed outbox: %v", err)
	}

	svc.processOutbox(ctx)

	row = reloadOutbox(t, database, ctx, row.ID)
	if row.Attempts != outboxMaxAttempts {
		t.Fatalf("attempts = %d, want %d", row.Attempts, outboxMaxAttempts)
	}
	if row.DeliveredAt != nil {
		t.Fatal("missing target was marked delivered")
	}
	if row.LastError == "" {
		t.Fatal("missing target has no last_error")
	}
}

func TestOutboxCancelledContextDoesNotDeliver(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	payload, _ := json.Marshal(Event{UserID: u.ID, Kind: EventCheckin, Text: "x"})
	row, err := database.Client.DeliveryOutbox.Create().
		SetUserID(u.ID).
		SetKind(deliveryoutbox.KindWebhook).
		SetTarget("424242").
		SetPayloadJSON(string(payload)).
		SetNextAttemptAt(now()).
		SetCreatedAt(now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("seed outbox: %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	svc.processOutbox(cancelled)
	row = reloadOutbox(t, database, ctx, row.ID)
	if row.Attempts != 0 || row.DeliveredAt != nil {
		t.Fatalf("cancelled poll changed row: attempts=%d delivered=%v", row.Attempts, row.DeliveredAt)
	}

	done := make(chan struct{})
	go func() {
		svc.RunOutbox(cancelled)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunOutbox did not return after cancellation")
	}
}

func TestOutboxDepthCountsUndelivered(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	if depth, err := svc.OutboxDepth(ctx); err != nil || depth != 0 {
		t.Fatalf("empty depth = %d, err = %v", depth, err)
	}
	if err := svc.Save(ctx, u.ID, "trmnl", "https://trmnl.example/hook", "", true); err != nil {
		t.Fatalf("save integration: %v", err)
	}
	if err := svc.Dispatch(ctx, Event{UserID: u.ID, Kind: EventCheckin, Text: "x"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if depth, err := svc.OutboxDepth(ctx); err != nil || depth != 1 {
		t.Fatalf("pending depth = %d, err = %v; want 1", depth, err)
	}
	row := onlyOutbox(t, database, ctx)
	if _, err := database.Client.DeliveryOutbox.UpdateOneID(row.ID).
		SetDeliveredAt(now()).Save(ctx); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if depth, err := svc.OutboxDepth(ctx); err != nil || depth != 0 {
		t.Fatalf("delivered depth = %d, err = %v; want 0", depth, err)
	}
}

func onlyOutbox(t *testing.T, database *db.DB, ctx context.Context) *ent.DeliveryOutbox {
	t.Helper()
	rows, err := database.Client.DeliveryOutbox.Query().All(ctx)
	if err != nil {
		t.Fatalf("load outbox: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("outbox rows = %d, want 1", len(rows))
	}
	return rows[0]
}

func reloadOutbox(t *testing.T, database *db.DB, ctx context.Context, id int) *ent.DeliveryOutbox {
	t.Helper()
	row, err := database.Client.DeliveryOutbox.Get(ctx, id)
	if err != nil {
		t.Fatalf("reload outbox %d: %v", id, err)
	}
	return row
}
