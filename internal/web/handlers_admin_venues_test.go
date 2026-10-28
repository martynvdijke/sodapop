package web

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/catalogrevision"
	"sodapop/internal/venues"
)

// rendererForPage builds a one-page renderer without touching the real
// templates, which the orchestrator wires separately from these handlers.
func rendererForPage(t *testing.T, page, source string) *Renderer {
	t.Helper()
	return &Renderer{pages: map[string]*template.Template{
		page: template.Must(template.New("base.html").Parse(source)),
	}}
}

func withTestUser(r *http.Request, u *ent.User) *http.Request {
	ctx := context.WithValue(r.Context(), userKey{}, u)
	return r.WithContext(ctx)
}

func bodyContainsAll(body string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(body, part) {
			return false
		}
	}
	return true
}

func TestAdminVenueMergeRoute(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	actor := h.user(t, "venue-admin")

	from, _, err := h.srv.Venues.Create(ctx, actor.ID, venues.CreateInput{Name: "Old Fizz Hall", City: "Bristol"})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	into, _, err := h.srv.Venues.Create(ctx, actor.ID, venues.CreateInput{Name: "New Fizz Hall", City: "Bristol"})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/venues/merge", h.srv.adminVenueMerge)

	form := h.form()
	form.Set("from_id", strconv.Itoa(from.ID))
	form.Set("into_id", strconv.Itoa(into.ID))
	req := h.request(http.MethodPost, "/admin/venues/merge", actor, form)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("merge status = %d, want 302 (%s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/venues?merged=1" {
		t.Fatalf("redirect = %q, want /admin/venues?merged=1", loc)
	}

	results, err := h.srv.Venues.Search(ctx, "Old Fizz", 20)
	if err != nil {
		t.Fatalf("search after merge: %v", err)
	}
	if len(results) != 1 || results[0].ID != into.ID {
		t.Fatalf("search results = %+v, want only target %d", results, into.ID)
	}
	names, err := h.srv.Venues.Names(ctx, into.ID)
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	found := false
	for _, name := range names {
		if name == "Old Fizz Hall" {
			found = true
		}
	}
	if !found {
		t.Fatalf("target names = %v, want the source name as an alias", names)
	}

	revisions, err := h.db.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityVenue)).
		All(ctx)
	if err != nil {
		t.Fatalf("revisions: %v", err)
	}
	if len(revisions) != 1 || revisions[0].Action != "merge" || revisions[0].EntityID != into.ID {
		t.Fatalf("revisions = %+v, want one merge on target %d", revisions, into.ID)
	}
}

func TestAdminVenueMergeRejectsInvalidSelection(t *testing.T) {
	h := newV12Harness(t)
	actor := h.user(t, "venue-admin")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/venues/merge", h.srv.adminVenueMerge)

	form := h.form()
	form.Set("from_id", "7")
	form.Set("into_id", "7")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodPost, "/admin/venues/merge", actor, form))

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/venues?error=invalid" {
		t.Fatalf("self merge = %d %q, want 302 error=invalid", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminVenueMergeMissingVenue(t *testing.T) {
	h := newV12Harness(t)
	actor := h.user(t, "venue-admin")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/venues/merge", h.srv.adminVenueMerge)

	form := h.form()
	form.Set("from_id", "99999")
	form.Set("into_id", "99998")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, h.request(http.MethodPost, "/admin/venues/merge", actor, form))

	if loc := rec.Header().Get("Location"); loc != "/admin/venues?error=missing" {
		t.Fatalf("missing merge redirect = %q, want error=missing", loc)
	}
}

func TestAdminVenuesPageSearchAndFrom(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	actor := h.user(t, "venue-admin")
	v, _, err := h.srv.Venues.Create(ctx, actor.ID, venues.CreateInput{Name: "Fizz Corner", City: "Bristol"})
	if err != nil {
		t.Fatalf("create venue: %v", err)
	}

	h.srv.Render = rendererForPage(t, "admin_venues",
		`{{with .From}}{{.Name}}|{{range .Names}}{{.}},{{end}}{{end}}|{{range .Results}}{{.ID}}={{.Name}};{{end}}|{{.Query}}`)
	req := httptest.NewRequest(http.MethodGet, "/admin/venues?q=Fizz&from="+strconv.Itoa(v.ID), nil)
	req = withTestUser(req, actor)
	rec := httptest.NewRecorder()
	h.srv.adminVenuesPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !bodyContainsAll(body, "Fizz Corner", "Fizz") {
		t.Fatalf("page body = %q", body)
	}
}
