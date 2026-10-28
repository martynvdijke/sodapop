package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"sodapop/ent/apitoken"
	"sodapop/ent/user"
	"sodapop/ent/usersetting"
	"sodapop/internal/auth"
	"sodapop/internal/db"
)

var apiClockPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// settingsProfileDTO is the private profile block of the mobile settings screen.
type settingsProfileDTO struct {
	Handle        string `json:"handle"`
	DisplayName   string `json:"display_name"`
	Bio           string `json:"bio"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Role          string `json:"role"`
}

// settingsPrefsDTO mirrors the editable preference fields plus the user-level
// privacy fields that live on the user row rather than user_settings.
type settingsPrefsDTO struct {
	Theme             string `json:"theme"`
	Units             string `json:"units"`
	EmailWeekly       bool   `json:"email_weekly"`
	EmailFriendReq    bool   `json:"email_friend_req"`
	PushEnabled       bool   `json:"push_enabled"`
	PublicProfile     bool   `json:"public_profile"`
	AnalyticsOptOut   bool   `json:"analytics_opt_out"`
	CaffeineBedtime   string `json:"caffeine_bedtime"`
	DefaultVisibility string `json:"default_visibility"`
}

// settingsSessionDTO is one browser session. Token-authenticated API requests
// never have a session cookie, so Current is false there.
type settingsSessionDTO struct {
	ID         int    `json:"id"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	Current    bool   `json:"current"`
}

type settingsResponseDTO struct {
	Profile      settingsProfileDTO   `json:"profile"`
	Prefs        settingsPrefsDTO     `json:"prefs"`
	Sessions     []settingsSessionDTO `json:"sessions"`
	TokenCount   int                  `json:"token_count"`
	BlockedCount int                  `json:"blocked_count"`
}

// meSettingsGet handles GET /api/v1/me/settings.
func (s *Server) meSettingsGet(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	data, err := s.settingsPayload(r.Context(), r, tok.User.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
	return nil
}

// settingsPatchInput uses pointers so an absent field is distinguishable from
// an explicitly falsy value. Clients may send any subset of the fields.
type settingsPatchInput struct {
	DisplayName       *string `json:"display_name"`
	Bio               *string `json:"bio"`
	Theme             *string `json:"theme"`
	Units             *string `json:"units"`
	EmailWeekly       *bool   `json:"email_weekly"`
	EmailFriendReq    *bool   `json:"email_friend_req"`
	PushEnabled       *bool   `json:"push_enabled"`
	PublicProfile     *bool   `json:"public_profile"`
	AnalyticsOptOut   *bool   `json:"analytics_opt_out"`
	CaffeineBedtime   *string `json:"caffeine_bedtime"`
	DefaultVisibility *string `json:"default_visibility"`
}

// meSettingsPatch handles PATCH /api/v1/me/settings. It validates every supplied
// value before writing anything so a bad field cannot leave a partial update.
func (s *Server) meSettingsPatch(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in settingsPatchInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}

	invalid := func(msg string) error {
		writeError(w, http.StatusBadRequest, "invalid_request", msg)
		return nil
	}

	if in.Theme != nil {
		switch strings.TrimSpace(*in.Theme) {
		case "system", "light", "dark":
		default:
			return invalid("theme must be system, light or dark")
		}
	}
	if in.Units != nil {
		switch strings.TrimSpace(*in.Units) {
		case "metric", "imperial":
		default:
			return invalid("units must be metric or imperial")
		}
	}
	if in.CaffeineBedtime != nil {
		bedtime := strings.TrimSpace(*in.CaffeineBedtime)
		if bedtime != "" && !apiClockPattern.MatchString(bedtime) {
			return invalid("caffeine bedtime must be a time such as 21:30")
		}
	}
	if in.DefaultVisibility != nil {
		switch strings.TrimSpace(*in.DefaultVisibility) {
		case "public", "friends", "private":
		default:
			return invalid("default visibility must be public, friends or private")
		}
	}

	ctx := r.Context()

	if in.DisplayName != nil || in.Bio != nil {
		displayName, bio := tok.User.DisplayName, tok.User.Bio
		if in.DisplayName != nil {
			displayName = *in.DisplayName
		}
		if in.Bio != nil {
			bio = *in.Bio
		}
		if err := s.Auth.UpdateProfile(ctx, tok.User.ID, displayName, bio); err != nil {
			if errors.Is(err, auth.ErrInvalidDisplayName) || errors.Is(err, auth.ErrInvalidBio) {
				return invalid(err.Error())
			}
			return err
		}
	}

	prefs, err := s.Auth.GetOrCreateSettings(ctx, tok.User.ID)
	if err != nil {
		return err
	}
	prefUpdate := prefs.Update()
	prefChanged := false
	if in.Theme != nil {
		prefUpdate.SetTheme(usersetting.Theme(strings.TrimSpace(*in.Theme)))
		prefChanged = true
	}
	if in.Units != nil {
		prefUpdate.SetUnits(usersetting.Units(strings.TrimSpace(*in.Units)))
		prefChanged = true
	}
	if in.EmailWeekly != nil {
		prefUpdate.SetEmailWeekly(*in.EmailWeekly)
		prefChanged = true
	}
	if in.EmailFriendReq != nil {
		prefUpdate.SetEmailFriendReq(*in.EmailFriendReq)
		prefChanged = true
	}
	if in.PushEnabled != nil {
		prefUpdate.SetPushEnabled(*in.PushEnabled)
		prefChanged = true
	}
	if in.PublicProfile != nil {
		prefUpdate.SetPublicProfile(*in.PublicProfile)
		prefChanged = true
	}
	if in.CaffeineBedtime != nil {
		prefUpdate.SetCaffeineBedtime(strings.TrimSpace(*in.CaffeineBedtime))
		prefChanged = true
	}
	if prefChanged {
		if _, err := prefUpdate.SetUpdatedAt(db.Now()).Save(ctx); err != nil {
			return err
		}
	}

	if in.DefaultVisibility != nil || in.AnalyticsOptOut != nil {
		userUpdate := s.Auth.Client.User.UpdateOneID(tok.User.ID).SetUpdatedAt(db.Now())
		if in.DefaultVisibility != nil {
			userUpdate.SetDefaultVisibility(user.DefaultVisibility(strings.TrimSpace(*in.DefaultVisibility)))
		}
		if in.AnalyticsOptOut != nil {
			userUpdate.SetAnalyticsOptOut(*in.AnalyticsOptOut)
		}
		if err := userUpdate.Exec(ctx); err != nil {
			return err
		}
	}

	data, err := s.settingsPayload(ctx, r, tok.User.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
	return nil
}

// settingsPayload loads the full GET /me/settings shape. It re-reads the user so
// a PATCH response reflects the fields it just changed.
func (s *Server) settingsPayload(ctx context.Context, r *http.Request, userID string) (*settingsResponseDTO, error) {
	u, err := s.Auth.Client.User.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	prefs, err := s.Auth.GetOrCreateSettings(ctx, userID)
	if err != nil {
		return nil, err
	}
	sessions, err := s.settingsSessions(ctx, r, userID)
	if err != nil {
		return nil, err
	}
	tokenCount, err := s.Auth.Client.APIToken.Query().
		Where(apitoken.UserID(userID), apitoken.RevokedAtIsNil()).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	blocked, err := s.Social.BlockedUsers(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &settingsResponseDTO{
		Profile: settingsProfileDTO{
			Handle:        u.Handle,
			DisplayName:   u.DisplayName,
			Bio:           u.Bio,
			Email:         u.Email,
			EmailVerified: u.EmailVerifiedAt != nil,
			Role:          string(u.Role),
		},
		Prefs: settingsPrefsDTO{
			Theme:             string(prefs.Theme),
			Units:             string(prefs.Units),
			EmailWeekly:       prefs.EmailWeekly,
			EmailFriendReq:    prefs.EmailFriendReq,
			PushEnabled:       prefs.PushEnabled,
			PublicProfile:     prefs.PublicProfile,
			AnalyticsOptOut:   u.AnalyticsOptOut,
			CaffeineBedtime:   prefs.CaffeineBedtime,
			DefaultVisibility: string(u.DefaultVisibility),
		},
		Sessions:     sessions,
		TokenCount:   tokenCount,
		BlockedCount: len(blocked),
	}, nil
}

// settingsSessions builds the session list. currentSessionID is 0 for token
// requests without a browser session cookie.
func (s *Server) settingsSessions(ctx context.Context, r *http.Request, userID string) ([]settingsSessionDTO, error) {
	rows, err := s.Auth.ListSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	currentID := 0
	if sess, u, err := s.Auth.CurrentSession(ctx, r); err == nil && u.ID == userID {
		currentID = sess.ID
	}
	out := make([]settingsSessionDTO, 0, len(rows))
	for _, row := range rows {
		dto := settingsSessionDTO{
			ID:         row.ID,
			CreatedAt:  row.CreatedAt,
			LastSeenAt: row.LastSeenAt,
			Current:    currentID != 0 && row.ID == currentID,
		}
		if row.IP != nil {
			dto.IP = *row.IP
		}
		if row.UserAgent != nil {
			dto.UserAgent = *row.UserAgent
		}
		out = append(out, dto)
	}
	return out, nil
}

// meSessionsGet handles GET /api/v1/me/sessions.
func (s *Server) meSessionsGet(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	sessions, err := s.settingsSessions(r.Context(), r, tok.User.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": sessions})
	return nil
}

// meSessionsRevoke handles DELETE /api/v1/me/sessions/{id}.
func (s *Server) meSessionsRevoke(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Auth.RevokeSession(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return nil
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// meSessionsRevokeAll handles DELETE /api/v1/me/sessions. API tokens are not
// sessions, so they are unaffected.
func (s *Server) meSessionsRevokeAll(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Auth.RevokeAllSessions(r.Context(), tok.User.ID, ""); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// mePassword handles POST /api/v1/me/password.
func (s *Server) mePassword(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	err := s.Auth.ChangePassword(r.Context(), tok.User.ID, in.CurrentPassword, in.NewPassword, "")
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
		return nil
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusBadRequest, "invalid_password", "current password is incorrect")
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, "weak_password", "new password must be at least 10 characters")
	case errors.Is(err, auth.ErrNoPassword):
		writeError(w, http.StatusBadRequest, "no_password", "this account does not use a password")
	default:
		return err
	}
	return nil
}

// meBlocks handles GET /api/v1/me/blocks. Unblocking reuses the existing
// DELETE /api/v1/users/{handle}/block route.
func (s *Server) meBlocks(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	blocked, err := s.Social.BlockedUsers(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(blocked))
	for _, b := range blocked {
		out = append(out, map[string]any{
			"id":           b.ID,
			"handle":       b.Handle,
			"display_name": b.DisplayName,
			"blocked_at":   b.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}
