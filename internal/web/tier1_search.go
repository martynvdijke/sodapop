package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"sodapop/ent"
	"sodapop/internal/savedsearch"
)

// savedSearchService builds the saved-search service on demand, the same way the
// comment service does: the handlers only need the database and the social
// service, so no extra field is threaded through the server struct.
func (s *Server) savedSearchService() *savedsearch.Service {
	return savedsearch.New(s.DB, s.Social)
}

// savedSearchForm keeps the submitted values so a validation error re-renders
// exactly what the member typed.
type savedSearchForm struct {
	Name         string
	Query        string
	BeverageType string
	StyleSlug    string
	BrandSlug    string
	FlavourSlug  string
	CaffeineFree bool
	SugarFree    bool
	NotifyDrops  bool
}

// savedSearchView pairs a stored search with the readable pills the page shows.
type savedSearchView struct {
	ID      string
	Name    string
	Filters []string
	Notify  bool
	Created string
}

// beverageTypeOption is one entry in the form's type select.
type beverageTypeOption struct {
	Value string
	Label string
}

func savedSearchBeverageTypes() []beverageTypeOption {
	return []beverageTypeOption{
		{Value: "", Label: "Any type"},
		{Value: "soda", Label: "Soda"},
		{Value: "water", Label: "Water"},
		{Value: "tea", Label: "Tea"},
		{Value: "coffee", Label: "Coffee"},
		{Value: "na_beer", Label: "NA beer"},
		{Value: "na_wine", Label: "NA wine"},
		{Value: "na_spirit", Label: "NA spirits"},
		{Value: "other", Label: "Other"},
	}
}

// filterLabel humanises a stored slug for a pill: "cold-brew" -> "Cold brew".
func filterLabel(slug string) string {
	return titleCase(strings.ReplaceAll(slug, "-", " "))
}

// savedSearchPills renders the readable filter list for one stored search. The
// order matches the form so the row reads top-to-bottom like it was filled in.
func savedSearchPills(row *ent.SavedSearch) []string {
	pills := []string{}
	if q := strings.TrimSpace(row.Query); q != "" {
		pills = append(pills, "“"+q+"”")
	}
	if row.BeverageType != "" {
		pills = append(pills, BeverageTypeLabel(row.BeverageType))
	}
	if row.StyleSlug != "" {
		pills = append(pills, "Style · "+filterLabel(row.StyleSlug))
	}
	if row.BrandSlug != "" {
		pills = append(pills, "Brand · "+filterLabel(row.BrandSlug))
	}
	if row.FlavourSlug != "" {
		pills = append(pills, "Flavour · "+filterLabel(row.FlavourSlug))
	}
	if row.CaffeineFree {
		pills = append(pills, "Caffeine-free")
	}
	if row.SugarFree {
		pills = append(pills, "Sugar-free")
	}
	return pills
}

// savedSearchesPage renders the member's standing catalog queries.
func (s *Server) savedSearchesPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login?next=/settings/searches", http.StatusFound)
		return
	}
	form := &savedSearchForm{NotifyDrops: true}
	s.renderSavedSearches(w, r, u, http.StatusOK, form, "", "")
}

// renderSavedSearches loads the list and renders the page. It backs the GET and
// both POST outcomes so the page never duplicates its own data loading.
func (s *Server) renderSavedSearches(w http.ResponseWriter, r *http.Request, u *ent.User, status int, form *savedSearchForm, errMsg, notice string) {
	svc := s.savedSearchService()
	rows, err := svc.List(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	views := make([]savedSearchView, 0, len(rows))
	for _, row := range rows {
		views = append(views, savedSearchView{
			ID:      row.ID,
			Name:    row.Name,
			Filters: savedSearchPills(row),
			Notify:  row.NotifyDrops,
			Created: row.CreatedAt,
		})
	}
	// Flavour suggestions are the one filter a member is unlikely to recall as a
	// slug, so the form offers the catalog's flavours as a datalist. A load
	// failure only costs the suggestions.
	flavours, err := svc.Flavours(r.Context())
	if err != nil {
		flavours = nil
	}
	if form == nil {
		form = &savedSearchForm{NotifyDrops: true}
	}
	s.render(w, r, status, "saved_searches", map[string]any{
		"Title":         "Saved searches",
		"Searches":      views,
		"Form":          form,
		"BeverageTypes": savedSearchBeverageTypes(),
		"Flavours":      flavours,
		"Count":         len(views),
		"Max":           savedsearch.MaxPerUser,
		"Error":         errMsg,
		"Notice":        notice,
	})
}

// savedSearchCreate validates and stores one saved search, then returns to the
// list. Validation failures re-render the form with the submitted values.
func (s *Server) savedSearchCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login?next=/settings/searches", http.StatusFound)
		return
	}

	form := &savedSearchForm{
		Name:         strings.TrimSpace(r.FormValue("name")),
		Query:        strings.TrimSpace(r.FormValue("query")),
		BeverageType: strings.TrimSpace(r.FormValue("beverage_type")),
		StyleSlug:    strings.TrimSpace(r.FormValue("style_slug")),
		BrandSlug:    strings.TrimSpace(r.FormValue("brand_slug")),
		FlavourSlug:  strings.TrimSpace(r.FormValue("flavour_slug")),
		CaffeineFree: formBool(r.FormValue("caffeine_free")),
		SugarFree:    formBool(r.FormValue("sugar_free")),
		// The checkbox is absent when cleared, so presence is the signal.
		NotifyDrops: formBool(r.FormValue("notify_drops")),
	}

	notify := form.NotifyDrops
	_, err := s.savedSearchService().Create(r.Context(), u.ID, savedsearch.Input{
		Name:         form.Name,
		Query:        form.Query,
		BeverageType: form.BeverageType,
		StyleSlug:    form.StyleSlug,
		BrandSlug:    form.BrandSlug,
		FlavourSlug:  form.FlavourSlug,
		CaffeineFree: form.CaffeineFree,
		SugarFree:    form.SugarFree,
		NotifyDrops:  &notify,
	})
	if err != nil {
		switch {
		case errors.Is(err, savedsearch.ErrLimit):
			s.renderSavedSearches(w, r, u, http.StatusBadRequest, form,
				fmt.Sprintf("You already have the maximum of %d saved searches. Delete one first.", savedsearch.MaxPerUser), "")
		case errors.Is(err, savedsearch.ErrInvalid):
			s.renderSavedSearches(w, r, u, http.StatusBadRequest, form, createSavedSearchMessage(err), "")
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/settings/searches", http.StatusSeeOther)
}

// createSavedSearchMessage turns the service's wrapped validation error into a
// short form message. The wrapper keeps the specific reason after ": ".
func createSavedSearchMessage(err error) string {
	msg := err.Error()
	if _, after, ok := strings.Cut(msg, ": "); ok && after != "" {
		return titleCase(after) + "."
	}
	return "Check the filters and try again."
}

// savedSearchDelete removes one of the member's own searches.
func (s *Server) savedSearchDelete(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login?next=/settings/searches", http.StatusFound)
		return
	}
	if err := s.savedSearchService().Delete(r.Context(), u.ID, r.PathValue("id")); err != nil {
		if errors.Is(err, savedsearch.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings/searches", http.StatusSeeOther)
}
