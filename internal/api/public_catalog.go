package api

import (
	"errors"
	"net/http"
	"time"

	"sodapop/internal/catalog"
)

// publicDrink handles GET /api/v1/public/drinks/{slug}. It is the only
// unauthenticated catalog route and deliberately has no list counterpart.
func (s *Server) publicDrink(w http.ResponseWriter, r *http.Request) {
	if !s.Limiter.Allow("publicdrink:"+s.clientIP(r), 30, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; try again shortly")
		return
	}
	d, err := s.Catalog.PublicBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		s.internal(w, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{"data": d})
}
