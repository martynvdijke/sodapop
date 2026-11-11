package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/internal/savedsearch"
)

const savedSearchesProbe = `C={{.Count}}|MAX={{.Max}}|Q={{.Form.Query}}|T={{.Form.BeverageType}}|N={{.Form.NotifyDrops}}|E={{.Error}}{{range .Searches}}|ROW={{.ID}}/{{.Name}}{{range .Filters}}[{{.}}]{{end}}{{end}}`

func (h *v12Harness) saveSearch(t *testing.T, u *ent.User, in savedsearch.Input) *ent.SavedSearch {
	t.Helper()
	row, err := h.srv.savedSearchService().Create(context.Background(), u.ID, in)
	if err != nil {
		t.Fatalf("create saved search: %v", err)
	}
	return row
}

func TestSavedSearchesPageRenders(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "alice")
	h.saveSearch(t, u, savedsearch.Input{Name: "Cherry drops", Query: "cherry", SugarFree: true})

	rec := h.renderWith(t, "saved_searches", savedSearchesProbe)
	req := h.request(http.MethodGet, "/settings/searches", u, url.Values{})
	h.srv.savedSearchesPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"C=1", "MAX=25", "ROW=", "Cherry drops", "[“cherry”]", "[Sugar-free]",
		// A fresh form defaults the alert toggle on.
		"N=true",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

// The real template must parse and render with the page's data shape.
func TestSavedSearchesPageRealTemplate(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	u := h.user(t, "alice")
	h.saveSearch(t, u, savedsearch.Input{Query: "cherry"})

	rec := httptest.NewRecorder()
	req := h.request(http.MethodGet, "/settings/searches", u, url.Values{})
	h.srv.savedSearchesPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Saved searches", `action="/settings/searches"`, "“cherry”", "Save search"} {
		if !strings.Contains(body, want) {
			t.Fatalf("page missing %q", want)
		}
	}
}

func TestSavedSearchCreate(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "alice")

	t.Run("stores and redirects", func(t *testing.T) {
		form := url.Values{
			"csrf_token":    {"test-csrf"},
			"name":          {" Cold brew "},
			"query":         {" cold "},
			"beverage_type": {"coffee"},
			"style_slug":    {"Cold-Brew"},
			"notify_drops":  {"1"},
		}
		rec := h.renderWith(t, "saved_searches", savedSearchesProbe)
		req := h.request(http.MethodPost, "/settings/searches", u, form)
		h.srv.savedSearchCreate(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "/settings/searches" {
			t.Fatalf("location = %q", loc)
		}
		rows, err := h.srv.savedSearchService().List(context.Background(), u.ID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("rows = %d", len(rows))
		}
		row := rows[0]
		if row.Name != "Cold brew" || row.Query != "cold" || row.BeverageType != "coffee" {
			t.Fatalf("row = %+v", row)
		}
		if row.StyleSlug != "cold-brew" {
			t.Fatalf("style slug was not normalized: %q", row.StyleSlug)
		}
		if !row.NotifyDrops {
			t.Fatal("notify_drops checkbox was not honoured")
		}
	})

	t.Run("notify toggle off is stored", func(t *testing.T) {
		form := url.Values{
			"csrf_token": {"test-csrf"},
			"query":      {"lime"},
		}
		rec := h.renderWith(t, "saved_searches", savedSearchesProbe)
		req := h.request(http.MethodPost, "/settings/searches", u, form)
		h.srv.savedSearchCreate(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		rows, err := h.srv.savedSearchService().List(context.Background(), u.ID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, row := range rows {
			if row.Query == "lime" && row.NotifyDrops {
				t.Fatal("unchecked notify toggle stored as on")
			}
		}
	})

	t.Run("unfiltered search re-renders with the error", func(t *testing.T) {
		form := url.Values{
			"csrf_token": {"test-csrf"},
			"query":      {"   "},
			"name":       {"Empty"},
		}
		rec := h.renderWith(t, "saved_searches", savedSearchesProbe)
		req := h.request(http.MethodPost, "/settings/searches", u, form)
		h.srv.savedSearchCreate(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Add at least one filter") {
			t.Fatalf("missing validation message: %s", rec.Body.String())
		}
	})

	t.Run("bad CSRF is refused", func(t *testing.T) {
		form := url.Values{"csrf_token": {"wrong"}, "query": {"cola"}}
		rec := h.renderWith(t, "saved_searches", savedSearchesProbe)
		req := h.request(http.MethodPost, "/settings/searches", u, form)
		h.srv.savedSearchCreate(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestSavedSearchDelete(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "alice")
	other := h.user(t, "bob")
	row := h.saveSearch(t, owner, savedsearch.Input{Query: "cherry"})

	t.Run("own row is removed", func(t *testing.T) {
		form := url.Values{"csrf_token": {"test-csrf"}}
		rec := h.renderWith(t, "saved_searches", savedSearchesProbe)
		req := h.request(http.MethodPost, "/settings/searches/"+row.ID+"/delete", owner, form)
		req.SetPathValue("id", row.ID)
		h.srv.savedSearchDelete(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		rows, err := h.srv.savedSearchService().List(context.Background(), owner.ID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("rows = %d", len(rows))
		}
	})

	t.Run("another member's row is 404", func(t *testing.T) {
		foreign := h.saveSearch(t, owner, savedsearch.Input{Query: "grape"})
		form := url.Values{"csrf_token": {"test-csrf"}}
		rec := h.renderWith(t, "saved_searches", savedSearchesProbe)
		req := h.request(http.MethodPost, "/settings/searches/"+foreign.ID+"/delete", other, form)
		req.SetPathValue("id", foreign.ID)
		h.srv.savedSearchDelete(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		if n, _ := h.db.Client.SavedSearch.Query().Count(context.Background()); n != 1 {
			t.Fatalf("foreign delete changed the row count: %d", n)
		}
	})
}

func TestSavedSearchesRequireUser(t *testing.T) {
	h := newV12Harness(t)
	h.renderWith(t, "saved_searches", savedSearchesProbe)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/settings/searches", nil)
	h.srv.savedSearchesPage(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want redirect to login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Fatalf("location = %q", loc)
	}
}
