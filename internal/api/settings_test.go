package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"sodapop/ent/apitoken"
	"sodapop/ent/session"
	"sodapop/ent/usersetting"
	"sodapop/internal/auth"
	"sodapop/internal/db"
)

type settingsEnvelope struct {
	Data settingsResponseDTO `json:"data"`
}

func decodeSettings(t *testing.T, body []byte) settingsResponseDTO {
	t.Helper()
	var env settingsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode settings: %v (%s)", err, body)
	}
	return env.Data
}

func TestSettingsGetShape(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodGet, "/api/v1/me/settings", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	data := decodeSettings(t, rec.Body.Bytes())
	if data.Profile.Handle != "alice" || data.Profile.Email != "alice@example.com" || !data.Profile.EmailVerified {
		t.Errorf("unexpected profile: %+v", data.Profile)
	}
	if data.Prefs.Theme != "system" || data.Prefs.Units != "metric" || !data.Prefs.EmailFriendReq {
		t.Errorf("unexpected default prefs: %+v", data.Prefs)
	}
	if data.Prefs.DefaultVisibility != "public" {
		t.Errorf("default visibility = %q, want public", data.Prefs.DefaultVisibility)
	}
	if data.TokenCount != 1 {
		t.Errorf("token count = %d, want 1", data.TokenCount)
	}
	if data.BlockedCount != 0 {
		t.Errorf("blocked count = %d, want 0", data.BlockedCount)
	}
	if len(data.Sessions) != 0 {
		t.Errorf("sessions = %d, want 0", len(data.Sessions))
	}
}

func TestSettingsPatchPartialUpdate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.server.DB.Client.User.UpdateOneID(h.alice).SetBio("original bio").Exec(ctx); err != nil {
		t.Fatal(err)
	}

	// Only one profile field changes; bio must survive untouched.
	rec := h.do(t, http.MethodPatch, "/api/v1/me/settings", `{"display_name":"Alice Two"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile patch = %d: %s", rec.Code, rec.Body.String())
	}
	data := decodeSettings(t, rec.Body.Bytes())
	if data.Profile.DisplayName != "Alice Two" {
		t.Errorf("display name = %q", data.Profile.DisplayName)
	}
	if data.Profile.Bio != "original bio" {
		t.Errorf("bio = %q, want original bio", data.Profile.Bio)
	}

	rec = h.do(t, http.MethodPatch, "/api/v1/me/settings",
		`{"bio":"new bio","theme":"dark","units":"imperial","email_weekly":true,`+
			`"email_friend_req":false,"push_enabled":true,"public_profile":true,`+
			`"analytics_opt_out":true,"caffeine_bedtime":"21:30","default_visibility":"friends"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("prefs patch = %d: %s", rec.Code, rec.Body.String())
	}
	data = decodeSettings(t, rec.Body.Bytes())
	if data.Profile.Bio != "new bio" {
		t.Errorf("bio = %q, want new bio", data.Profile.Bio)
	}
	p := data.Prefs
	if p.Theme != "dark" || p.Units != "imperial" || !p.EmailWeekly || p.EmailFriendReq ||
		!p.PushEnabled || !p.PublicProfile || !p.AnalyticsOptOut || p.CaffeineBedtime != "21:30" ||
		p.DefaultVisibility != "friends" {
		t.Errorf("prefs not applied: %+v", p)
	}

	// Persisted, not just echoed.
	prefs, err := h.server.DB.Client.UserSetting.Query().Where(usersetting.UserID(h.alice)).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.Theme != usersetting.ThemeDark || prefs.Units != usersetting.UnitsImperial {
		t.Errorf("persisted prefs = %+v", prefs)
	}
	u, err := h.server.DB.Client.User.Get(ctx, h.alice)
	if err != nil {
		t.Fatal(err)
	}
	if !u.AnalyticsOptOut || u.DefaultVisibility != "friends" {
		t.Errorf("persisted user privacy = %+v", u)
	}
}

func TestSettingsPatchRejectsInvalidValues(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		body string
		code string
	}{
		{`{"theme":"neon"}`, "invalid_request"},
		{`{"units":"cups"}`, "invalid_request"},
		{`{"caffeine_bedtime":"24:00"}`, "invalid_request"},
		{`{"caffeine_bedtime":"9:5"}`, "invalid_request"},
		{`{"default_visibility":"everyone"}`, "invalid_request"},
	} {
		rec := h.do(t, http.MethodPatch, "/api/v1/me/settings", tc.body, true)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", tc.body, rec.Code, rec.Body.String())
			continue
		}
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if payload.Error.Code != tc.code {
			t.Errorf("%s: error code = %q, want %q", tc.body, payload.Error.Code, tc.code)
		}
		if payload.Error.Message == "" {
			t.Errorf("%s: empty error message", tc.body)
		}
	}
}

func TestSettingsSessionsListAndRevoke(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.server.Auth.CreateSession(ctx, h.alice, "192.0.2.1", "agent-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.server.Auth.CreateSession(ctx, h.alice, "192.0.2.2", "agent-two"); err != nil {
		t.Fatal(err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/me/sessions", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Data []settingsSessionDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 2 {
		t.Fatalf("sessions = %d, want 2", len(list.Data))
	}
	if list.Data[0].Current {
		t.Error("token request marked a session current; want false")
	}
	if list.Data[0].IP == "" || list.Data[0].UserAgent == "" {
		t.Errorf("session DTO missing ip/user agent: %+v", list.Data[0])
	}
	revokeID := list.Data[0].ID

	rec = h.do(t, http.MethodDelete, "/api/v1/me/sessions/"+strconv.Itoa(revokeID), "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if remaining, _ := h.server.DB.Client.Session.Query().Where(session.UserID(h.alice)).Count(ctx); remaining != 1 {
		t.Fatalf("sessions after revoke = %d, want 1", remaining)
	}

	// Foreign or missing IDs are 404, not 204.
	rec = h.do(t, http.MethodDelete, "/api/v1/me/sessions/999999", "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing revoke = %d, want 404", rec.Code)
	}

	rec = h.do(t, http.MethodDelete, "/api/v1/me/sessions", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke all = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if remaining, _ := h.server.DB.Client.Session.Query().Where(session.UserID(h.alice)).Count(ctx); remaining != 0 {
		t.Fatalf("sessions after revoke all = %d, want 0", remaining)
	}
	// API tokens are not sessions.
	if n, _ := h.server.DB.Client.APIToken.Query().
		Where(apitoken.UserID(h.alice), apitoken.RevokedAtIsNil()).Count(ctx); n != 1 {
		t.Fatalf("active tokens after revoke all = %d, want 1", n)
	}
}

func TestSettingsPasswordChange(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	hash, err := h.server.Auth.HashPassword("originalpass")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.server.DB.Client.User.UpdateOneID(h.alice).
		SetPasswordHash(hash).SetUpdatedAt(db.Now()).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	rec := h.do(t, http.MethodPost, "/api/v1/me/password",
		`{"current_password":"wrongpass","new_password":"a-new-password"}`, true)
	if rec.Code != http.StatusBadRequest || !containsCode(rec.Body.Bytes(), "invalid_password") {
		t.Fatalf("incorrect current = %d %s", rec.Code, rec.Body.String())
	}

	rec = h.do(t, http.MethodPost, "/api/v1/me/password",
		`{"current_password":"originalpass","new_password":"short"}`, true)
	if rec.Code != http.StatusBadRequest || !containsCode(rec.Body.Bytes(), "weak_password") {
		t.Fatalf("weak = %d %s", rec.Code, rec.Body.String())
	}

	rec = h.do(t, http.MethodPost, "/api/v1/me/password",
		`{"current_password":"originalpass","new_password":"a-new-password"}`, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	u, err := h.server.DB.Client.User.Get(ctx, h.alice)
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash == nil || !auth.CheckPassword(*u.PasswordHash, "a-new-password") {
		t.Error("new password was not stored")
	}
	if auth.CheckPassword(*u.PasswordHash, "originalpass") {
		t.Error("old password still works")
	}
}

func TestSettingsPasswordNoPassword(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodPost, "/api/v1/me/password",
		`{"current_password":"x","new_password":"a-new-password"}`, true)
	if rec.Code != http.StatusBadRequest || !containsCode(rec.Body.Bytes(), "no_password") {
		t.Fatalf("no password = %d %s", rec.Code, rec.Body.String())
	}
}

func TestSettingsBlocksList(t *testing.T) {
	h := newHarness(t)
	if err := h.server.Social.Block(context.Background(), h.alice, "bob"); err != nil {
		t.Fatal(err)
	}
	rec := h.do(t, http.MethodGet, "/api/v1/me/blocks", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("blocks = %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			Handle      string `json:"handle"`
			DisplayName string `json:"display_name"`
			BlockedAt   string `json:"blocked_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Handle != "bob" || payload.Data[0].ID != h.bob {
		t.Fatalf("unexpected blocks payload: %+v", payload.Data)
	}
	if payload.Data[0].BlockedAt == "" {
		t.Error("blocked_at is empty")
	}
}

func TestSettingsPatchRequiresVerifiedEmail(t *testing.T) {
	h := newHarness(t)
	if err := h.server.DB.Client.User.UpdateOneID(h.alice).
		ClearEmailVerifiedAt().SetUpdatedAt(db.Now()).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := h.do(t, http.MethodPatch, "/api/v1/me/settings", `{"theme":"dark"}`, true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unverified patch = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func containsCode(body []byte, want string) bool {
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload.Error.Code == want
}
