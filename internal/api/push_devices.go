package api

import (
	"errors"
	"net/http"

	"sodapop/internal/auth"
	"sodapop/internal/integrations"
)

const invalidPushDeviceMessage = "invalid push device"

// pushDeviceRegister handles POST /api/v1/me/push/devices.
func (s *Server) pushDeviceRegister(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if s.Integrations == nil {
		writeError(w, http.StatusServiceUnavailable, "push_unavailable", "push registration is unavailable")
		return nil
	}
	var in struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if err := s.Integrations.RegisterDevice(r.Context(), tok.User.ID, in.Token, in.Platform); err != nil {
		if errors.Is(err, integrations.ErrInvalidDeviceToken) || errors.Is(err, integrations.ErrUnsupportedPlatform) {
			writeError(w, http.StatusBadRequest, "invalid_request", invalidPushDeviceMessage)
			return nil
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// pushDeviceUnregister handles DELETE /api/v1/me/push/devices. It is scoped to
// the caller, so a token owned by another user is indistinguishable from an
// unknown token.
func (s *Server) pushDeviceUnregister(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if s.Integrations == nil {
		writeError(w, http.StatusServiceUnavailable, "push_unavailable", "push registration is unavailable")
		return nil
	}
	var in struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	deleted, err := s.Integrations.UnregisterDevice(r.Context(), tok.User.ID, in.Token)
	if err != nil {
		if errors.Is(err, integrations.ErrInvalidDeviceToken) {
			writeError(w, http.StatusBadRequest, "invalid_request", invalidPushDeviceMessage)
			return nil
		}
		return err
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "not_found", "push device not found")
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
