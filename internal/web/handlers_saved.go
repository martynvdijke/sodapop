package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"sodapop/internal/saves"
)

// saveCheckin bookmarks a visible check-in for the signed-in user. It always
// redirects back to the page the form was posted from when that page is on
// this host, so the button feels in-place and cannot become an open redirect.
func (s *Server) saveCheckin(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	id := r.PathValue("id")
	if err := s.Saves.Save(r.Context(), u.ID, id); err != nil {
		if errors.Is(err, saves.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, saves.ErrInvalid) {
			http.Error(w, "invalid checkin", http.StatusBadRequest)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, sameOriginBack(r, "/checkins/"+url.PathEscape(id)), http.StatusFound)
}

// unsaveCheckin removes a bookmark. Removing an absent save is a no-op.
func (s *Server) unsaveCheckin(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if err := s.Saves.Unsave(r.Context(), u.ID, r.PathValue("id")); err != nil {
		if errors.Is(err, saves.ErrInvalid) {
			http.Error(w, "invalid checkin", http.StatusBadRequest)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, sameOriginBack(r, "/saved"), http.StatusFound)
}

// savedPage renders the viewer's bookmarked check-ins, newest save first.
func (s *Server) savedPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	before := r.URL.Query().Get("before")

	ids, hasMore, nextBefore, err := s.Saves.ListIDs(r.Context(), u.ID, limit, before)
	if err != nil {
		if errors.Is(err, saves.ErrInvalid) {
			// A stale or tampered cursor restarts from the newest page rather
			// than erroring the whole view.
			before = ""
			ids, hasMore, nextBefore, err = s.Saves.ListIDs(r.Context(), u.ID, limit, "")
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}

	items, err := s.Social.ByIDs(r.Context(), u.ID, ids)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	csrf := ""
	if sess := SessionFromContext(r); sess != nil {
		csrf = sess.CsrfToken
	}
	for i := range items {
		items[i].Saved = true
		items[i].CSRF = csrf
	}

	s.render(w, r, http.StatusOK, "saved", map[string]any{
		"Title":      "Saved",
		"Items":      items,
		"CSRF":       csrf,
		"HasMore":    hasMore,
		"NextBefore": nextBefore,
		"Empty":      len(items) == 0,
	})
}

// sameOriginBack returns the Referer's path+query when it points at this host,
// and otherwise the supplied local fallback. It never returns an absolute URL,
// so a posted link cannot bounce a user off-site.
func sameOriginBack(r *http.Request, fallback string) string {
	ref := strings.TrimSpace(r.Header.Get("Referer"))
	if ref == "" {
		return fallback
	}
	u, err := url.Parse(ref)
	if err != nil {
		return fallback
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return fallback
	}
	if u.Host != "" && !strings.EqualFold(u.Host, r.Host) {
		return fallback
	}
	path := u.EscapedPath()
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return fallback
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return path
}
