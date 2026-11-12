package integrations

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"sodapop/ent/pushdevice"
	"sodapop/internal/settings"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSendTestFCMReportsOperatorFacingErrors(t *testing.T) {
	svc, database, ctx := newTestService(t)
	ada := createUser(t, database, "fcm-test-ada")

	if err := svc.SendTestFCM(ctx, ada.ID); !errors.Is(err, ErrFCMNotConfigured) {
		t.Fatalf("unconfigured SendTestFCM = %v, want ErrFCMNotConfigured", err)
	}

	enableTestFCM(t, svc)

	if err := svc.SendTestFCM(ctx, ada.ID); !errors.Is(err, ErrNoFCMDevice) {
		t.Fatalf("no-device SendTestFCM = %v, want ErrNoFCMDevice", err)
	}

	oldHook := fcmSendHook
	t.Cleanup(func() { fcmSendHook = oldHook })

	var gotToken, gotTitle, gotURL string
	fcmSendHook = func(_ context.Context, token, title, _ string, url string) (bool, error) {
		gotToken, gotTitle, gotURL = token, title, url
		return false, nil
	}

	older := testDeviceToken("older")
	newer := testDeviceToken("newer")
	if err := svc.RegisterDevice(ctx, ada.ID, older, "android"); err != nil {
		t.Fatalf("register older: %v", err)
	}
	if err := svc.RegisterDevice(ctx, ada.ID, newer, "android"); err != nil {
		t.Fatalf("register newer: %v", err)
	}
	// Age the first device so "newest" is deterministic rather than a tie.
	if _, err := database.Client.PushDevice.Update().
		Where(pushdevice.TokenEQ(older)).
		SetLastSeenAt("2000-01-01T00:00:00Z").
		Save(ctx); err != nil {
		t.Fatalf("age device: %v", err)
	}

	if err := svc.SendTestFCM(ctx, ada.ID); err != nil {
		t.Fatalf("SendTestFCM: %v", err)
	}
	if gotToken != newer {
		t.Fatalf("test notification went to %q, want the newest device", gotToken)
	}
	if gotURL != "/notifications" || gotTitle == "" {
		t.Fatalf("test notification payload = %q %q", gotTitle, gotURL)
	}

	fcmSendHook = func(context.Context, string, string, string, string) (bool, error) {
		return true, nil
	}
	if err := svc.SendTestFCM(ctx, ada.ID); !errors.Is(err, ErrStaleFCMDevice) {
		t.Fatalf("pruned SendTestFCM = %v, want ErrStaleFCMDevice", err)
	}

	sentinel := errors.New("delivery exploded")
	fcmSendHook = func(context.Context, string, string, string, string) (bool, error) {
		return false, sentinel
	}
	if err := svc.SendTestFCM(ctx, ada.ID); !errors.Is(err, sentinel) {
		t.Fatalf("failed SendTestFCM = %v, want the delivery error", err)
	}
}

func TestFCMCredentialsPreferRuntimeSettings(t *testing.T) {
	svc, database, ctx := newTestService(t)
	enableTestFCM(t, svc)

	// Environment-only credentials resolve first so existing deployments keep
	// working without a visit to the admin panel.
	raw, _ := svc.fcmCredentials()
	if raw == "" || !svc.fcmEnabled() {
		t.Fatal("environment credentials were not resolved")
	}

	settingsSvc, err := settings.New(database.Client, svc.Cfg, "test-session-secret", discardLogger())
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	if err := settingsSvc.Load(ctx); err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	svc.Settings = settingsSvc

	if err := settingsSvc.Set(ctx, settings.KeyFCMServiceAccount, fcmAccountJSON(t, testFCMKey(t), ""), true, "admin-1"); err != nil {
		t.Fatalf("save settings service account: %v", err)
	}
	if err := settingsSvc.Set(ctx, settings.KeyFCMProjectID, "panel-project", false, "admin-1"); err != nil {
		t.Fatalf("save project override: %v", err)
	}

	account, err := svc.fcmAccount()
	if err != nil {
		t.Fatalf("fcmAccount: %v", err)
	}
	if account == nil || account.ProjectID != "panel-project" {
		t.Fatalf("runtime settings did not win: %+v", account)
	}
	if !svc.fcmEnabled() {
		t.Fatal("fcmEnabled = false with panel credentials")
	}
}

func TestFCMAccountFingerprintTracksCredentials(t *testing.T) {
	svc, _, _ := newTestService(t)
	enableTestFCM(t, svc)

	first, err := svc.fcmAccount()
	if err != nil || first == nil {
		t.Fatalf("fcmAccount: %v", err)
	}
	svc.Cfg.FCMServiceAccountJSON = fcmAccountJSON(t, testFCMKey(t), "")
	second, err := svc.fcmAccount()
	if err != nil || second == nil {
		t.Fatalf("fcmAccount after rotation: %v", err)
	}
	if first.fingerprint == "" {
		t.Fatal("fingerprint is empty")
	}
	if first.fingerprint == second.fingerprint {
		t.Fatal("fingerprint did not change when credentials rotated")
	}
}
