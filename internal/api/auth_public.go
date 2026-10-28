package api

import (
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/emailverificationtoken"
	"sodapop/ent/user"
	"sodapop/internal/auth"
	"sodapop/internal/db"
)

const (
	publicAuthRateLimit  = 5
	publicAuthRateWindow = 15 * time.Minute
	publicVerifyTTL      = 24 * time.Hour
)

var publicHandleRE = regexp.MustCompile(`^[a-z0-9_]{3,24}$`)

// authRateLimited applies the same per-IP bucket used by the mobile token
// exchange. It returns false after writing the 429 response.
func (s *Server) authRateLimited(w http.ResponseWriter, r *http.Request, bucket string) bool {
	if s.Limiter == nil {
		return false
	}
	if s.Limiter.Allow(bucket+":"+s.clientIP(r), publicAuthRateLimit, publicAuthRateWindow) {
		return false
	}
	w.Header().Set("Retry-After", "900")
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later")
	return true
}

func (s *Server) authRegistrationOpen() bool {
	if s.Auth.Settings != nil {
		if snap := s.Auth.Settings.Snapshot(); snap != nil {
			return snap.RegistrationOpen()
		}
	}
	return true
}

func (s *Server) authPasswordFallback() bool {
	if s.Auth.Settings != nil {
		if snap := s.Auth.Settings.Snapshot(); snap != nil {
			return snap.AllowPasswordFallback()
		}
	}
	return s.Cfg.PasswordFallback
}

func (s *Server) authEmailAllowed(emailAddr string) bool {
	if s.Auth.Settings != nil {
		if snap := s.Auth.Settings.Snapshot(); snap != nil {
			emails, domains := snap.AllowedEmails(), snap.AllowedDomains()
			if len(emails) == 0 && len(domains) == 0 {
				return true
			}
			emailAddr = strings.ToLower(strings.TrimSpace(emailAddr))
			for _, allowed := range emails {
				if strings.EqualFold(allowed, emailAddr) {
					return true
				}
			}
			domain := ""
			if i := strings.LastIndexByte(emailAddr, '@'); i >= 0 {
				domain = emailAddr[i+1:]
			}
			for _, allowed := range domains {
				if strings.EqualFold(strings.TrimPrefix(allowed, "@"), domain) {
					return true
				}
			}
			return false
		}
	}
	return s.Cfg.EmailAllowed(emailAddr)
}

// authConfig handles GET /api/v1/auth/config.
func (s *Server) authConfig(w http.ResponseWriter, r *http.Request) {
	if s.authRateLimited(w, r, "auth-config") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"registration_open":           s.authRegistrationOpen(),
		"password_fallback":           s.authPasswordFallback(),
		"oidc_enabled":                s.Auth.OIDC != nil && s.Auth.OIDC.Enabled,
		"email_verification_required": true,
	}})
}

// authRegister handles POST /api/v1/auth/register.
func (s *Server) authRegister(w http.ResponseWriter, r *http.Request) {
	if s.authRateLimited(w, r, "auth-register") {
		return
	}
	if !s.authPasswordFallback() {
		writeError(w, http.StatusForbidden, "password_login_disabled", "password sign-in is disabled")
		return
	}
	if !s.authRegistrationOpen() {
		writeError(w, http.StatusForbidden, "registration_closed", "registration is currently closed")
		return
	}
	var in struct {
		Email       string `json:"email"`
		Handle      string `json:"handle"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.mapError(w, err)
		return
	}
	handle := strings.ToLower(strings.TrimSpace(in.Handle))
	emailAddr := strings.ToLower(strings.TrimSpace(in.Email))
	displayName := strings.TrimSpace(in.DisplayName)
	if !publicHandleRE.MatchString(handle) {
		writeError(w, http.StatusBadRequest, "invalid_request", "handle must be 3-24 characters: lowercase letters, numbers or underscores")
		return
	}
	parsed, err := mail.ParseAddress(emailAddr)
	if err != nil || !strings.EqualFold(parsed.Address, emailAddr) {
		writeError(w, http.StatusBadRequest, "invalid_request", "enter a valid email address")
		return
	}
	if len([]rune(in.Password)) < 10 {
		writeError(w, http.StatusBadRequest, "invalid_request", "use at least 10 characters for your password")
		return
	}
	if !s.authEmailAllowed(emailAddr) {
		writeError(w, http.StatusForbidden, "email_not_allowed", "registration is not available for this email address")
		return
	}

	hash, err := s.Auth.HashPassword(in.Password)
	if err != nil {
		s.mapError(w, err)
		return
	}
	reg, err := s.Auth.RegisterLocal(r.Context(), s.Auth.Client, auth.RegisterInput{
		Handle:       handle,
		Email:        emailAddr,
		DisplayName:  displayName,
		PasswordHash: hash,
	})
	if err != nil {
		if errors.Is(err, auth.ErrRegistrationConflict) {
			writeError(w, http.StatusConflict, "registration_conflict", "that handle or email is already in use")
			return
		}
		s.mapError(w, err)
		return
	}

	status := "created"
	if reg.User.EmailVerifiedAt == nil {
		status = "verification_sent"
		if s.Mailer != nil {
			if err := s.Mailer.Verification(r.Context(), emailAddr, handle, reg.VerificationToken); err != nil && s.Log != nil {
				s.Log.Warn("queue verification email failed", "err", err)
			}
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"status": status}})
}

// authVerify handles POST /api/v1/auth/verify.
func (s *Server) authVerify(w http.ResponseWriter, r *http.Request) {
	if s.authRateLimited(w, r, "auth-verify") {
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.mapError(w, err)
		return
	}
	raw := strings.TrimSpace(in.Token)
	if raw == "" {
		writeError(w, http.StatusBadRequest, "invalid_token", "this verification link is invalid or has expired")
		return
	}
	ctx := r.Context()
	row, err := s.Auth.Client.EmailVerificationToken.Query().
		Where(
			emailverificationtoken.TokenHash(auth.HashToken(raw)),
			emailverificationtoken.UsedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			writeError(w, http.StatusBadRequest, "invalid_token", "this verification link is invalid or has expired")
			return
		}
		s.mapError(w, err)
		return
	}
	expires, err := time.Parse(time.RFC3339, row.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		writeError(w, http.StatusBadRequest, "invalid_token", "this verification link is invalid or has expired")
		return
	}
	if _, err := s.Auth.Client.User.Get(ctx, row.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_token", "this verification link is invalid or has expired")
		return
	}
	if err := s.Auth.Client.User.UpdateOneID(row.UserID).
		SetEmailVerifiedAt(db.Now()).
		SetUpdatedAt(db.Now()).
		Exec(ctx); err != nil {
		s.mapError(w, err)
		return
	}
	if err := s.Auth.Client.EmailVerificationToken.UpdateOneID(row.ID).
		SetUsedAt(db.Now()).
		Exec(ctx); err != nil {
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"verified": true}})
}

// authVerifyResend handles POST /api/v1/auth/verify/resend. It always returns
// 202 so callers cannot probe for accounts.
func (s *Server) authVerifyResend(w http.ResponseWriter, r *http.Request) {
	if s.authRateLimited(w, r, "auth-verify-resend") {
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.mapError(w, err)
		return
	}
	emailAddr := strings.ToLower(strings.TrimSpace(in.Email))
	if emailAddr == "" {
		writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"status": "sent"}})
		return
	}
	ctx := r.Context()
	u, err := s.Auth.Client.User.Query().
		Where(
			user.Email(emailAddr),
			user.DeletedAtIsNil(),
			user.StatusEQ(user.StatusActive),
			user.EmailVerifiedAtIsNil(),
			user.PasswordHashNotNil(),
		).
		Only(ctx)
	if err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"status": "sent"}})
		return
	}
	now := time.Now().UTC()
	_, _ = s.Auth.Client.EmailVerificationToken.Update().
		Where(
			emailverificationtoken.UserID(u.ID),
			emailverificationtoken.UsedAtIsNil(),
		).
		SetUsedAt(now.Format(time.RFC3339)).
		Save(ctx)
	raw := auth.RandomToken(32)
	if _, err := s.Auth.Client.EmailVerificationToken.Create().
		SetTokenHash(auth.HashToken(raw)).
		SetUserID(u.ID).
		SetEmail(u.Email).
		SetExpiresAt(now.Add(publicVerifyTTL).Format(time.RFC3339)).
		Save(ctx); err != nil {
		s.mapError(w, err)
		return
	}
	if s.Mailer != nil {
		if err := s.Mailer.Verification(ctx, u.Email, u.Handle, raw); err != nil && s.Log != nil {
			s.Log.Warn("queue verification email failed", "err", err)
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"status": "sent"}})
}

// authPasswordResetRequest handles POST /api/v1/auth/password/reset.
func (s *Server) authPasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	if s.authRateLimited(w, r, "auth-password-reset") {
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.mapError(w, err)
		return
	}
	if err := s.Auth.IssuePasswordReset(r.Context(), in.Email); err != nil {
		s.mapError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"status": "sent"}})
}

// authPasswordResetConfirm handles POST /api/v1/auth/password/reset/confirm.
func (s *Server) authPasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	if s.authRateLimited(w, r, "auth-password-reset-confirm") {
		return
	}
	var in struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		s.mapError(w, err)
		return
	}
	err := s.Auth.ConsumePasswordReset(r.Context(), in.Token, in.NewPassword)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, auth.ErrInvalidResetToken):
		writeError(w, http.StatusBadRequest, "invalid_token", "this reset link is invalid or has expired")
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, "weak_password", "new password must be at least 10 characters")
	default:
		s.mapError(w, err)
	}
}
