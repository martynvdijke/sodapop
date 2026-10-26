package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"sodapop/ent/drink"
)

func TestAPISearchDefaultStaysDrinks(t *testing.T) {
	h := newHarness(t)
	seedSearchCatalog(t, h, 1, 1, 1)

	rec := h.do(t, http.MethodGet, "/api/v1/search?q=cola", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Data) != 1 {
		t.Fatalf("drinks = %d, want 1", len(payload.Data))
	}
	if strings.Contains(rec.Body.String(), `"brands"`) {
		t.Fatalf("default search leaked grouped brands: %s", rec.Body.String())
	}
}

func TestAPISearchAllGroupsCappedWithoutTotals(t *testing.T) {
	h := newHarness(t)
	seedSearchCatalog(t, h, 6, 5, 5)

	rec := h.do(t, http.MethodGet, "/api/v1/search?q=cola&type=all", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("search all: %d %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data struct {
			Drinks []map[string]any `json:"drinks"`
			Brands []map[string]any `json:"brands"`
			Styles []map[string]any `json:"styles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := len(payload.Data.Drinks); got != 4 {
		t.Errorf("drinks = %d, want 4", got)
	}
	if got := len(payload.Data.Brands); got != 3 {
		t.Errorf("brands = %d, want 3", got)
	}
	if got := len(payload.Data.Styles); got != 3 {
		t.Errorf("styles = %d, want 3", got)
	}
	if strings.Contains(rec.Body.String(), `"total"`) {
		t.Fatalf("search leaks a total count: %s", rec.Body.String())
	}
}

func TestAPISearchAllRequiresAuthAndScope(t *testing.T) {
	h := newHarness(t)
	seedSearchCatalog(t, h, 1, 1, 1)

	rec := h.do(t, http.MethodGet, "/api/v1/search?q=cola&type=all", "", false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: got %d, want 401", rec.Code)
	}

	readOnly, _, err := h.server.Auth.CreateAPIToken(context.Background(), h.alice, "write-only", []string{"write"})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	rec = h.doWith(t, readOnly, http.MethodGet, "/api/v1/search?q=cola&type=all", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("insufficient scope: got %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
}

func TestAPISearchAllShortQueryRejected(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodGet, "/api/v1/search?q=a&type=all", "", true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short query: got %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func seedSearchCatalog(t *testing.T, h *harness, drinks, brands, styles int) {
	t.Helper()
	ctx := context.Background()
	b, err := h.server.DB.Client.Brand.Create().SetName("Cola Company").SetSlug("cola-company").Save(ctx)
	if err != nil {
		t.Fatalf("brand: %v", err)
	}
	st, err := h.server.DB.Client.Style.Create().SetName("Cola Family").SetSlug("cola-family").Save(ctx)
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	for i := 0; i < drinks; i++ {
		if _, err := h.server.DB.Client.Drink.Create().
			SetName("Cola " + string(rune('A'+i))).SetSlug("cola-" + string(rune('a'+i))).
			SetStatus(drink.StatusActive).SetBrandID(b.ID).SetStyleID(st.ID).Save(ctx); err != nil {
			t.Fatalf("drink: %v", err)
		}
	}
	for i := 1; i < brands; i++ {
		if _, err := h.server.DB.Client.Brand.Create().
			SetName("Cola Brand " + string(rune('A'+i))).SetSlug("cola-brand-" + string(rune('a'+i))).Save(ctx); err != nil {
			t.Fatalf("brand: %v", err)
		}
	}
	for i := 1; i < styles; i++ {
		if _, err := h.server.DB.Client.Style.Create().
			SetName("Cola Style " + string(rune('A'+i))).SetSlug("cola-style-" + string(rune('a'+i))).Save(ctx); err != nil {
			t.Fatalf("style: %v", err)
		}
	}
}
