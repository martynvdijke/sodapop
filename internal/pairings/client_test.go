package pairings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestForDrinkParsesItemsAndAbsolutisesImages(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[
			{"id":1,"title":"Cherry Cheese","slug":"cherry-cheese","description":"A tangy cherry jam and sharp cheddar on rye.","image":"/media/recipes/cherry.jpg"},
			{"id":2,"title":"Plain Cheese","slug":"plain-cheese","description":"","image":"media/recipes/plain.jpg"},
			{"id":3,"title":"No Photo","slug":"no-photo","description":"Toasted.","image":""}
		],"total":3}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	if c == nil || !c.Enabled() {
		t.Fatalf("client should be enabled for %q", srv.URL)
	}
	if c.Base() != srv.URL {
		t.Fatalf("Base() = %q, want %q", c.Base(), srv.URL)
	}

	items := c.ForDrink(context.Background(), "cherry-cola")
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	if gotPath != "/api/v1/recipes" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "drink=cherry-cola") || !strings.Contains(gotQuery, "limit=6") {
		t.Fatalf("query = %q", gotQuery)
	}
	if items[0].Title != "Cherry Cheese" || items[0].Slug != "cherry-cheese" {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[0].ImageURL != srv.URL+"/media/recipes/cherry.jpg" {
		t.Fatalf("root-relative image = %q", items[0].ImageURL)
	}
	if items[1].ImageURL != srv.URL+"/media/recipes/plain.jpg" {
		t.Fatalf("relative image = %q", items[1].ImageURL)
	}
	if items[2].ImageURL != "" {
		t.Fatalf("empty image = %q", items[2].ImageURL)
	}
	if items[0].URL != srv.URL+"/recipes/cherry-cheese" {
		t.Fatalf("URL = %q", items[0].URL)
	}
}

func TestForDrinkTruncatesDescription(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("toasted sourdough and sharp cheddar ", 8))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":1,"title":"Long","slug":"long","description":"` + long + `","image":""}],"total":1}`))
	}))
	defer srv.Close()

	items := New(srv.URL).ForDrink(context.Background(), "cola")
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	if len([]rune(items[0].Summary)) > 121 {
		t.Fatalf("summary too long: %d runes", len([]rune(items[0].Summary)))
	}
	if !strings.HasSuffix(items[0].Summary, "…") {
		t.Fatalf("summary not truncated: %q", items[0].Summary)
	}
}

func TestForDrinkStatusFailureReturnsEmpty(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()
			if items := New(srv.URL).ForDrink(context.Background(), "cola"); items != nil {
				t.Fatalf("items = %+v, want nil", items)
			}
		})
	}
}

func TestForDrinkCachesSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"items":[{"id":1,"title":"Cheese","slug":"cheese","description":"","image":""}],"total":1}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	first := c.ForDrink(context.Background(), "cola")
	second := c.ForDrink(context.Background(), "cola")
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("items = %d, %d", len(first), len(second))
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls.Load())
	}

	// The cache hands out a copy: mutating a result must not poison later reads.
	first[0].Title = "mutated"
	if third := c.ForDrink(context.Background(), "cola"); third[0].Title != "Cheese" {
		t.Fatalf("cached item mutated: %q", third[0].Title)
	}
}

func TestForDrinkCachesFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL)
	if items := c.ForDrink(context.Background(), "cola"); items != nil {
		t.Fatalf("first call = %+v, want nil", items)
	}
	if items := c.ForDrink(context.Background(), "cola"); items != nil {
		t.Fatalf("second call = %+v, want nil", items)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1 (negative cache)", calls.Load())
	}
}

func TestForDrinkRejectsInvalidSlug(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	cases := []string{
		"",
		"Cherry-Cola",
		"-leading",
		"has space",
		"../../etc/passwd",
		strings.Repeat("a", 121),
	}
	for _, slug := range cases {
		if items := c.ForDrink(context.Background(), slug); items != nil {
			t.Errorf("slug %q returned %+v", slug, items)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls.Load())
	}
}

func TestNilClientIsSafe(t *testing.T) {
	var c *Client
	if c.Enabled() {
		t.Fatal("nil client reports enabled")
	}
	if c.Base() != "" {
		t.Fatalf("nil client base = %q", c.Base())
	}
	if items := c.ForDrink(context.Background(), "cola"); items != nil {
		t.Fatalf("nil client items = %+v", items)
	}
	if New("") != nil {
		t.Fatal("New(\"\") should be nil")
	}
}
