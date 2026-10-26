package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"sodapop/ent/checkin"
	"sodapop/internal/db"
)

func blockMux(h *harness) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/users/{handle}/block", h.server.protect("write", true, h.server.blockUser))
	mux.HandleFunc("DELETE /api/v1/users/{handle}/block", h.server.protect("write", true, h.server.unblockUser))
	return mux
}

func (h *harness) blockRequest(mux *http.ServeMux, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestBlockAndUnblockAPI(t *testing.T) {
	h := newHarness(t)
	mux := blockMux(h)

	rec := h.blockRequest(mux, http.MethodPost, "/api/v1/users/bob/block", h.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("block status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	n, err := h.server.DB.Client.Block.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("blocks = %d, want 1", n)
	}

	// Repeat is idempotent.
	rec = h.blockRequest(mux, http.MethodPost, "/api/v1/users/bob/block", h.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat block status = %d, want 200", rec.Code)
	}
	if n, _ = h.server.DB.Client.Block.Query().Count(context.Background()); n != 1 {
		t.Fatalf("repeat block rows = %d, want 1", n)
	}

	rec = h.blockRequest(mux, http.MethodDelete, "/api/v1/users/bob/block", h.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("unblock status = %d, want 200", rec.Code)
	}
	if n, _ = h.server.DB.Client.Block.Query().Count(context.Background()); n != 0 {
		t.Fatalf("blocks after unblock = %d, want 0", n)
	}
}

func TestBlockAPI404ForUnknownAndSelf(t *testing.T) {
	h := newHarness(t)
	mux := blockMux(h)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/users/nobody/block"},
		{http.MethodPost, "/api/v1/users/alice/block"},
		{http.MethodDelete, "/api/v1/users/nobody/block"},
		{http.MethodDelete, "/api/v1/users/alice/block"},
	} {
		rec := h.blockRequest(mux, tc.method, tc.path, h.token)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: got %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
	if n, _ := h.server.DB.Client.Block.Query().Count(context.Background()); n != 0 {
		t.Fatalf("404 paths wrote blocks: %d", n)
	}
}

func TestBlockedUserReadsReturn404(t *testing.T) {
	h := newHarness(t)
	if err := h.server.Social.Block(context.Background(), h.alice, "bob"); err != nil {
		t.Fatalf("block: %v", err)
	}
	for _, path := range []string{"/api/v1/users/bob", "/api/v1/users/bob/checkins"} {
		rec := h.do(t, http.MethodGet, path, "", true)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", path, rec.Code)
		}
	}
}

func TestBlockAPIRequiresWriteScopeAndVerifiedEmail(t *testing.T) {
	h := newHarness(t)
	mux := blockMux(h)
	ctx := context.Background()

	// A read-only token is refused with 403 before the handler runs.
	readToken, _, err := h.server.Auth.CreateAPIToken(ctx, h.alice, "read", []string{"read", "feed"})
	if err != nil {
		t.Fatal(err)
	}
	rec := h.blockRequest(mux, http.MethodPost, "/api/v1/users/bob/block", readToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read-only block status = %d, want 403", rec.Code)
	}

	// Unverified users cannot write.
	if err := h.server.DB.Client.User.UpdateOneID(h.alice).
		ClearEmailVerifiedAt().SetUpdatedAt(db.Now()).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	rec = h.blockRequest(mux, http.MethodPost, "/api/v1/users/bob/block", h.token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unverified block status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if n, _ := h.server.DB.Client.Block.Query().Count(ctx); n != 0 {
		t.Fatalf("forbidden requests wrote blocks: %d", n)
	}
}

// There is no API follow route: following is a web-only action. A blocked pair
// must therefore never get a 403 from the API surface, and no row may appear.
func TestFollowAPIDoesNotForbiddenOrCreateRows(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.server.Social.Block(ctx, h.bob, "alice"); err != nil {
		t.Fatalf("block: %v", err)
	}
	rec := h.do(t, http.MethodPost, "/api/v1/users/bob/follow", "", true)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("follow API leaked a 403 for a blocked pair: %s", rec.Body.String())
	}
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("follow API status = %d, want 404/405 (no follow route)", rec.Code)
	}
	n, err := h.server.DB.Client.Follow.Query().Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("follow rows after API attempt = %d, want 0", n)
	}
}

func TestCheckinAPIGet404sForBlockedViewer(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	d, err := h.server.DB.Client.Drink.Create().
		SetName("Bloc Cola").SetSlug("bloc-cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	c, err := h.server.DB.Client.Checkin.Create().
		SetUserID(h.bob).SetDrinkID(d.ID).
		SetVisibility(checkin.VisibilityPublic).Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}

	path := "/api/v1/checkins/" + c.ID
	if rec := h.do(t, http.MethodGet, path, "", true); rec.Code != http.StatusOK {
		t.Fatalf("unblocked read = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if _, err := h.server.DB.Client.Block.Create().
		SetBlockerID(h.alice).SetBlockedID(h.bob).
		SetCreatedAt(db.Now()).Save(ctx); err != nil {
		t.Fatalf("block: %v", err)
	}
	rec := h.do(t, http.MethodGet, path, "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("blocked read = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
