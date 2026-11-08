package api

import (
	"errors"
	"net/http"
	"strings"

	"sodapop/internal/auth"
	"sodapop/internal/overview"
)

// adminOverview returns one newest-first page of the admin activity feed.
// Non-admins get 404 so the surface is not discoverable.
func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !isAdmin(tok) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}

	q := r.URL.Query()
	var kinds []string
	if raw := strings.TrimSpace(q.Get("kind")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				kinds = append(kinds, part)
			}
		}
	}

	items, next, err := overview.New(s.DB.SQL).List(r.Context(), overview.Filter{
		Kinds:  kinds,
		Before: strings.TrimSpace(q.Get("before")),
		Limit:  queryInt(r, "limit", 50, 100),
	})
	if err != nil {
		if errors.Is(err, overview.ErrInvalidCursor) {
			return bad("invalid cursor")
		}
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": items,
		"meta": map[string]any{
			"count":       len(items),
			"next_before": next,
		},
	})
	return nil
}

// adminOverviewCounts returns the small activity summary for the admin page.
func (s *Server) adminOverviewCounts(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !isAdmin(tok) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	counts, err := overview.New(s.DB.SQL).Counts(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": counts})
	return nil
}
