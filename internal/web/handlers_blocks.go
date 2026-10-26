package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"sodapop/internal/social"
)

// settingsBlockedPage renders the accounts the viewer has blocked. Unblocking
// happens from this page's forms; templates are supplied by the wiring lane.
func (s *Server) settingsBlockedPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	blocked, err := s.Social.BlockedUsers(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "settings_blocked", map[string]any{
		"Title":        "Blocked accounts",
		"BlockedUsers": blocked,
		"BlockedError": "",
	})
}

// blockUser handles POST /u/{handle}/block. Unknown handles are a silent
// success and self-blocks are ignored, so the form cannot be used to probe for
// accounts. The redirect prefers a same-origin Referer, falling back to the
// blocked-accounts settings page because the target profile now 404s.
func (s *Server) blockUser(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	handle := strings.ToLower(strings.TrimSpace(r.PathValue("handle")))
	if err := s.Social.Block(r.Context(), u.ID, handle); err != nil && !errors.Is(err, social.ErrSelfAction) {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, blockRedirect(r, handle, "/settings/blocked"), http.StatusFound)
}

// unblockUser handles POST /u/{handle}/unblock. It only removes the viewer's
// own block, and an unknown handle is a silent success.
func (s *Server) unblockUser(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	handle := strings.ToLower(strings.TrimSpace(r.PathValue("handle")))
	if err := s.Social.Unblock(r.Context(), u.ID, handle); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, blockRedirect(r, handle, "/settings/blocked"), http.StatusFound)
}

// blockRedirect returns a safe same-origin Referer unless it points at the
// just-blocked profile (which now renders as 404).
func blockRedirect(r *http.Request, handle, fallback string) string {
	target := fallback
	if ref := r.Header.Get("Referer"); ref != "" {
		if parsed, err := url.Parse(ref); err == nil &&
			(parsed.Host == "" || strings.EqualFold(parsed.Host, r.Host)) &&
			strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "//") &&
			parsed.Path != "/u/"+handle {
			target = parsed.EscapedPath()
			if parsed.RawQuery != "" {
				target += "?" + parsed.RawQuery
			}
		}
	}
	return target
}
