package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sodapop/ent/user"
	"sodapop/internal/auth"
	"sodapop/internal/badges"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/integrations"
	"sodapop/internal/ratelimit"
	"sodapop/internal/social"
)

type hookHarness struct {
	srv       *Server
	mux       *http.ServeMux
	userID    string
	drinkSlug string
}

func newHookHarness(t *testing.T) *hookHarness {
	t.Helper()
	cfg := &config.Config{
		AppName:         "Sodapop",
		Env:             "test",
		BaseURL:         "http://localhost",
		Version:         "test",
		SessionKey:      []byte("0123456789abcdef0123456789abcdef"),
		SessionTTL:      time.Hour,
		BcryptCost:      10,
		RateLimitPerMin: 120,
	}
	database, err := db.Init(filepath.Join(t.TempDir(), "hooks.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	authSvc := auth.New(cfg, database)
	catalogSvc := catalog.New(cfg, database)
	socialSvc := social.New(database)
	checkinSvc := checkins.New(database, socialSvc)

	srv := &Server{
		Cfg:          cfg,
		Auth:         authSvc,
		Catalog:      catalogSvc,
		Checkins:     checkinSvc,
		Social:       socialSvc,
		Badges:       badges.New(database),
		Integrations: integrations.New(database, cfg, log),
		DB:           database,
		Log:          log,
		Limiter:      ratelimit.New(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hooks/ha/{token}", srv.haInbound)

	ctx := context.Background()
	user, err := database.Client.User.Create().
		SetHandle("homelab").
		SetEmail("homelab@example.com").
		SetDisplayName("Homelab").
		Save(ctx)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	drink, err := database.Client.Drink.Create().
		SetName("Test Cola").
		SetSlug("test-cola").
		SetStatus("active").
		SetVolumeMl(355).
		Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	return &hookHarness{srv: srv, mux: mux, userID: user.ID, drinkSlug: drink.Slug}
}

func (h *hookHarness) token(t *testing.T) string {
	t.Helper()
	raw, _, err := h.srv.Integrations.CreateInboundToken(context.Background(), h.userID, "test")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return raw
}

func (h *hookHarness) post(t *testing.T, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/hooks/ha/"+token, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func (h *hookHarness) checkinCount(t *testing.T) int {
	t.Helper()
	n, err := h.srv.DB.Client.Checkin.Query().Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestHAInboundRejectsBadTokenGenerically(t *testing.T) {
	h := newHookHarness(t)
	rec := h.post(t, "not-a-real-token", `{"slug":"test-cola"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "not-a-real-token") {
		t.Fatal("response leaked the token")
	}
	if got := rec.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("webhook set a cookie: %q", got)
	}
}

func TestHAInboundCreatesAndReplaysCheckin(t *testing.T) {
	h := newHookHarness(t)
	token := h.token(t)

	first := h.post(t, token, `{"slug":"test-cola","rating":16,"note":"from HA"}`, map[string]string{"Idempotency-Key": "ha-1"})
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, body %s", first.Code, first.Body.String())
	}
	var payload struct {
		Data struct {
			CheckinID string `json:"checkin_id"`
			Drink     struct {
				Slug string `json:"slug"`
			} `json:"drink"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Data.CheckinID == "" || payload.Data.Drink.Slug != "test-cola" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if got := h.checkinCount(t); got != 1 {
		t.Fatalf("checkins = %d, want 1", got)
	}

	replay := h.post(t, token, `{"slug":"test-cola","rating":16,"note":"from HA"}`, map[string]string{"Idempotency-Key": "ha-1"})
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body %s", replay.Code, replay.Body.String())
	}
	var replayPayload struct {
		Data struct {
			CheckinID string `json:"checkin_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayPayload); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replayPayload.Data.CheckinID != payload.Data.CheckinID {
		t.Fatalf("replay id = %q, want %q", replayPayload.Data.CheckinID, payload.Data.CheckinID)
	}
	if got := h.checkinCount(t); got != 1 {
		t.Fatalf("checkins after replay = %d, want 1", got)
	}
}

func TestHAInboundValidatesInput(t *testing.T) {
	h := newHookHarness(t)
	token := h.token(t)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"both identifiers", `{"slug":"test-cola","gtin":"5449000000996"}`, http.StatusBadRequest},
		{"no identifier", `{"note":"nope"}`, http.StatusBadRequest},
		{"rating too high", `{"slug":"test-cola","rating":21}`, http.StatusBadRequest},
		{"unknown slug", `{"slug":"does-not-exist"}`, http.StatusNotFound},
		{"unknown field", `{"slug":"test-cola","sneaky":true}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.post(t, token, tc.body, nil)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestHAInboundRateLimit(t *testing.T) {
	h := newHookHarness(t)
	token := h.token(t)
	body := `{"slug":"test-cola"}`
	var last int
	for i := 0; i < 11; i++ {
		last = h.post(t, token, body, map[string]string{"Idempotency-Key": "burst"}).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("11th status = %d, want 429", last)
	}
}

func TestDeleteAccountRevokesInboundTokens(t *testing.T) {
	h := newHookHarness(t)
	ctx := context.Background()
	raw, _, err := h.srv.Integrations.CreateInboundToken(ctx, h.userID, "test")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := h.srv.Integrations.AuthenticateInbound(ctx, raw); err != nil {
		t.Fatalf("token should work before deletion: %v", err)
	}

	if err := h.srv.deleteAccount(ctx, h.userID); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	tok, err := h.srv.DB.Client.InboundToken.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query token: %v", err)
	}
	if tok.RevokedAt == nil {
		t.Fatal("inbound token still active after account deletion")
	}
	if _, err := h.srv.Integrations.AuthenticateInbound(ctx, raw); !errors.Is(err, integrations.ErrInvalidInboundToken) {
		t.Fatalf("authenticate after delete: got %v, want ErrInvalidInboundToken", err)
	}
	u, err := h.srv.Auth.Client.User.Get(ctx, h.userID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.Status != user.StatusDeleted || u.DeletedAt == nil {
		t.Fatalf("user not soft-deleted: status=%s deleted_at=%v", u.Status, u.DeletedAt)
	}
}

func TestAuthenticateInboundRejectsSuspendedOwner(t *testing.T) {
	h := newHookHarness(t)
	ctx := context.Background()
	raw, _, err := h.srv.Integrations.CreateInboundToken(ctx, h.userID, "test")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if err := h.srv.Auth.Client.User.UpdateOneID(h.userID).
		SetStatus(user.StatusSuspended).
		Exec(ctx); err != nil {
		t.Fatalf("suspend user: %v", err)
	}
	if _, err := h.srv.Integrations.AuthenticateInbound(ctx, raw); !errors.Is(err, integrations.ErrInvalidInboundToken) {
		t.Fatalf("authenticate suspended owner: got %v, want ErrInvalidInboundToken", err)
	}
	tok, err := h.srv.DB.Client.InboundToken.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query token: %v", err)
	}
	if tok.LastUsedAt != nil {
		t.Fatalf("rejected token recorded use at %v", *tok.LastUsedAt)
	}
}
