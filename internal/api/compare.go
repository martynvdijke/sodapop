package api

import (
	"net/http"
	"strings"

	"sodapop/internal/auth"
)

// compare loads two to six drinks by slug in the requested order. It accepts
// both comma-separated and repeated `drinks` parameters and never returns a
// catalog listing.
func (s *Server) compare(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	raw := r.URL.Query()["drinks"]
	slugs := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				slugs = append(slugs, part)
			}
		}
	}
	drinks, err := s.Catalog.Compare(r.Context(), slugs)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(drinks))
	for _, d := range drinks {
		out = append(out, drinkDTO(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": out,
		"meta": map[string]any{"count": len(out)},
	})
	return nil
}
