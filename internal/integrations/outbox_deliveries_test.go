package integrations

import (
	"context"
	"encoding/json"
	"testing"

	"sodapop/ent/deliveryoutbox"
	"sodapop/internal/db"
)

func deliveryHistoryFixture(t *testing.T) (*Service, *db.DB, context.Context, map[string]int) {
	t.Helper()
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	orphan := "00112233445566778899aabbccddeeff"

	event, err := json.Marshal(Event{UserID: u.ID, Kind: EventCheckin, Text: "Drank a cola", URL: "https://sodapop.test/you"})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	base := struct {
		userID      string
		kind        deliveryoutbox.Kind
		target      string
		payload     string
		attempts    int
		lastError   string
		createdAt   string
		deliveredAt string
	}{
		userID: u.ID, kind: deliveryoutbox.KindWebhook, target: "1",
		payload: string(event), createdAt: "2026-01-01T00:00:00Z",
	}

	delivered := base
	delivered.createdAt = "2026-01-01T00:00:01Z"
	delivered.deliveredAt = "2026-01-01T00:01:00Z"
	deliveredID := seedDeliveryID(t, database, ctx, delivered)

	pending := base
	pending.createdAt = "2026-01-01T00:00:02Z"
	pendingID := seedDeliveryID(t, database, ctx, pending)

	retrying := base
	retrying.createdAt = "2026-01-01T00:00:03Z"
	retrying.attempts = 2
	retrying.lastError = "temporary failure"
	retryingID := seedDeliveryID(t, database, ctx, retrying)

	abandoned := base
	abandoned.createdAt = "2026-01-01T00:00:04Z"
	abandoned.attempts = outboxMaxAttempts
	abandoned.lastError = "gave up"
	abandonedID := seedDeliveryID(t, database, ctx, abandoned)

	orphanRow := base
	orphanRow.userID = orphan
	orphanRow.createdAt = "2026-01-01T00:00:05Z"
	orphanRow.payload = "{not json"
	orphanID := seedDeliveryID(t, database, ctx, orphanRow)

	return svc, database, ctx, map[string]int{
		DeliveryStatusDelivered: deliveredID,
		DeliveryStatusPending:   pendingID,
		DeliveryStatusRetrying:  retryingID,
		DeliveryStatusAbandoned: abandonedID,
		"orphan":                orphanID,
	}
}

func seedDeliveryID(t *testing.T, database *db.DB, ctx context.Context, spec struct {
	userID      string
	kind        deliveryoutbox.Kind
	target      string
	payload     string
	attempts    int
	lastError   string
	createdAt   string
	deliveredAt string
}) int {
	t.Helper()
	builder := database.Client.DeliveryOutbox.Create().
		SetUserID(spec.userID).
		SetKind(spec.kind).
		SetTarget(spec.target).
		SetPayloadJSON(spec.payload).
		SetAttempts(spec.attempts).
		SetNextAttemptAt(now()).
		SetLastError(spec.lastError).
		SetCreatedAt(spec.createdAt)
	if spec.deliveredAt != "" {
		builder.SetDeliveredAt(spec.deliveredAt)
	}
	row, err := builder.Save(ctx)
	if err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	return row.ID
}

func TestListDeliveriesClassifiesAndFilters(t *testing.T) {
	svc, _, ctx, ids := deliveryHistoryFixture(t)

	rows, total, err := svc.ListDeliveries(ctx, "", 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 5 || len(rows) != 5 {
		t.Fatalf("total = %d, rows = %d; want 5, 5", total, len(rows))
	}

	byID := map[int]DeliveryRecord{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	for status, id := range ids {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing row %d (%s)", id, status)
		}
		if status != "orphan" && row.Status != status {
			t.Errorf("row %d status = %q, want %q", id, row.Status, status)
		}
	}
	if got := byID[ids[DeliveryStatusDelivered]].DeliveredAt; got == nil {
		t.Error("delivered row has no delivered_at")
	}
	if got := byID[ids[DeliveryStatusPending]].DeliveredAt; got != nil {
		t.Error("pending row reports delivered_at")
	}
	if got := byID[ids[DeliveryStatusDelivered]].Text; got != "Drank a cola" {
		t.Errorf("delivered text = %q", got)
	}
	if got := byID[ids[DeliveryStatusDelivered]].URL; got != "https://sodapop.test/you" {
		t.Errorf("delivered url = %q", got)
	}
	if got := byID[ids["orphan"]]; got.Text != "" || got.URL != "" {
		t.Errorf("malformed payload leaked text %q url %q", got.Text, got.URL)
	}

	// Newest first by created_at DESC.
	if rows[0].ID != ids["orphan"] {
		t.Errorf("first row = %d, want newest %d", rows[0].ID, ids["orphan"])
	}

	filters := map[string][]int{
		DeliveryStatusDelivered: {ids[DeliveryStatusDelivered]},
		DeliveryStatusPending:   {ids[DeliveryStatusPending], ids["orphan"]},
		DeliveryStatusRetrying:  {ids[DeliveryStatusRetrying]},
		DeliveryStatusAbandoned: {ids[DeliveryStatusAbandoned]},
	}
	for status, want := range filters {
		got, total, err := svc.ListDeliveries(ctx, status, 50, 0)
		if err != nil {
			t.Fatalf("list %s: %v", status, err)
		}
		if total != len(want) || len(got) != len(want) {
			t.Fatalf("%s total = %d rows = %d, want %d", status, total, len(got), len(want))
		}
		for _, row := range got {
			if row.Status != status {
				t.Errorf("%s filter returned %q row", status, row.Status)
			}
		}
	}

	// An unknown status behaves like no filter.
	if _, total, err := svc.ListDeliveries(ctx, "bogus", 50, 0); err != nil || total != 5 {
		t.Fatalf("unknown status total = %d, err = %v; want 5", total, err)
	}
}

func TestListDeliveriesPaginationAndHandles(t *testing.T) {
	svc, _, ctx, _ := deliveryHistoryFixture(t)

	page, total, err := svc.ListDeliveries(ctx, "", 2, 0)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if total != 5 || len(page) != 2 {
		t.Fatalf("page 1 total = %d rows = %d, want 5, 2", total, len(page))
	}
	last, total, err := svc.ListDeliveries(ctx, "", 2, 4)
	if err != nil {
		t.Fatalf("last page: %v", err)
	}
	if total != 5 || len(last) != 1 {
		t.Fatalf("last page total = %d rows = %d, want 5, 1", total, len(last))
	}

	// Defaults cap the page size and reject negative offsets.
	rows, _, err := svc.ListDeliveries(ctx, "", 0, -10)
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("default limit rows = %d, want 5", len(rows))
	}
	if rows, _, err := svc.ListDeliveries(ctx, "", 1000, 0); err != nil || len(rows) != 5 {
		t.Fatalf("capped limit rows = %d, err = %v", len(rows), err)
	}

	for _, row := range rows {
		if row.UserHandle == "ada" {
			return
		}
	}
	t.Fatalf("no row resolved the user handle: %+v", rows)
}

func TestDeliveryStatusCounts(t *testing.T) {
	svc, _, ctx, _ := deliveryHistoryFixture(t)

	counts, err := svc.DeliveryStatusCounts(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	want := map[string]int{
		DeliveryStatusDelivered: 1,
		DeliveryStatusPending:   2,
		DeliveryStatusRetrying:  1,
		DeliveryStatusAbandoned: 1,
	}
	for status, n := range want {
		if counts[status] != n {
			t.Errorf("%s = %d, want %d", status, counts[status], n)
		}
	}
}

func TestRetryDeliveryOnlyResetsAbandoned(t *testing.T) {
	svc, database, ctx, ids := deliveryHistoryFixture(t)

	ok, err := svc.RetryDelivery(ctx, ids[DeliveryStatusAbandoned])
	if err != nil {
		t.Fatalf("retry abandoned: %v", err)
	}
	if !ok {
		t.Fatal("abandoned row was not retried")
	}
	row, err := database.Client.DeliveryOutbox.Get(ctx, ids[DeliveryStatusAbandoned])
	if err != nil {
		t.Fatalf("reload abandoned: %v", err)
	}
	if row.Attempts != 0 || row.LastError != "" || row.DeliveredAt != nil {
		t.Fatalf("retried row = attempts %d last_error %q delivered %v", row.Attempts, row.LastError, row.DeliveredAt)
	}

	tests := []struct {
		name string
		id   int
	}{
		{"pending", ids[DeliveryStatusPending]},
		{"retrying", ids[DeliveryStatusRetrying]},
		{"delivered", ids[DeliveryStatusDelivered]},
		{"unknown", 999999},
		{"non-positive", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := svc.RetryDelivery(ctx, tt.id)
			if err != nil {
				t.Fatalf("retry %s: %v", tt.name, err)
			}
			if ok {
				t.Fatalf("retry %s = true, want false", tt.name)
			}
		})
	}

	// The in-flight rows kept their state.
	pending, err := database.Client.DeliveryOutbox.Get(ctx, ids[DeliveryStatusPending])
	if err != nil {
		t.Fatalf("reload pending: %v", err)
	}
	if pending.Attempts != 0 || pending.DeliveredAt != nil {
		t.Fatalf("pending row was modified: %+v", pending)
	}
	retrying, err := database.Client.DeliveryOutbox.Get(ctx, ids[DeliveryStatusRetrying])
	if err != nil {
		t.Fatalf("reload retrying: %v", err)
	}
	if retrying.Attempts != 2 {
		t.Fatalf("retrying attempts = %d, want 2", retrying.Attempts)
	}
	delivered, err := database.Client.DeliveryOutbox.Get(ctx, ids[DeliveryStatusDelivered])
	if err != nil {
		t.Fatalf("reload delivered: %v", err)
	}
	if delivered.DeliveredAt == nil {
		t.Fatal("delivered row lost its delivered_at")
	}
}
