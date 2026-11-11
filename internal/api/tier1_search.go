package api

import (
	"errors"
	"net/http"

	"sodapop/ent"
	"sodapop/internal/auth"
	"sodapop/internal/savedsearch"
)

// Tier 1 saved-search surface. The service enforces the per-user cap, filter
// validation and ownership; these handlers only parse and shape the response.

// savedSearchDTO is the shared wire shape for one saved search. last_notified_at
// is null until the first alert, which mirrors how the other optional
// timestamps in the API are encoded.
func savedSearchDTO(row *ent.SavedSearch) map[string]any {
	data := map[string]any{
		"id":               row.ID,
		"name":             row.Name,
		"query":            row.Query,
		"beverage_type":    row.BeverageType,
		"style_slug":       row.StyleSlug,
		"brand_slug":       row.BrandSlug,
		"flavour_slug":     row.FlavourSlug,
		"caffeine_free":    row.CaffeineFree,
		"sugar_free":       row.SugarFree,
		"notify_drops":     row.NotifyDrops,
		"created_at":       row.CreatedAt,
		"last_notified_at": nil,
	}
	if row.LastNotifiedAt != nil && *row.LastNotifiedAt != "" {
		data["last_notified_at"] = *row.LastNotifiedAt
	}
	return data
}

// listSavedSearches returns the caller's own saved searches, newest first.
// There is no way to ask for another member's list.
func (s *Server) listSavedSearches(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	rows, err := savedsearch.New(s.DB, s.Social).List(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, savedSearchDTO(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": out,
		"meta": map[string]any{"count": len(out), "max": savedsearch.MaxPerUser},
	})
	return nil
}

// createSavedSearch stores one standing query. An omitted notify_drops defaults
// to on; the pointer in the request keeps an explicit false.
func (s *Server) createSavedSearch(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Name         string `json:"name"`
		Query        string `json:"query"`
		BeverageType string `json:"beverage_type"`
		StyleSlug    string `json:"style_slug"`
		BrandSlug    string `json:"brand_slug"`
		FlavourSlug  string `json:"flavour_slug"`
		CaffeineFree bool   `json:"caffeine_free"`
		SugarFree    bool   `json:"sugar_free"`
		NotifyDrops  *bool  `json:"notify_drops"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}

	row, err := savedsearch.New(s.DB, s.Social).Create(r.Context(), tok.User.ID, savedsearch.Input{
		Name:         in.Name,
		Query:        in.Query,
		BeverageType: in.BeverageType,
		StyleSlug:    in.StyleSlug,
		BrandSlug:    in.BrandSlug,
		FlavourSlug:  in.FlavourSlug,
		CaffeineFree: in.CaffeineFree,
		SugarFree:    in.SugarFree,
		NotifyDrops:  in.NotifyDrops,
	})
	if err != nil {
		return mapSavedSearchError(w, err)
	}
	w.Header().Set("Location", "/api/v1/me/searches/"+row.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"data": savedSearchDTO(row)})
	return nil
}

// deleteSavedSearch removes one of the caller's own searches. Another member's
// row is a 404 exactly like a missing one.
func (s *Server) deleteSavedSearch(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := savedsearch.New(s.DB, s.Social).Delete(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		return mapSavedSearchError(w, err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// mapSavedSearchError keeps the saved-search status codes local instead of
// widening the shared mapError table for one package. It writes the response and
// returns nil when it handled the error.
func mapSavedSearchError(w http.ResponseWriter, err error) error {
	switch {
	case errors.Is(err, savedsearch.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "saved search not found")
	case errors.Is(err, savedsearch.ErrLimit):
		writeError(w, http.StatusBadRequest, "limit_reached", err.Error())
	case errors.Is(err, savedsearch.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		return err
	}
	return nil
}
