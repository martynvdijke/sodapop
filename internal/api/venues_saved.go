package api

import (
	"net/http"

	"sodapop/internal/auth"
)

// savedVenues returns the venues the viewer bookmarked, newest first. It is
// personal, read-scoped and carries no total.
func (s *Server) savedVenues(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	rows, err := s.Venues.Saved(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, v := range rows {
		out = append(out, map[string]any{
			"id": v.ID, "name": v.Name, "kind": v.Kind, "city": v.City,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}
