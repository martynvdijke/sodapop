package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sodapop/internal/auth"
	"sodapop/internal/social"
)

// nearbyFeed returns friend-scoped check-ins near a point supplied with the
// request. The point is used only to build the query: it is never stored,
// logged or echoed back.
func (s *Server) nearbyFeed(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !s.Limiter.Allow("nearby:"+tok.User.ID, 30, time.Minute) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many nearby requests; try again shortly")
		return nil
	}

	q := r.URL.Query()
	latRaw := strings.TrimSpace(q.Get("lat"))
	lngRaw := strings.TrimSpace(q.Get("lng"))
	if latRaw == "" || lngRaw == "" {
		return bad("lat and lng are required")
	}
	lat, err := strconv.ParseFloat(latRaw, 64)
	if err != nil || math.IsNaN(lat) || math.IsInf(lat, 0) {
		return bad("lat must be a finite number")
	}
	lng, err := strconv.ParseFloat(lngRaw, 64)
	if err != nil || math.IsNaN(lng) || math.IsInf(lng, 0) {
		return bad("lng must be a finite number")
	}

	radius := 2.0
	if raw := strings.TrimSpace(q.Get("radius")); raw != "" {
		radius, err = strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(radius) || math.IsInf(radius, 0) {
			return bad("radius must be a finite number of kilometres")
		}
	}

	limit := 20
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			return bad("limit must be between 1 and 50")
		}
		limit = n
	}

	before := q.Get("before")
	items, hasMore, next, err := s.Social.Nearby(r.Context(), tok.User.ID, lat, lng, radius, limit, before)
	if err != nil {
		switch {
		case errors.Is(err, social.ErrInvalidLocation):
			return bad("lat must be -90..90 and lng must be -180..180")
		case errors.Is(err, social.ErrInvalidRadius):
			return bad("radius must be between 0.1 and 25 km")
		case errors.Is(err, social.ErrInvalidLimit):
			return bad("limit must be between 1 and 50")
		case errors.Is(err, social.ErrInvalidCursor):
			return bad("before cursor is invalid")
		default:
			return err
		}
	}

	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"data": items,
		"meta": map[string]any{
			"count":       len(items),
			"has_more":    hasMore,
			"next_before": next,
		},
	})
	return nil
}
