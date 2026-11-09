package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sodapop/internal/pairings"
)

// drinkPageRequest builds the request the requireUser middleware would hand to
// drinkPage: a signed-in member plus the routed slug.
func drinkPageRequest(h *v12Harness, t *testing.T, slug string) *http.Request {
	t.Helper()
	u := h.user(t, "taster")
	req := httptest.NewRequest(http.MethodGet, "/drinks/"+slug, nil)
	req.SetPathValue("slug", slug)
	ctx := context.WithValue(req.Context(), userKey{}, u)
	return req.WithContext(ctx)
}

func TestDrinkPageShowsSandwichPairings(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/recipes" {
			t.Errorf("unexpected upstream path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":1,"title":"Cherry Cheese Melt","slug":"cherry-cheese-melt","description":"Sharp cheddar and cherry jam.","image":"/media/recipes/melt.jpg"}],"total":1}`))
	}))
	defer upstream.Close()

	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	h.srv.Pairings = pairings.New(upstream.URL)
	if _, err := h.db.Client.Drink.Create().SetName("Cherry Cola").SetSlug("cherry-cola").Save(context.Background()); err != nil {
		t.Fatalf("drink: %v", err)
	}

	rec := httptest.NewRecorder()
	h.srv.drinkPage(rec, drinkPageRequest(h, t, "cherry-cola"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Pairs with", "Cherry Cheese Melt", upstream.URL + "/recipes/cherry-cheese-melt", upstream.URL + "/media/recipes/melt.jpg"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestDrinkPageOmitsPairingsWhenDisabled(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	h.srv.Pairings = pairings.New("")
	if _, err := h.db.Client.Drink.Create().SetName("Cherry Cola").SetSlug("cherry-cola").Save(context.Background()); err != nil {
		t.Fatalf("drink: %v", err)
	}

	rec := httptest.NewRecorder()
	h.srv.drinkPage(rec, drinkPageRequest(h, t, "cherry-cola"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, "Pairs with") {
		t.Fatalf("disabled feature rendered a pairings heading")
	}
}
