package api

import (
	"errors"
	"net/http"

	"sodapop/internal/account"
	"sodapop/internal/auth"
)

// meExport handles GET /api/v1/me/export. It returns the same JSON document as
// the web export route, unwrapped, as a file download.
func (s *Server) meExport(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	raw, err := s.Account.Export(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="sodapop-export.json"`)
	// The document contains the user's whole account, so it must never land in
	// a shared or browser cache.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
	return nil
}

type deleteAccountInput struct {
	Confirm string `json:"confirm"`
}

// meDelete handles DELETE /api/v1/me. The caller must echo their exact handle
// in the confirm field before the account is closed.
func (s *Server) meDelete(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in deleteAccountInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.Confirm != tok.User.Handle {
		return bad("confirm must match your handle exactly")
	}
	if err := s.Account.Delete(r.Context(), tok.User.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// mapAccountError translates account sentinels into API responses.
func mapAccountError(err error) (int, string, string, bool) {
	switch {
	case errors.Is(err, account.ErrLastAdmin):
		return http.StatusConflict, "last_admin", "the last active admin cannot be deleted", true
	case errors.Is(err, account.ErrInvalid):
		return http.StatusBadRequest, "invalid_request", err.Error(), true
	case errors.Is(err, account.ErrNotFound):
		return http.StatusNotFound, "not_found", "not found", true
	default:
		return 0, "", "", false
	}
}
