package web

import (
	"net/http"
	"time"

	"sodapop/internal/integrations"
)

// adminDigestPage shows the weekly digest schedule, lock state and queue depth.
func (s *Server) adminDigestPage(w http.ResponseWriter, r *http.Request) {
	status, err := s.Integrations.DigestStatus(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}

	notice, errMsg := "", ""
	if r.URL.Query().Get("sent") == "1" {
		notice = "Weekly digest queued."
	}
	if r.URL.Query().Get("error") == "send" {
		errMsg = "The weekly digest could not be queued."
	}

	next := integrations.NextDigestTime(time.Now().UTC())
	locked := "Unlocked"
	if status.LockedUntil != "" {
		locked = digestTimeLabel(status.LockedUntil)
	}
	s.render(w, r, http.StatusOK, "admin_digest", map[string]any{
		"Title":       "Weekly digest",
		"LastRun":     digestTimeLabel(status.LastRunAt),
		"NextRun":     next.Format("Mon 02 Jan 2006 15:04 MST"),
		"LockedUntil": locked,
		"Recipients":  status.Recipients,
		"Pending":     status.Pending,
		"Notice":      notice,
		"Error":       errMsg,
	})
}

// digestTimeLabel renders a stored RFC 3339 timestamp for admins. Unknown or
// empty values are shown as-is rather than hidden.
func digestTimeLabel(raw string) string {
	if raw == "" {
		return "Never"
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return t.UTC().Format("Mon 02 Jan 2006 15:04 MST")
}
