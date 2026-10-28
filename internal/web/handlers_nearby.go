package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sodapop/internal/social"
)

const (
	nearbyDefaultRadius = 2.0
	nearbyPageSize      = 20
)

// nearbyPage renders the location picker and, after a search, the visible
// pours nearest to the submitted point. The coordinates live only in this
// request: they are never stored, logged, or echoed back into the page.
func (s *Server) nearbyPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.nearbySearch(w, r)
		return
	}
	s.renderNearby(w, r, http.StatusOK, nearbyDefaultRadius, "", "", "", nil, false)
}

func (s *Server) nearbySearch(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if s.Limiter != nil && !s.Limiter.Allow("nearby:"+u.ID, 30, time.Minute) {
		s.renderNearby(w, r, http.StatusTooManyRequests, nearbyDefaultRadius, "", "Too many nearby searches. Wait a minute and try again.", "", nil, false)
		return
	}

	radius := nearbyDefaultRadius
	if raw := strings.TrimSpace(r.PostFormValue("radius")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			radius = v
		}
	}
	if radius < 0.1 || radius > 25 {
		s.renderNearby(w, r, http.StatusOK, nearbyDefaultRadius, "", "Choose a radius between 0.1 and 25 km.", "", nil, false)
		return
	}

	latRaw := strings.TrimSpace(r.PostFormValue("lat"))
	lngRaw := strings.TrimSpace(r.PostFormValue("lng"))
	if latRaw == "" || lngRaw == "" {
		s.renderNearby(w, r, http.StatusOK, radius, "", "Set your location first, then show nearby pours.", "", nil, false)
		return
	}
	lat, err := strconv.ParseFloat(latRaw, 64)
	if err != nil {
		s.renderNearby(w, r, http.StatusOK, radius, "", "That location could not be read. Try again.", "", nil, false)
		return
	}
	lng, err := strconv.ParseFloat(lngRaw, 64)
	if err != nil {
		s.renderNearby(w, r, http.StatusOK, radius, "", "That location could not be read. Try again.", "", nil, false)
		return
	}

	cursor := strings.TrimSpace(r.PostFormValue("cursor"))
	items, hasMore, nextCursor, err := s.Social.Nearby(r.Context(), u.ID, lat, lng, radius, nearbyPageSize, cursor)
	if err != nil {
		switch {
		case errors.Is(err, social.ErrInvalidLocation):
			s.renderNearby(w, r, http.StatusOK, radius, "", "That location could not be read. Try again.", "", nil, false)
		case errors.Is(err, social.ErrInvalidRadius):
			s.renderNearby(w, r, http.StatusOK, nearbyDefaultRadius, "", "Choose a radius between 0.1 and 25 km.", "", nil, false)
		case errors.Is(err, social.ErrInvalidCursor):
			s.renderNearby(w, r, http.StatusOK, radius, "", "Those results expired. Try the search again.", "", nil, false)
		default:
			s.fail(w, r, err)
		}
		return
	}

	s.renderNearby(w, r, http.StatusOK, radius, nextCursor, "", cursor, items, hasMore)
}

// renderNearby writes the page used by both the first load and the fetch
// fragment response. cursor retains the submitted value on an error so the
// form can be retried without silently restarting at the newest page.
func (s *Server) renderNearby(w http.ResponseWriter, r *http.Request, status int, radius float64, nextCursor, errMsg, submittedCursor string, items []social.NearbyItem, hasMore bool) {
	csrf := ""
	if sess := SessionFromContext(r); sess != nil {
		csrf = sess.CsrfToken
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	cursor := nextCursor
	if errMsg != "" {
		cursor = submittedCursor
	}
	locationSet := r.Method == http.MethodPost &&
		strings.TrimSpace(r.PostFormValue("lat")) != "" &&
		strings.TrimSpace(r.PostFormValue("lng")) != ""
	s.render(w, r, status, "nearby", map[string]any{
		"Title":       "Nearby",
		"CSRF":        csrf,
		"Cursor":      cursor,
		"Error":       errMsg,
		"LocationSet": locationSet,
		"Radius":      radius,
		"Empty":       errMsg == "" && items != nil && len(items) == 0,
		"Items":       items,
		"HasMore":     hasMore,
	})
}
