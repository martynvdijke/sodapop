package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"sodapop/ent/user"
	"sodapop/internal/comments"
)

func (s *Server) commentService() *comments.Service {
	return comments.New(s.DB, s.Checkins)
}

// commentCreate posts a comment to a check-in the viewer can see.
func (s *Server) commentCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusFound)
		return
	}

	checkinID := r.PathValue("id")
	body := r.FormValue("body")
	_, err := s.commentService().Create(r.Context(), u.ID, checkinID, body)
	if err == nil {
		http.Redirect(w, r, "/checkins/"+checkinID+"#comments", http.StatusFound)
		return
	}
	switch {
	case errors.Is(err, comments.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, comments.ErrInvalid):
		message := "Write something before posting."
		if utf8.RuneCountInString(strings.TrimSpace(body)) > 1000 {
			message = "Comments are capped at 1,000 characters."
		}
		s.renderCheckinDetail(w, r, http.StatusBadRequest, message)
	case errors.Is(err, comments.ErrRateLimited):
		s.renderCheckinDetail(w, r, http.StatusTooManyRequests, "You're commenting too quickly. Try again later.")
	default:
		s.fail(w, r, err)
	}
}

// commentDelete soft-deletes a comment. The redirect prefers a same-origin
// Referer so inline actions keep their place, but never trusts a foreign URL.
func (s *Server) commentDelete(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	commentID := r.PathValue("id")
	checkinID := strings.TrimSpace(r.FormValue("checkin_id"))
	if checkinID == "" {
		if c, err := s.DB.Client.Comment.Get(r.Context(), commentID); err == nil {
			checkinID = c.CheckinID
		}
	}

	admin := u.Role == user.RoleAdmin || u.Role == user.RoleModerator
	if err := s.commentService().Delete(r.Context(), u.ID, admin, commentID); err != nil {
		if errors.Is(err, comments.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}

	target := "/"
	if checkinID != "" {
		target = "/checkins/" + checkinID
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		if parsed, err := url.Parse(ref); err == nil &&
			(parsed.Host == "" || strings.EqualFold(parsed.Host, r.Host)) &&
			strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "//") {
			target = parsed.EscapedPath()
			if parsed.RawQuery != "" {
				target += "?" + parsed.RawQuery
			}
		}
	}
	http.Redirect(w, r, target, http.StatusFound)
}
