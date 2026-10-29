package integrations

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"strings"
	"testing"

	"sodapop/internal/settings"
)

func TestValidatePushEndpointAllowsKnownPushServices(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"fcm exact", "https://fcm.googleapis.com/fcm/send/abc:APA91b"},
		{"fcm subdomain", "https://push.fcm.googleapis.com/fcm/send/abc"},
		{"apple web push", "https://web.push.apple.com/w/abc"},
		{"apple base", "https://push.apple.com/w/abc"},
		{"apple subdomain", "https://push-relay.push.apple.com/w/abc"},
		{"mozilla", "https://updates.push.services.mozilla.com/wpush/v2/abc"},
		{"mozilla base", "https://mozilla.com/wpush/v2/abc"},
		{"windows", "https://wns2-par02p.notify.windows.com/w/?token=abc"},
		{"windows base", "https://notify.windows.com/w/?token=abc"},
		{"with port", "https://fcm.googleapis.com:443/fcm/send/abc"},
		{"uppercase host", "https://FCM.GoogleAPIs.com/fcm/send/abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validatePushEndpoint(tt.endpoint); err != nil {
				t.Fatalf("validatePushEndpoint(%q) = %v, want nil", tt.endpoint, err)
			}
		})
	}
}

func TestValidatePushEndpointRejectsUnsafeEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"http scheme", "http://fcm.googleapis.com/fcm/send/abc"},
		{"ftp scheme", "ftp://fcm.googleapis.com/fcm/send/abc"},
		{"missing scheme", "fcm.googleapis.com/fcm/send/abc"},
		{"cloud metadata", "https://169.254.169.254/x"},
		{"loopback ipv4", "https://127.0.0.1/x"},
		{"private ipv4", "https://192.168.1.5/x"},
		{"private ipv4 ten", "https://10.0.0.5/x"},
		{"unique local ipv6", "https://[fd00::1]/x"},
		{"link local ipv6", "https://[fe80::1]/x"},
		{"loopback ipv6", "https://[::1]/x"},
		{"localhost", "https://localhost/x"},
		{"localhost subdomain", "https://app.localhost/x"},
		{"unrelated host", "https://evil.com/x"},
		{"suffix impostor fcm", "https://fcm.googleapis.com.evil.com/x"},
		{"suffix impostor mozilla", "https://evil-mozilla.com/x"},
		{"suffix impostor apple", "https://evilpush.apple.com/x"},
		{"suffix impostor windows", "https://evil.notify.windows.com.evil.com/x"},
		{"host in query only", "https://evil.com/?next=https://fcm.googleapis.com/x"},
		{"userinfo", "https://user:pass@fcm.googleapis.com/x"},
		{"userinfo host trick", "https://fcm.googleapis.com@evil.com/x"},
		{"fragment", "https://fcm.googleapis.com/x#frag"},
		{"malformed escape", "https://fcm.googleapis.com/%zz"},
		{"malformed scheme", "://bad"},
		{"space in host", "https://fcm googleapis.com/x"},
		{"overlong", "https://fcm.googleapis.com/" + strings.Repeat("a", maxPushEndpointLen)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validatePushEndpoint(tt.endpoint); err == nil {
				t.Fatalf("validatePushEndpoint(%q) = nil, want an error", tt.endpoint)
			}
		})
	}
}

func TestSubscribePushRejectsUnsafeEndpointWithoutStoring(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	for _, endpoint := range []string{
		"http://fcm.googleapis.com/fcm/send/abc",
		"https://169.254.169.254/latest/meta-data/",
		"https://127.0.0.1/x",
		"https://localhost/x",
		"https://evil.com/x",
		"https://fcm.googleapis.com.evil.com/x",
		"https://evil-mozilla.com/x",
		"https://user:pass@fcm.googleapis.com/x",
		"https://fcm.googleapis.com/x#frag",
		"https://fcm.googleapis.com/%zz",
		"https://fcm.googleapis.com/" + strings.Repeat("a", maxPushEndpointLen),
	} {
		if err := svc.SubscribePush(ctx, u.ID, endpoint, "p256dh", "auth"); err == nil {
			t.Fatalf("SubscribePush(%q) = nil, want an error", endpoint)
		}
	}

	n, err := database.Client.PushSubscription.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if n != 0 {
		t.Fatalf("subscriptions after rejected endpoints = %d, want 0", n)
	}
}

func TestVAPIDResolutionOrderAndDisabledState(t *testing.T) {
	pubEnv := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x01}, 65))
	privEnv := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x02}, 32))
	t.Setenv("VAPID_PUBLIC_KEY", pubEnv)
	t.Setenv("VAPID_PRIVATE_KEY", privEnv)
	t.Setenv("VAPID_SUBJECT", "mailto:env@example.test")

	svc, database, ctx := newTestService(t)
	if got := svc.VAPIDPublicKey(); got != pubEnv {
		t.Fatalf("environment public key = %q", got)
	}
	if !svc.PushEnabled() {
		t.Fatal("push should be enabled with both environment keys")
	}
	pub, priv, subject, enabled, err := svc.vapidConfig()
	if err != nil || !enabled || pub != pubEnv || priv != privEnv || subject != "mailto:env@example.test" {
		t.Fatalf("vapidConfig = %q/%q/%q/%v/%v", pub, priv, subject, enabled, err)
	}

	// Runtime settings override the environment.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	settingsSvc, err := settings.New(database.Client, svc.Cfg, "0123456789abcdef0123456789abcdef", log)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	if err := settingsSvc.Load(ctx); err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	pubDB := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x03}, 65))
	privDB := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x04}, 32))
	if err := settingsSvc.Set(ctx, settings.KeyVAPIDPublicKey, pubDB, false, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := settingsSvc.Set(ctx, settings.KeyVAPIDPrivateKey, privDB, true, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := settingsSvc.Set(ctx, settings.KeyVAPIDSubject, "https://db.example.test/push", false, "admin"); err != nil {
		t.Fatal(err)
	}
	svc.Settings = settingsSvc
	if got := svc.VAPIDPublicKey(); got != pubDB {
		t.Fatalf("runtime public key = %q", got)
	}
	pub, priv, subject, enabled, err = svc.vapidConfig()
	if err != nil || !enabled || pub != pubDB || priv != privDB || subject != "https://db.example.test/push" {
		t.Fatalf("runtime vapidConfig = %q/%q/%q/%v/%v", pub, priv, subject, enabled, err)
	}

	// Deleting the runtime overrides falls back to the environment again.
	if err := settingsSvc.Delete(ctx, settings.KeyVAPIDPublicKey, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := settingsSvc.Delete(ctx, settings.KeyVAPIDPrivateKey, "admin"); err != nil {
		t.Fatal(err)
	}
	if got := svc.VAPIDPublicKey(); got != pubEnv {
		t.Fatalf("public key after delete = %q, want env", got)
	}

	// A single missing key disables push while still exposing the public key.
	if err := settingsSvc.Set(ctx, settings.KeyVAPIDPublicKey, "", false, "admin"); err != nil {
		t.Fatal(err)
	}
	if svc.PushEnabled() {
		t.Fatal("push should be disabled when the public key is empty")
	}
	if got := svc.VAPIDPublicKey(); got != "" {
		t.Fatalf("cleared public key = %q, want empty", got)
	}
}

func TestSubscribePushAcceptsAllowedEndpoint(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	if err := svc.SubscribePush(ctx, u.ID, "https://web.push.apple.com/test/1", "p256dh", "auth"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	n, err := database.Client.PushSubscription.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if n != 1 {
		t.Fatalf("subscriptions = %d, want 1", n)
	}
}
