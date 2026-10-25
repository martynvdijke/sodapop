package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sodapop/ent/apitoken"
	"sodapop/ent/inboundtoken"
	"sodapop/ent/session"
	"sodapop/ent/user"
	"sodapop/ent/usersetting"
	"sodapop/internal/auth"
	"sodapop/internal/db"
)

const maxAvatarBytes = 5 << 20

var clockPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// settingsSessionView is the template-friendly shape of one browser session.
type settingsSessionView struct {
	ID         int
	IP         string
	UserAgent  string
	CreatedAt  string
	LastSeenAt string
	Current    bool
}

// settingsData loads the profile, preferences, sessions and token count that
// the settings page renders.
func (s *Server) settingsData(ctx context.Context, r *http.Request) (map[string]any, error) {
	u := UserFromContext(r)
	prefs, err := s.Auth.GetOrCreateSettings(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	rows, err := s.Auth.ListSessions(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	current := SessionFromContext(r)
	sessions := make([]settingsSessionView, 0, len(rows))
	for _, row := range rows {
		v := settingsSessionView{
			ID:         row.ID,
			CreatedAt:  row.CreatedAt,
			LastSeenAt: row.LastSeenAt,
			Current:    current != nil && current.ID == row.ID,
		}
		if row.IP != nil {
			v.IP = *row.IP
		}
		if row.UserAgent != nil {
			v.UserAgent = *row.UserAgent
		}
		sessions = append(sessions, v)
	}
	tokenCount, err := s.Auth.Client.APIToken.Query().
		Where(apitoken.UserID(u.ID), apitoken.RevokedAtIsNil()).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Title": "Settings",
		"Profile": map[string]string{
			"Handle":      u.Handle,
			"DisplayName": u.DisplayName,
			"Bio":         u.Bio,
			"Email":       u.Email,
		},
		"Prefs": map[string]any{
			"Theme":           prefs.Theme,
			"Units":           prefs.Units,
			"EmailWeekly":     prefs.EmailWeekly,
			"EmailFriendReq":  prefs.EmailFriendReq,
			"PushEnabled":     prefs.PushEnabled,
			"PublicProfile":   prefs.PublicProfile,
			"AnalyticsOptOut": u.AnalyticsOptOut,
			"CaffeineBedtime": prefs.CaffeineBedtime,
		},
		"Sessions":          sessions,
		"TokenCount":        tokenCount,
		"DefaultVisibility": string(u.DefaultVisibility),
	}, nil
}

// renderSettingsError re-renders the settings page with a form error.
func (s *Server) renderSettingsError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	data, err := s.settingsData(r.Context(), r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data["Error"] = msg
	s.render(w, r, status, "settings", data)
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.settingsData(r.Context(), r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "settings", data)
}

func (s *Server) settingsProfile(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if err := s.Auth.UpdateProfile(r.Context(), u.ID,
		r.FormValue("display_name"), r.FormValue("bio")); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrInvalidDisplayName) || errors.Is(err, auth.ErrInvalidBio) {
			status = http.StatusBadRequest
		}
		if status == http.StatusBadRequest {
			s.renderSettingsError(w, r, status, err.Error())
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusFound)
}

func (s *Server) settingsAvatar(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+4096)
	if !s.csrfOK(w, r) {
		return
	}
	if err := r.ParseMultipartForm(maxAvatarBytes); err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	u := UserFromContext(r)
	file, hdr, err := r.FormFile("avatar")
	if err != nil {
		s.renderSettingsError(w, r, http.StatusBadRequest, "Choose an image to upload.")
		return
	}
	defer file.Close()
	if hdr.Size > maxAvatarBytes {
		s.renderSettingsError(w, r, http.StatusRequestEntityTooLarge, "Images must be 5 MB or smaller.")
		return
	}
	row, err := s.Media.Store(r.Context(), u.ID, "avatar",
		io.LimitReader(file, maxAvatarBytes), hdr.Filename, hdr.Header.Get("Content-Type"))
	if err != nil {
		s.renderSettingsError(w, r, http.StatusBadRequest, "That image could not be used. Try a JPEG, PNG, WebP or GIF.")
		return
	}
	if err := s.Auth.SetAvatar(r.Context(), u.ID, row.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusFound)
}

func (s *Server) settingsPreferences(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	prefs, err := s.Auth.GetOrCreateSettings(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	upd := prefs.Update()
	// The appearance form always submits `theme`; the notification form does
	// not, so use it to tell the two apart instead of clearing settings the
	// submitted form never showed.
	if r.Form.Has("theme") {
		theme := strings.TrimSpace(r.FormValue("theme"))
		switch theme {
		case "system", "light", "dark":
		default:
			s.renderSettingsError(w, r, http.StatusBadRequest, "Choose a valid theme.")
			return
		}
		units := strings.TrimSpace(r.FormValue("units"))
		switch units {
		case "metric", "imperial":
		default:
			s.renderSettingsError(w, r, http.StatusBadRequest, "Choose valid units.")
			return
		}
		bedtime := strings.TrimSpace(r.FormValue("caffeine_bedtime"))
		if bedtime != "" && !clockPattern.MatchString(bedtime) {
			s.renderSettingsError(w, r, http.StatusBadRequest, "Caffeine bedtime must be a time such as 21:30.")
			return
		}
		upd.SetTheme(usersetting.Theme(theme)).SetUnits(usersetting.Units(units)).SetCaffeineBedtime(bedtime)
	} else {
		upd.SetEmailWeekly(r.Form.Has("email_weekly")).
			SetEmailFriendReq(r.Form.Has("email_friend_req")).
			SetPushEnabled(r.Form.Has("push_enabled"))
	}
	if _, err := upd.SetUpdatedAt(db.Now()).Save(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusFound)
}

func (s *Server) settingsPrivacy(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	visibility := strings.TrimSpace(r.FormValue("default_visibility"))
	switch visibility {
	case "public", "friends", "private":
	default:
		s.renderSettingsError(w, r, http.StatusBadRequest, "Choose who can see new check-ins.")
		return
	}
	prefs, err := s.Auth.GetOrCreateSettings(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = prefs.Update().
		SetPublicProfile(r.Form.Has("public_profile")).
		SetUpdatedAt(db.Now()).
		Exec(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.Client.User.UpdateOneID(u.ID).
		SetDefaultVisibility(user.DefaultVisibility(visibility)).
		SetAnalyticsOptOut(r.Form.Has("analytics_opt_out")).
		SetUpdatedAt(db.Now()).
		Exec(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusFound)
}

func (s *Server) settingsPassword(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	current := r.FormValue("current_password")
	next := r.FormValue("new_password")
	if next != r.FormValue("confirm_password") {
		s.renderSettingsError(w, r, http.StatusBadRequest, "New passwords do not match.")
		return
	}
	sess := SessionFromContext(r)
	sessionID := ""
	if sess != nil {
		sessionID = strconv.Itoa(sess.ID)
	}
	if err := s.Auth.ChangePassword(r.Context(), u.ID, current, next, sessionID); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			s.renderSettingsError(w, r, http.StatusBadRequest, "Current password is incorrect.")
		case errors.Is(err, auth.ErrWeakPassword):
			s.renderSettingsError(w, r, http.StatusBadRequest, "New password must be at least 10 characters.")
		case errors.Is(err, auth.ErrNoPassword):
			s.renderSettingsError(w, r, http.StatusBadRequest, "This account signs in without a password.")
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/settings", http.StatusFound)
}

func (s *Server) settingsSessionRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if err := s.Auth.RevokeSession(r.Context(), u.ID, r.PathValue("id")); err != nil && !errors.Is(err, auth.ErrNotFound) {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusFound)
}

func (s *Server) settingsLogoutAll(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	keep := ""
	if sess := SessionFromContext(r); sess != nil {
		keep = strconv.Itoa(sess.ID)
	}
	if err := s.Auth.RevokeAllSessions(r.Context(), u.ID, keep); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusFound)
}

func (s *Server) settingsDelete(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if strings.TrimSpace(r.FormValue("confirm")) != u.Handle {
		s.renderSettingsError(w, r, http.StatusBadRequest, "Type your handle exactly to confirm.")
		return
	}
	if err := s.deleteAccount(r.Context(), u.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.Auth.ClearSessionCookie(w, r)
	http.Redirect(w, r, "/", http.StatusFound)
}

// deleteAccount soft-deletes a user and revokes every credential that could
// still act as them: browser sessions, API tokens, and inbound webhook tokens.
func (s *Server) deleteAccount(ctx context.Context, userID string) error {
	now := db.Now()
	tx, err := s.Auth.Client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.User.UpdateOneID(userID).
		SetStatus(user.StatusDeleted).
		SetDeletedAt(now).
		SetUpdatedAt(now).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Session.Delete().Where(session.UserID(userID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.APIToken.Update().
		Where(apitoken.UserID(userID), apitoken.RevokedAtIsNil()).
		SetRevokedAt(now).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.InboundToken.Update().
		Where(inboundtoken.UserID(userID), inboundtoken.RevokedAtIsNil()).
		SetRevokedAt(now).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Server) forgotPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "forgot", map[string]any{"Title": "Reset your password"})
}

func (s *Server) forgotSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if s.Limiter != nil && !s.Limiter.Allow("reset:"+clientIP(r), 5, time.Hour) {
		s.render(w, r, http.StatusTooManyRequests, "forgot", map[string]any{
			"Title": "Reset your password",
			"Error": "Too many requests. Try again later.",
		})
		return
	}
	// Always report the same outcome, whether or not the address exists.
	if err := s.Auth.IssuePasswordReset(r.Context(), r.FormValue("email")); err != nil {
		s.Log.Warn("password reset email failed", "err", err)
	}
	s.render(w, r, http.StatusOK, "forgot", map[string]any{
		"Title":  "Reset your password",
		"Notice": "If that address has an account, a reset link is on its way.",
	})
}

func resetTokenFrom(r *http.Request) string {
	if tok := r.PathValue("token"); tok != "" {
		return tok
	}
	return r.URL.Query().Get("token")
}

func (s *Server) resetPage(w http.ResponseWriter, r *http.Request) {
	token := resetTokenFrom(r)
	if !s.Auth.PasswordResetValid(r.Context(), token) {
		s.render(w, r, http.StatusBadRequest, "reset", map[string]any{
			"Title": "Choose a new password",
			"Error": "That reset link is no longer valid.",
		})
		return
	}
	s.render(w, r, http.StatusOK, "reset", map[string]any{
		"Title": "Choose a new password",
		"Token": token,
	})
}

func (s *Server) resetSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	// Anonymous visitors have no session to bind a CSRF token to; the reset
	// token itself is the proof. Signed-in visitors still get the CSRF check.
	if sess := SessionFromContext(r); sess != nil && !auth.ValidCSRF(r, sess) {
		http.Error(w, "CSRF check failed", http.StatusForbidden)
		return
	}
	token := resetTokenFrom(r)
	next := r.FormValue("new_password")
	if next != r.FormValue("confirm_password") {
		s.render(w, r, http.StatusBadRequest, "reset", map[string]any{
			"Title": "Choose a new password",
			"Token": token,
			"Error": "New passwords do not match.",
		})
		return
	}
	if err := s.Auth.ConsumePasswordReset(r.Context(), token, next); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidResetToken):
			s.render(w, r, http.StatusBadRequest, "reset", map[string]any{
				"Title": "Choose a new password",
				"Error": "That reset link is no longer valid.",
			})
		case errors.Is(err, auth.ErrWeakPassword):
			s.render(w, r, http.StatusBadRequest, "reset", map[string]any{
				"Title": "Choose a new password",
				"Token": token,
				"Error": "Use at least 10 characters.",
			})
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}
