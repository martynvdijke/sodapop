package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sodapop/ent/drink"
)

func TestSearchPageDefaultStaysDrinksOnly(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	ctx := context.Background()
	b, err := h.db.Client.Brand.Create().SetName("Cola Company").SetSlug("cola-company").Save(ctx)
	if err != nil {
		t.Fatalf("brand: %v", err)
	}
	st, err := h.db.Client.Style.Create().SetName("Cola Family").SetSlug("cola-family").Save(ctx)
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	if _, err := h.db.Client.Drink.Create().
		SetName("Cola Classic").SetSlug("cola-classic").SetStatus(drink.StatusActive).
		SetBrandID(b.ID).SetStyleID(st.ID).Save(ctx); err != nil {
		t.Fatalf("drink: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/search?q=cola", nil)
	h.srv.searchPage(rec, req)
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, body)
	}
	if !strings.Contains(body, "Cola Classic") {
		t.Fatalf("default search missing drink: %s", body)
	}
	if strings.Contains(body, "<h2>Brands</h2>") || strings.Contains(body, "<h2>Styles</h2>") {
		t.Fatalf("default search rendered grouped sections: %s", body)
	}
}

func TestSearchPageAllGroupsBrandsAndStyles(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	ctx := context.Background()
	b, err := h.db.Client.Brand.Create().SetName("Cola Company").SetSlug("cola-company").Save(ctx)
	if err != nil {
		t.Fatalf("brand: %v", err)
	}
	st, err := h.db.Client.Style.Create().SetName("Cola Family").SetSlug("cola-family").Save(ctx)
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	if _, err := h.db.Client.Drink.Create().
		SetName("Cola Classic").SetSlug("cola-classic").SetStatus(drink.StatusActive).
		SetBrandID(b.ID).SetStyleID(st.ID).Save(ctx); err != nil {
		t.Fatalf("drink: %v", err)
	}

	for _, target := range []string{"/search?q=cola&type=all", "/search?q=cola&scope=all"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		h.srv.searchPage(rec, req)
		body := rec.Body.String()

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body %s", target, rec.Code, body)
		}
		for _, want := range []string{"<h2>Drinks</h2>", "<h2>Brands</h2>", "<h2>Styles</h2>",
			`href="/brands/cola-company"`, `href="/styles/cola-family"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", target, want)
			}
		}
	}
}

func TestSearchPageShortQueryKeepsCopy(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/search?q=a&type=all", nil)
	h.srv.searchPage(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(body, "Type at least two characters.") {
		t.Fatalf("missing short-query copy: %s", body)
	}
}
