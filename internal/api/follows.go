package api

import (
	"errors"
	"net/http"
	"strings"

	"sodapop/internal/auth"
	"sodapop/internal/checkins"
	"sodapop/internal/social"
)

// followUser records a one-way follow. Unknown, deleted and blocked targets all
// return the same 404 so the route cannot be used to probe for accounts. A
// self-follow is a 400, and repeating a successful follow is an idempotent 204.
func (s *Server) followUser(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	handle, ok := s.followTarget(w, r, tok)
	if !ok {
		return nil
	}
	if err := s.Social.Follow(r.Context(), tok.User.ID, handle); err != nil {
		if errors.Is(err, social.ErrSelfAction) {
			writeError(w, http.StatusBadRequest, "self_action", "you cannot follow yourself")
			return nil
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// unfollowUser removes a one-way follow with the same target rules as follow.
func (s *Server) unfollowUser(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	handle, ok := s.followTarget(w, r, tok)
	if !ok {
		return nil
	}
	if err := s.Social.Unfollow(r.Context(), tok.User.ID, handle); err != nil {
		if errors.Is(err, social.ErrSelfAction) {
			writeError(w, http.StatusBadRequest, "self_action", "you cannot unfollow yourself")
			return nil
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// followTarget resolves the handle for a follow mutation and writes 404 for
// unknown, deleted, blocked and invisible accounts. It returns ok=false after a
// response has been written.
func (s *Server) followTarget(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) (string, bool) {
	handle := strings.ToLower(strings.TrimSpace(r.PathValue("handle")))
	p, err := checkins.LookupProfile(r.Context(), s.Auth.Client, handle)
	if err != nil {
		if errors.Is(err, checkins.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return "", false
		}
		s.internal(w, err)
		return "", false
	}
	if p.ID == tok.User.ID {
		writeError(w, http.StatusBadRequest, "self_action", "you cannot follow yourself")
		return "", false
	}
	blocked, err := s.Social.AreBlocked(r.Context(), tok.User.ID, p.ID)
	if err != nil {
		s.internal(w, err)
		return "", false
	}
	if blocked {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return "", false
	}
	return handle, true
}
