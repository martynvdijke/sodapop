package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/internal/auth"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/comments"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/media"
	"sodapop/internal/ratelimit"
	"sodapop/internal/social"
	"sodapop/internal/venues"
)

type harness struct {
	server *Server
	mux    *http.ServeMux
	alice  string
	bob    string
	token  string
}

func newHarness(t *testing.T) *harness {
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
		DataDir:         t.TempDir(),
	}
	database, err := db.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	authSvc := auth.New(cfg, database)
	if err := authSvc.InitOIDC(context.Background()); err != nil {
		t.Fatalf("oidc init: %v", err)
	}
	catalogSvc := catalog.New(cfg, database)
	socialSvc := social.New(database)
	checkinSvc := checkins.New(database, socialSvc)
	commentSvc := comments.New(database, checkinSvc)
	venueSvc := venues.New(database)
	mediaSvc := media.New(database, cfg)
	limiter := ratelimit.New()

	srv := &Server{
		Cfg: cfg, Auth: authSvc, Catalog: catalogSvc, Checkins: checkinSvc,
		Comments: commentSvc, Social: socialSvc, Venues: venueSvc,
		Limiter: limiter, DB: database, Media: mediaSvc,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	srv.Register(mux)

	ctx := context.Background()
	alice, err := database.Client.User.Create().
		SetHandle("alice").SetEmail("alice@example.com").SetDisplayName("Alice").
		SetEmailVerifiedAt(db.Now()).Save(ctx)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	bob, err := database.Client.User.Create().
		SetHandle("bob").SetEmail("bob@example.com").SetDisplayName("Bob").
		SetEmailVerifiedAt(db.Now()).Save(ctx)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	token, _, err := authSvc.CreateAPIToken(ctx, alice.ID, "test", []string{"read", "write", "feed"})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	return &harness{server: srv, mux: mux, alice: alice.ID, bob: bob.ID, token: token}
}

func (h *harness) do(t *testing.T, method, path, body string, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func TestAPIRefusesAnonymous(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/api/v1/me", "/api/v1/feed", "/api/v1/search?q=cola", "/api/v1/drinks/coca-cola"} {
		rec := h.do(t, http.MethodGet, path, "", false)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: got %d, want 401", path, rec.Code)
		}
	}
}

// TestAPICannotEnumerateCatalog is the regression test for the anti-scraping
// requirement: no collection endpoints exist and search is hard-capped.
func TestAPICannotEnumerateCatalog(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/api/v1/drinks", "/api/v1/users", "/api/v1/venues", "/api/v1/checkins"} {
		rec := h.do(t, http.MethodGet, path, "", true)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: got %d, want 404/405 (collection listing must not exist)", path, rec.Code)
		}
	}

	short := h.do(t, http.MethodGet, "/api/v1/search?q=a", "", true)
	if short.Code != http.StatusBadRequest {
		t.Errorf("short search: got %d, want 400", short.Code)
	}

	ctx := context.Background()
	for i := 0; i < 25; i++ {
		if _, _, err := h.server.Catalog.CreateProvisional(ctx, h.alice, catalog.CreateInput{
			Name: "Cola " + string(rune('A'+i)), Brand: "Test", Container: "can", VolumeML: 355,
		}); err != nil {
			t.Fatalf("seed drink %d: %v", i, err)
		}
	}
	rec := h.do(t, http.MethodGet, "/api/v1/search?q=cola&limit=100", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: got %d body %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Data) > 10 {
		t.Fatalf("search returned %d rows; cap is 10", len(payload.Data))
	}
	if strings.Contains(rec.Body.String(), "total") {
		t.Fatalf("search leaks a total count: %s", rec.Body.String())
	}
}

func TestStrangerCheckinsAreNotFound(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d, _, err := h.server.Catalog.CreateProvisional(ctx, h.alice, catalog.CreateInput{
		Name: "Ginger Beer", Brand: "Test", Container: "bottle", VolumeML: 275,
	})
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	if _, _, err := h.server.Checkins.Create(ctx, mustUser(t, h, h.bob), checkins.CreateInput{
		DrinkID: d.ID, Visibility: "public",
	}); err != nil {
		t.Fatalf("checkin: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/users/bob/checkins", "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger: got %d, want 404", rec.Code)
	}

	f, err := h.server.Social.Request(ctx, h.alice, "bob")
	if err != nil || f == nil {
		t.Fatalf("friend request: %v", err)
	}
	if err := h.server.Social.Accept(ctx, h.bob, strconv.Itoa(f.ID)); err != nil {
		t.Fatalf("accept: %v", err)
	}
	rec = h.do(t, http.MethodGet, "/api/v1/users/bob/checkins", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("friend: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestProfileDoesNotExposeEmail(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodGet, "/api/v1/users/bob", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile: got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "bob@example.com") {
		t.Fatalf("public profile leaks email: %s", rec.Body.String())
	}
}

func TestOpenAPIIsServed(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodGet, "/api/openapi.json", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi: got %d", rec.Code)
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Fatal("openapi.json is not valid JSON")
	}
	docs := h.do(t, http.MethodGet, "/api/docs/", "", false)
	if docs.Code != http.StatusOK {
		t.Fatalf("swagger ui: got %d", docs.Code)
	}
}

func mustUser(t *testing.T, h *harness, id string) *ent.User {
	t.Helper()
	u, err := h.server.Auth.Client.User.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	return u
}
