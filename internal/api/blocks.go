package api

import (
	"errors"
	"net/http"
	"strings"

	"sodapop/internal/auth"
	"sodapop/internal/checkins"
	"sodapop/internal/social"
)

// blockUser handles POST /api/v1/users/{handle}/block. It requires the write
// scope and a verified email (enforced by protect). Unknown handles and
// self-blocks return 404, matching the task contract for this write endpoint.
func (s *Server) blockUser(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	handle := strings.ToLower(strings.TrimSpace(r.PathValue("handle")))
	if _, ok := s.blockTarget(w, r, tok, handle); !ok {
		return nil
	}
	if err := s.Social.Block(r.Context(), tok.User.ID, handle); err != nil {
		if errors.Is(err, social.ErrSelfAction) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return nil
		}
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"blocked": true}})
	return nil
}

// unblockUser handles DELETE /api/v1/users/{handle}/block. It removes only the
// caller's own block; unknown handles return 404.
func (s *Server) unblockUser(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	handle := strings.ToLower(strings.TrimSpace(r.PathValue("handle")))
	if _, ok := s.blockTarget(w, r, tok, handle); !ok {
		return nil
	}
	if err := s.Social.Unblock(r.Context(), tok.User.ID, handle); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"blocked": false}})
	return nil
}

// blockTarget resolves a handle for a block mutation and writes the 404 used
// for unknown and self targets. It returns ok=false when a response was sent.
func (s *Server) blockTarget(w http.ResponseWriter, r *http.Request, tok *auth.APIToken, handle string) (string, bool) {
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
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return "", false
	}
	return p.ID, true
}
