package api

import (
	"net/http"
	"strings"
	"time"

	"sodapop/ent/user"
	"sodapop/internal/auth"
)

// passwordToken is the break-glass mobile sign-in. It accepts an email address
// or handle and returns a fresh personal token. Responses are deliberately
// generic so the endpoint cannot be used to discover accounts.
func (s *Server) passwordToken(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.PasswordFallback {
		writeError(w, http.StatusForbidden, "password_login_disabled", "password sign-in is disabled")
		return
	}
	if !s.Limiter.Allow("mobile-login:"+clientIP(r), 5, 15*time.Minute) {
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many sign-in attempts; try again later")
		return
	}
	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}
	login := strings.TrimSpace(strings.ToLower(in.Login))
	if login == "" || in.Password == "" {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
		return
	}
	u, err := s.Auth.Client.User.Query().
		Where(user.Or(
			user.HandleEQ(login),
			user.EmailEQ(login),
		)).
		Only(r.Context())
	if err != nil || u.Status != user.StatusActive || u.PasswordHash == nil ||
		!auth.CheckPassword(*u.PasswordHash, in.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
		return
	}
	if u.EmailVerifiedAt == nil {
		writeError(w, http.StatusForbidden, "email_not_verified", "verify your email address first")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 60 {
		name = "Android app"
	}
	token, _, err := s.Auth.CreateAPIToken(r.Context(), u.ID, name, []string{"read", "write", "feed"})
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"token":  token,
		"handle": u.Handle,
	}})
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		host = host[:i]
	}
	return host
}
