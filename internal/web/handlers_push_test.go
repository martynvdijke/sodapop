package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/pushsubscription"
	"sodapop/ent/usersetting"
	"sodapop/internal/integrations"
)

func newPushHarness(t *testing.T) *v12Harness {
	t.Helper()
	h := newV12Harness(t)
	h.srv.Integrations = integrations.New(h.db, h.srv.Cfg, h.srv.Log)
	return h
}

func pushRequest(t *testing.T, u *ent.User, target string, body any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "test-csrf")
	ctx := context.WithValue(req.Context(), userKey{}, u)
	ctx = context.WithValue(ctx, sessionKey{}, &ent.Session{CsrfToken: "test-csrf"})
	return req.WithContext(ctx)
}

func subscribeBody(endpoint string) map[string]string {
	return map[string]string{"endpoint": endpoint, "p256dh": "p256dh", "auth": "auth"}
}

func pushPreference(t *testing.T, h *v12Harness, userID string) (*ent.UserSetting, error) {
	t.Helper()
	return h.db.Client.UserSetting.Query().Where(usersetting.UserID(userID)).Only(context.Background())
}

func TestPushSubscribeEnablesPreferenceIdempotently(t *testing.T) {
	h := newPushHarness(t)
	u := h.user(t, "push-fan")

	body := subscribeBody("https://fcm.googleapis.com/test/1")
	rec := httptest.NewRecorder()
	h.srv.pushSubscribe(rec, pushRequest(t, u, "/push/subscribe", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe = %d (%s)", rec.Code, rec.Body.String())
	}

	prefs, err := pushPreference(t, h, u.ID)
	if err != nil {
		t.Fatalf("load preferences: %v", err)
	}
	if !prefs.PushEnabled {
		t.Fatal("successful subscribe left push_enabled false")
	}

	// A repeat subscribe must succeed without duplicating the subscription or
	// the preference row.
	rec = httptest.NewRecorder()
	h.srv.pushSubscribe(rec, pushRequest(t, u, "/push/subscribe", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat subscribe = %d (%s)", rec.Code, rec.Body.String())
	}
	subs, err := h.db.Client.PushSubscription.Query().
		Where(pushsubscription.UserID(u.ID)).
		Count(context.Background())
	if err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if subs != 1 {
		t.Fatalf("subscriptions = %d, want 1", subs)
	}
	prefsCount, err := h.db.Client.UserSetting.Query().
		Where(usersetting.UserID(u.ID)).
		Count(context.Background())
	if err != nil {
		t.Fatalf("count preferences: %v", err)
	}
	if prefsCount != 1 {
		t.Fatalf("preference rows = %d, want 1", prefsCount)
	}
}

func TestPushSubscribeTurnsExistingFalsePreferenceOn(t *testing.T) {
	h := newPushHarness(t)
	u := h.user(t, "push-optin")
	if _, err := h.db.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetPushEnabled(false).
		Save(context.Background()); err != nil {
		t.Fatalf("create preferences: %v", err)
	}

	rec := httptest.NewRecorder()
	h.srv.pushSubscribe(rec, pushRequest(t, u, "/push/subscribe", subscribeBody("https://fcm.googleapis.com/test/2")))
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe = %d (%s)", rec.Code, rec.Body.String())
	}

	prefs, err := pushPreference(t, h, u.ID)
	if err != nil {
		t.Fatalf("load preferences: %v", err)
	}
	if !prefs.PushEnabled {
		t.Fatal("subscribe did not turn an existing false preference on")
	}
}

func TestPushUnsubscribeLeavesPreferenceAlone(t *testing.T) {
	h := newPushHarness(t)
	u := h.user(t, "push-multi")

	rec := httptest.NewRecorder()
	h.srv.pushSubscribe(rec, pushRequest(t, u, "/push/subscribe", subscribeBody("https://fcm.googleapis.com/test/3")))
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe = %d (%s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.srv.pushUnsubscribe(rec, pushRequest(t, u, "/push/unsubscribe", map[string]string{"endpoint": "https://fcm.googleapis.com/test/3"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("unsubscribe = %d (%s)", rec.Code, rec.Body.String())
	}

	prefs, err := pushPreference(t, h, u.ID)
	if err != nil {
		t.Fatalf("load preferences: %v", err)
	}
	if !prefs.PushEnabled {
		t.Fatal("unsubscribe flipped push_enabled off")
	}
	subs, err := h.db.Client.PushSubscription.Query().
		Where(pushsubscription.UserID(u.ID)).
		Count(context.Background())
	if err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if subs != 0 {
		t.Fatalf("subscriptions after unsubscribe = %d, want 0", subs)
	}
}

func TestPushUnsubscribeDoesNotEnablePreference(t *testing.T) {
	h := newPushHarness(t)
	u := h.user(t, "push-off")
	if _, err := h.db.Client.PushSubscription.Create().
		SetUserID(u.ID).
		SetEndpoint("https://fcm.googleapis.com/test/4").
		SetP256dh("p256dh").
		SetAuth("auth").
		Save(context.Background()); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if _, err := h.db.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetPushEnabled(false).
		Save(context.Background()); err != nil {
		t.Fatalf("create preferences: %v", err)
	}

	rec := httptest.NewRecorder()
	h.srv.pushUnsubscribe(rec, pushRequest(t, u, "/push/unsubscribe", map[string]string{"endpoint": "https://fcm.googleapis.com/test/4"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("unsubscribe = %d (%s)", rec.Code, rec.Body.String())
	}

	prefs, err := pushPreference(t, h, u.ID)
	if err != nil {
		t.Fatalf("load preferences: %v", err)
	}
	if prefs.PushEnabled {
		t.Fatal("unsubscribe enabled push_enabled")
	}
}
