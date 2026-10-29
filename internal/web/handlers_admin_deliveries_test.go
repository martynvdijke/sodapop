package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/deliveryoutbox"
	"sodapop/ent/user"
	"sodapop/internal/integrations"
)

func newDeliveriesHarness(t *testing.T) *v12Harness {
	t.Helper()
	h := newV12Harness(t)
	h.srv.Integrations = integrations.New(h.db, h.srv.Cfg, h.srv.Log)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	return h
}

func deliveriesMux(h *v12Harness) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/deliveries", h.srv.requireAdminStrict(h.srv.deliveriesPage))
	mux.HandleFunc("POST /admin/deliveries/{id}/retry", h.srv.requireAdminStrict(h.srv.deliveriesRetry))
	return mux
}

// seedOutboxRow inserts a delivery row for the user with an explicit state.
func seedOutboxRow(t *testing.T, h *v12Harness, u *ent.User, text string, attempts int, delivered bool, createdAt string) *ent.DeliveryOutbox {
	t.Helper()
	ctx := context.Background()
	payload, err := json.Marshal(integrations.Event{UserID: u.ID, Kind: integrations.EventCheckin, Text: text, URL: "https://sodapop.test/you"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	builder := h.db.Client.DeliveryOutbox.Create().
		SetUserID(u.ID).
		SetKind(deliveryoutbox.KindWebhook).
		SetTarget("1").
		SetPayloadJSON(string(payload)).
		SetAttempts(attempts).
		SetNextAttemptAt(createdAt).
		SetCreatedAt(createdAt)
	if attempts > 0 {
		builder.SetLastError("webhook returned 500")
	}
	if delivered {
		builder.SetDeliveredAt(createdAt)
	}
	row, err := builder.Save(ctx)
	if err != nil {
		t.Fatalf("seed outbox: %v", err)
	}
	return row
}

func TestAdminDeliveriesPageVisibleToAdminOnly(t *testing.T) {
	h := newDeliveriesHarness(t)
	admin := h.verifiedUser(t, "delivery-admin", user.RoleAdmin)
	seedOutboxRow(t, h, admin, "Drank a cola", 5, false, "2026-01-01T00:00:04Z")
	mux := deliveriesMux(h)

	req := h.request(http.MethodGet, "/admin/deliveries", admin, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Drank a cola", "abandoned", "delivery-admin", "Retry"} {
		if !strings.Contains(body, want) {
			t.Fatalf("page missing %q: %s", want, body)
		}
	}

	moderator := h.verifiedUser(t, "delivery-mod", user.RoleModerator)
	req = h.request(http.MethodGet, "/admin/deliveries", moderator, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("moderator status = %d, want 403", rec.Code)
	}

	req = h.request(http.MethodGet, "/admin/deliveries", nil, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("anonymous status = %d, want redirect", rec.Code)
	}
}

func TestAdminDeliveriesStatusFilter(t *testing.T) {
	h := newDeliveriesHarness(t)
	admin := h.verifiedUser(t, "delivery-filter", user.RoleAdmin)
	seedOutboxRow(t, h, admin, "delivered text", 0, true, "2026-01-01T00:00:01Z")
	seedOutboxRow(t, h, admin, "abandoned text", 5, false, "2026-01-01T00:00:02Z")
	mux := deliveriesMux(h)

	req := h.request(http.MethodGet, "/admin/deliveries?status=delivered", admin, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "delivered text") || strings.Contains(body, "abandoned text") {
		t.Fatalf("filter returned the wrong rows: %s", body)
	}
}

func TestAdminDeliveriesRetryRequiresCSRF(t *testing.T) {
	h := newDeliveriesHarness(t)
	admin := h.verifiedUser(t, "delivery-csrf", user.RoleAdmin)
	row := seedOutboxRow(t, h, admin, "retry me", 5, false, "2026-01-01T00:00:01Z")
	mux := deliveriesMux(h)

	req := h.request(http.MethodPost, "/admin/deliveries/"+strconv.Itoa(row.ID)+"/retry", admin, nil)
	req.SetPathValue("id", strconv.Itoa(row.ID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAdminDeliveriesRetryAbandoned(t *testing.T) {
	h := newDeliveriesHarness(t)
	admin := h.verifiedUser(t, "delivery-retry", user.RoleAdmin)
	row := seedOutboxRow(t, h, admin, "retry me", 5, false, "2026-01-01T00:00:01Z")
	mux := deliveriesMux(h)

	form := h.form()
	form.Set("status", "abandoned")
	req := h.request(http.MethodPost, "/admin/deliveries/"+strconv.Itoa(row.ID)+"/retry", admin, form)
	req.SetPathValue("id", strconv.Itoa(row.ID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/deliveries?retry=ok&status=abandoned" {
		t.Fatalf("Location = %q", loc)
	}

	got, err := h.db.Client.DeliveryOutbox.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("reload row: %v", err)
	}
	if got.Attempts != 0 || got.LastError != "" || got.DeliveredAt != nil {
		t.Fatalf("row not reset: attempts=%d last_error=%q delivered=%v", got.Attempts, got.LastError, got.DeliveredAt)
	}
}

func TestAdminDeliveriesRetryNonAbandonable(t *testing.T) {
	h := newDeliveriesHarness(t)
	admin := h.verifiedUser(t, "delivery-no", user.RoleAdmin)
	delivered := seedOutboxRow(t, h, admin, "already delivered", 0, true, "2026-01-01T00:00:01Z")
	mux := deliveriesMux(h)

	tests := []struct {
		name   string
		id     int
		status string
		want   string
	}{
		{"delivered row", delivered.ID, "", "/admin/deliveries?retry=not_retryable"},
		{"unknown id", 999999, "abandoned", "/admin/deliveries?retry=not_retryable&status=abandoned"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := h.form()
			if tt.status != "" {
				form.Set("status", tt.status)
			}
			req := h.request(http.MethodPost, "/admin/deliveries/"+strconv.Itoa(tt.id)+"/retry", admin, form)
			req.SetPathValue("id", strconv.Itoa(tt.id))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != tt.want {
				t.Fatalf("Location = %q, want %q", loc, tt.want)
			}
		})
	}

	got, err := h.db.Client.DeliveryOutbox.Get(context.Background(), delivered.ID)
	if err != nil {
		t.Fatalf("reload delivered: %v", err)
	}
	if got.DeliveredAt == nil {
		t.Fatal("delivered row lost its delivered_at")
	}
}
