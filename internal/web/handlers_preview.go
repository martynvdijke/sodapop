package web

import (
	"net/http"

	"sodapop/internal/checkins"
)

// peoplePage renders the logged-out directory of opted-in public profiles.
func (s *Server) peoplePage(w http.ResponseWriter, r *http.Request) {
	if UserFromContext(r) != nil {
		http.Redirect(w, r, "/friends", http.StatusFound)
		return
	}
	people, err := s.Checkins.PublicPeople(r.Context(), 60)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "people", map[string]any{
		"Title":  "Public people",
		"People": people,
	})
}

// previewFeed loads one page of public pours for the anonymous home page.
func (s *Server) previewFeed(r *http.Request) ([]checkins.ProfileItem, string) {
	before := r.URL.Query().Get("before")
	items, err := s.Checkins.PublicFeed(r.Context(), checkins.PublicFeedFilter{
		Before: before,
		Limit:  20,
	})
	if err != nil {
		return nil, ""
	}
	next := ""
	if len(items) == 20 {
		last := items[len(items)-1]
		next = checkins.Cursor(last.CreatedAt, last.ID)
	}
	return items, next
}
