package web

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sodapop/internal/auth"
	"sodapop/internal/clientip"
	"sodapop/internal/setup"
)

const (
	setupNonceCookie = "sd_setup_nonce"
	setupNonceTTL    = 15 * time.Minute
)

// setupPage renders the one-time bootstrap form. It returns 404 as soon as an
// admin exists so the route cannot be probed afterwards.
func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	if s.Setup == nil {
		http.NotFound(w, r)
		return
	}
	required, err := s.Setup.Required(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !required {
		http.NotFound(w, r)
		return
	}
	s.renderSetup(w, r, http.StatusOK, "", "", "", "")
}

// setupSubmit validates the same-origin request, setup nonce and bootstrap
// token, then creates the first admin and signs them in.
func (s *Server) setupSubmit(w http.ResponseWriter, r *http.Request) {
	if s.Setup == nil {
		http.NotFound(w, r)
		return
	}
	required, err := s.Setup.Required(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !required {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderSetup(w, r, http.StatusBadRequest, "The form could not be read. Try again.", "", "", "")
		return
	}
	if !setupSameOrigin(r) {
		s.renderSetup(w, r, http.StatusForbidden, "The request could not be verified. Reload the page and try again.", "", "", "")
		return
	}
	if !s.setupNonceOK(r) {
		s.renderSetup(w, r, http.StatusForbidden, "The request could not be verified. Reload the page and try again.", "", "", "")
		return
	}

	token := strings.TrimSpace(r.FormValue("token"))
	handle := strings.TrimSpace(r.FormValue("handle"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	display := strings.TrimSpace(r.FormValue("display_name"))

	_, session, err := s.Setup.ClaimSession(r.Context(), token, handle, email, password, display, s.setupSourceIP(r), r.UserAgent())
	if err != nil {
		switch {
		case errors.Is(err, setup.ErrRateLimited):
			s.renderSetup(w, r, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.", handle, email, display)
		case errors.Is(err, setup.ErrInvalidToken):
			s.renderSetup(w, r, http.StatusForbidden, "That setup token is not valid.", handle, email, display)
		case errors.Is(err, setup.ErrInvalidInput):
			s.renderSetup(w, r, http.StatusBadRequest, "Check the handle, email address and password, then try again.", handle, email, display)
		case errors.Is(err, setup.ErrSetupComplete), errors.Is(err, setup.ErrConflict):
			s.renderSetup(w, r, http.StatusConflict, "Setup could not be completed. The account details may already be in use, or setup has finished.", handle, email, display)
		default:
			s.fail(w, r, err)
		}
		return
	}
	s.Auth.SetSessionCookie(w, r, session.Token, session.ExpiresAt)
	http.Redirect(w, r, "/admin/settings", http.StatusFound)
}

func (s *Server) renderSetup(w http.ResponseWriter, r *http.Request, status int, message, handle, email, displayName string) {
	nonce := s.setupNonce(w, r)
	s.render(w, r, status, "setup", map[string]any{
		"Title":         "Set up Sodapop",
		"SetupRequired": true,
		"Nonce":         nonce,
		"SetupNonce":    nonce,
		"Error":         message,
		"Notice":        "",
		"Handle":        handle,
		"Email":         email,
		"DisplayName":   displayName,
	})
}

// setupNonce reuses a real session CSRF token when present, otherwise issues a
// short-lived signed cookie. The bootstrap token remains the authorization
// factor; this only proves the form is same-origin.
func (s *Server) setupNonce(w http.ResponseWriter, r *http.Request) string {
	if sess := SessionFromContext(r); sess != nil && sess.CsrfToken != "" {
		return sess.CsrfToken
	}
	if c, err := r.Cookie(setupNonceCookie); err == nil && c.Value != "" {
		if payload, err := s.Auth.VerifyValue(c.Value); err == nil {
			nonce := string(payload)
			if len(nonce) >= 16 {
				return nonce
			}
		}
	}
	nonce := auth.RandomToken(32)
	http.SetCookie(w, &http.Cookie{
		Name:     setupNonceCookie,
		Value:    s.Auth.SignValue([]byte(nonce)),
		Path:     "/setup",
		MaxAge:   int(setupNonceTTL.Seconds()),
		HttpOnly: true,
		Secure:   setupHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nonce
}

func (s *Server) setupNonceOK(r *http.Request) bool {
	got := r.FormValue("nonce")
	if got == "" {
		got = r.FormValue("setup_nonce")
	}
	if got == "" {
		got = r.FormValue("csrf_token")
	}
	if got == "" {
		got = r.Header.Get(auth.CSRFHeader)
	}
	if got == "" {
		return false
	}
	if sess := SessionFromContext(r); sess != nil {
		return subtle.ConstantTimeCompare([]byte(got), []byte(sess.CsrfToken)) == 1
	}
	c, err := r.Cookie(setupNonceCookie)
	if err != nil || c.Value == "" {
		return false
	}
	payload, err := s.Auth.VerifyValue(c.Value)
	if err != nil {
		return false
	}
	want := string(payload)
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// setupSourceIP derives the setup rate-limit key. X-Forwarded-For is honoured
// only when the direct peer is a configured trusted proxy.
func (s *Server) setupSourceIP(r *http.Request) string {
	var trusted []string
	if s != nil && s.Cfg != nil {
		trusted = s.Cfg.TrustedProxies
	}
	return clientip.FromRequest(r, trusted)
}

func setupSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		// Non-browser clients may omit both; the nonce still gates the POST.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func setupHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
