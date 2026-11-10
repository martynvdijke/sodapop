package api

import (
	"net/http"
	"strconv"
	"strings"

	"sodapop/internal/auth"
)

// The accepted recap window mirrors checkins.recapMinYear/recapMaxYear. The
// service rejects anything outside it as a caller bug; the handler rejects it
// first so a bad path is a 400 rather than an internal error.
const (
	recapMinYear = 2000
	recapMaxYear = 2100
)

// meRecapGet returns the caller's own year-in-review aggregate. The route is
// rooted at /me, so there is no user id in the path and no way to read another
// member's recap. A year with no check-ins is still a 200 with has_data false;
// only a malformed or out-of-window year is a 400.
func (s *Server) meRecapGet(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	raw := strings.TrimSpace(r.PathValue("year"))
	year, err := strconv.Atoi(raw)
	if err != nil || year < recapMinYear || year > recapMaxYear {
		return bad("year must be an integer between 2000 and 2100")
	}

	recap, err := s.Checkins.Recap(r.Context(), tok.User.ID, year)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": recap})
	return nil
}
