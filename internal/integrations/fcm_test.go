package integrations

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"sodapop/ent/deliveryoutbox"
	"sodapop/ent/pushdevice"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func stubResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func testDeviceToken(suffix string) string {
	return strings.Repeat("token-", 8) + suffix
}

func testFCMKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func fcmAccountJSON(t *testing.T, key *rsa.PrivateKey, tokenURI string) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	payload := map[string]string{
		"client_email": "push@example.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"project_id":   "sodapop-test",
	}
	if tokenURI != "" {
		payload["token_uri"] = tokenURI
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal service account: %v", err)
	}
	return string(raw)
}

func enableTestFCM(t *testing.T, svc *Service) {
	t.Helper()
	svc.Cfg.FCMServiceAccountJSON = fcmAccountJSON(t, testFCMKey(t), fcmDefaultTokenURI)
	svc.Cfg.FCMProjectID = "sodapop-test"
}

func TestFCMAssertionClaimsAndSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	account := &fcmServiceAccount{
		ClientEmail: "push@example.iam.gserviceaccount.com",
		ProjectID:   "sodapop-test",
		TokenURI:    fcmDefaultTokenURI,
		key:         key,
	}
	at := time.Unix(1_800_000_000, 0).UTC()
	assertion, err := fcmAssertion(account, at)
	if err != nil {
		t.Fatalf("assertion: %v", err)
	}
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("assertion has %d parts, want 3", len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("parse header: %v", err)
	}
	if header["alg"] != "RS256" || header["typ"] != "JWT" {
		t.Fatalf("header = %+v", header)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("parse claims: %v", err)
	}
	if claims["iss"] != account.ClientEmail || claims["aud"] != account.TokenURI || claims["scope"] != fcmScope {
		t.Fatalf("claims = %+v", claims)
	}
	iat, _ := claims["iat"].(float64)
	exp, _ := claims["exp"].(float64)
	if int64(iat) != at.Unix() || int64(exp) != at.Add(time.Hour).Unix() {
		t.Fatalf("iat/exp = %v/%v, want %d/%d", claims["iat"], claims["exp"], at.Unix(), at.Add(time.Hour).Unix())
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
}

func TestParseFCMPrivateKeyAcceptsPKCS8(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	parsed, err := parseFCMPrivateKey(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !parsed.Equal(key) {
		t.Fatal("parsed key differs from the generated key")
	}
}

func TestSendFCMUnsetIsSilentNoOp(t *testing.T) {
	svc, _, ctx := newTestService(t)
	if svc.fcmEnabled() {
		t.Fatal("fcmEnabled = true without credentials")
	}
	pruned, err := svc.SendFCM(ctx, testDeviceToken("a"), "title", "body", "/")
	if err != nil || pruned {
		t.Fatalf("SendFCM = %v, %v; want false, nil", pruned, err)
	}
}

func TestSendFCMConfiguredInvalidReturnsError(t *testing.T) {
	svc, _, ctx := newTestService(t)
	svc.Cfg.FCMServiceAccountJSON = "{not json"
	svc.Cfg.FCMProjectID = "project"
	if svc.fcmEnabled() {
		t.Fatal("fcmEnabled = true with malformed credentials")
	}
	pruned, err := svc.SendFCM(ctx, testDeviceToken("b"), "title", "body", "/")
	if err == nil {
		t.Fatal("SendFCM with malformed config returned nil, want a config error")
	}
	if pruned {
		t.Fatal("SendFCM with malformed config reported a pruned token")
	}
}

func TestRegisterDeviceUpsertsAndScopesUnregister(t *testing.T) {
	svc, database, ctx := newTestService(t)
	ada := createUser(t, database, "ada")
	bob := createUser(t, database, "bob")
	token := testDeviceToken("shared")

	if err := svc.RegisterDevice(ctx, ada.ID, token, "android"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := database.Client.PushDevice.Update().
		Where(pushdevice.TokenEQ(token)).
		SetLastSeenAt("2000-01-01T00:00:00Z").
		Save(ctx); err != nil {
		t.Fatalf("age device: %v", err)
	}
	if err := svc.RegisterDevice(ctx, bob.ID, token, "android"); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	rows, err := database.Client.PushDevice.Query().All(ctx)
	if err != nil {
		t.Fatalf("query devices: %v", err)
	}
	if len(rows) != 1 || rows[0].UserID != bob.ID {
		t.Fatalf("devices = %+v, want one row owned by bob", rows)
	}
	if rows[0].Platform != pushdevice.PlatformAndroid {
		t.Fatalf("platform = %q", rows[0].Platform)
	}
	if rows[0].LastSeenAt == "2000-01-01T00:00:00Z" {
		t.Fatal("re-register did not refresh last_seen_at")
	}

	if err := svc.RegisterDevice(ctx, ada.ID, "short", "android"); !errors.Is(err, ErrInvalidDeviceToken) {
		t.Fatalf("short token error = %v, want ErrInvalidDeviceToken", err)
	}
	if err := svc.RegisterDevice(ctx, ada.ID, testDeviceToken("ios"), "ios"); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("non-android platform error = %v, want ErrUnsupportedPlatform", err)
	}
	if _, err := svc.UnregisterDevice(ctx, ada.ID, "short"); !errors.Is(err, ErrInvalidDeviceToken) {
		t.Fatalf("unregister invalid token error = %v, want ErrInvalidDeviceToken", err)
	}

	deleted, err := svc.UnregisterDevice(ctx, ada.ID, token)
	if err != nil {
		t.Fatalf("unregister wrong owner: %v", err)
	}
	if deleted {
		t.Fatal("unregister by a non-owner deleted the device")
	}
	deleted, err = svc.UnregisterDevice(ctx, bob.ID, token)
	if err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if !deleted {
		t.Fatal("unregister by the owner did not delete the device")
	}
	if deleted, err := svc.UnregisterDevice(ctx, bob.ID, token); err != nil || deleted {
		t.Fatalf("repeat unregister = %v, %v; want false, nil", deleted, err)
	}
}

func TestDevicesForUser(t *testing.T) {
	svc, database, ctx := newTestService(t)
	ada := createUser(t, database, "ada")
	bob := createUser(t, database, "bob")
	if err := svc.RegisterDevice(ctx, ada.ID, testDeviceToken("one"), "android"); err != nil {
		t.Fatalf("register one: %v", err)
	}
	if err := svc.RegisterDevice(ctx, ada.ID, testDeviceToken("two"), "android"); err != nil {
		t.Fatalf("register two: %v", err)
	}
	if err := svc.RegisterDevice(ctx, bob.ID, testDeviceToken("other"), "android"); err != nil {
		t.Fatalf("register other: %v", err)
	}
	devices, err := svc.DevicesForUser(ctx, ada.ID)
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(devices))
	}
	for _, d := range devices {
		if d.UserID != ada.ID || d.Platform != "android" || d.Token == "" {
			t.Fatalf("unexpected device %+v", d)
		}
	}
}

func TestRegisterDeviceCapsAtFive(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	type registered struct {
		token, id, lastSeen, created string
	}
	records := make([]registered, 0, 7)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i < 7; i++ {
		token := testDeviceToken(fmt.Sprintf("cap-%d", i))
		if err := svc.RegisterDevice(ctx, u.ID, token, "android"); err != nil {
			t.Fatalf("register %d: %v", i, err)
		}
		row, err := database.Client.PushDevice.Query().
			Where(pushdevice.TokenEQ(token)).
			Only(ctx)
		if err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
		// Give each registration a distinct age so the cap ordering is
		// deterministic even when the test runs within one wall-clock second.
		stamp := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		if _, err := database.Client.PushDevice.UpdateOneID(row.ID).
			SetLastSeenAt(stamp).
			SetCreatedAt(stamp).
			Save(ctx); err != nil {
			t.Fatalf("stamp %d: %v", i, err)
		}
		records = append(records, registered{
			token: token, id: row.ID, lastSeen: stamp, created: stamp,
		})
	}

	rows, err := database.Client.PushDevice.Query().
		Where(pushdevice.UserID(u.ID)).
		All(ctx)
	if err != nil {
		t.Fatalf("query devices: %v", err)
	}
	if len(rows) != maxDevicesPerUser {
		t.Fatalf("devices = %d, want %d", len(rows), maxDevicesPerUser)
	}

	// The cap keeps the newest by last_seen_at, then created_at, then id.
	sort.Slice(records, func(i, j int) bool {
		if records[i].lastSeen != records[j].lastSeen {
			return records[i].lastSeen > records[j].lastSeen
		}
		if records[i].created != records[j].created {
			return records[i].created > records[j].created
		}
		return records[i].id > records[j].id
	})
	for _, evicted := range records[maxDevicesPerUser:] {
		n, err := database.Client.PushDevice.Query().
			Where(pushdevice.TokenEQ(evicted.token)).
			Count(ctx)
		if err != nil {
			t.Fatalf("count evicted: %v", err)
		}
		if n != 0 {
			t.Errorf("oldest device %q survived the cap", evicted.token)
		}
	}
	for _, kept := range records[:maxDevicesPerUser] {
		n, err := database.Client.PushDevice.Query().
			Where(pushdevice.TokenEQ(kept.token)).
			Count(ctx)
		if err != nil {
			t.Fatalf("count kept: %v", err)
		}
		if n != 1 {
			t.Errorf("newer device %q was evicted", kept.token)
		}
	}
}

func TestFCMTokenRejectedOnlyForUnregisteredOrMismatch(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "404 unregistered json",
			status: 404,
			body:   `{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`,
			want:   true,
		},
		{
			name:   "403 sender mismatch json",
			status: 403,
			body:   `{"error":{"status":"PERMISSION_DENIED","details":[{"errorCode":"SENDER_ID_MISMATCH"}]}}`,
			want:   true,
		},
		{
			name:   "400 unregistered is not pruned",
			status: 400,
			body:   `{"error":{"details":[{"errorCode":"UNREGISTERED"}]}}`,
			want:   false,
		},
		{
			name:   "400 invalid argument is kept",
			status: 400,
			body:   `{"error":{"status":"INVALID_ARGUMENT","details":[{"errorCode":"INVALID_ARGUMENT"}]}}`,
			want:   false,
		},
		{
			name:   "403 invalid argument is kept",
			status: 403,
			body:   `{"error":{"status":"INVALID_ARGUMENT","details":[{"errorCode":"INVALID_ARGUMENT"}]}}`,
			want:   false,
		},
		{
			name:   "non-json fallback",
			status: 404,
			body:   "UNREGISTERED",
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fcmTokenRejected(tt.status, []byte(tt.body)); got != tt.want {
				t.Fatalf("fcmTokenRejected = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFCMAuthFailureClassification(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"401", 401, `{"error":{"status":"UNAUTHENTICATED"}}`, true},
		{"403 unauthenticated", 403, `{"error":{"status":"UNAUTHENTICATED"}}`, true},
		{"403 mismatch is not auth", 403, `{"error":{"status":"PERMISSION_DENIED","details":[{"errorCode":"SENDER_ID_MISMATCH"}]}}`, false},
		{"404 is not auth", 404, `{"error":{"status":"NOT_FOUND"}}`, false},
		{"400 is not auth", 400, `{"error":{"status":"INVALID_ARGUMENT"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fcmAuthFailure(tt.status, []byte(tt.body)); got != tt.want {
				t.Fatalf("fcmAuthFailure = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSendFCMRetriesOnceWithFreshToken(t *testing.T) {
	svc, _, ctx := newTestService(t)
	enableTestFCM(t, svc)

	var tokenCalls, sendCalls int
	var authHeaders []string
	svc.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "oauth2.googleapis.com":
			tokenCalls++
			return stubResponse(http.StatusOK,
				fmt.Sprintf(`{"access_token":"token-%d","expires_in":3600}`, tokenCalls)), nil
		case "fcm.googleapis.com":
			sendCalls++
			authHeaders = append(authHeaders, req.Header.Get("Authorization"))
			if sendCalls == 1 {
				return stubResponse(http.StatusUnauthorized, `{"error":{"status":"UNAUTHENTICATED"}}`), nil
			}
			return stubResponse(http.StatusOK, `{"name":"projects/x/messages/1"}`), nil
		default:
			return nil, fmt.Errorf("unexpected host %s", req.URL.Host)
		}
	})}

	pruned, err := svc.SendFCM(ctx, testDeviceToken("retry"), "title", "body", "/")
	if err != nil || pruned {
		t.Fatalf("SendFCM = %v, %v; want false, nil", pruned, err)
	}
	if tokenCalls != 2 || sendCalls != 2 {
		t.Fatalf("token calls = %d, send calls = %d; want 2 and 2", tokenCalls, sendCalls)
	}
	if len(authHeaders) != 2 || authHeaders[0] != "Bearer token-1" || authHeaders[1] != "Bearer token-2" {
		t.Fatalf("authorization headers = %v", authHeaders)
	}
}

func TestSendFCMDoesNotRetryMoreThanOnce(t *testing.T) {
	svc, _, ctx := newTestService(t)
	enableTestFCM(t, svc)

	var tokenCalls, sendCalls int
	svc.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "oauth2.googleapis.com":
			tokenCalls++
			return stubResponse(http.StatusOK,
				fmt.Sprintf(`{"access_token":"token-%d","expires_in":3600}`, tokenCalls)), nil
		case "fcm.googleapis.com":
			sendCalls++
			return stubResponse(http.StatusUnauthorized, `{"error":{"status":"UNAUTHENTICATED"}}`), nil
		default:
			return nil, fmt.Errorf("unexpected host %s", req.URL.Host)
		}
	})}

	pruned, err := svc.SendFCM(ctx, testDeviceToken("retry-limit"), "title", "body", "/")
	if err == nil {
		t.Fatal("SendFCM returned nil error after two auth failures")
	}
	if pruned {
		t.Fatal("SendFCM reported a pruned token for an auth failure")
	}
	if tokenCalls != 2 || sendCalls != 2 {
		t.Fatalf("token calls = %d, send calls = %d; want exactly 2 and 2", tokenCalls, sendCalls)
	}
}

func TestResetFCMTokenClearsCache(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.fcmToken = "stale"
	svc.fcmTokenExp = time.Now().Add(time.Hour)
	svc.resetFCMToken()
	if svc.fcmToken != "" || !svc.fcmTokenExp.IsZero() {
		t.Fatalf("cache after reset = %q, %v", svc.fcmToken, svc.fcmTokenExp)
	}
}

func TestFCMTokenExpiryMargin(t *testing.T) {
	at := time.Unix(1_800_000_000, 0).UTC()
	tests := []struct {
		ttl  int
		want time.Duration
	}{
		{3600, 3240 * time.Second},
		{600, 540 * time.Second},
		{300, 240 * time.Second},
		{100, 3240 * time.Second},
		{0, 3240 * time.Second},
	}
	for _, tt := range tests {
		if got := fcmTokenExpiry(at, tt.ttl).Sub(at); got != tt.want {
			t.Errorf("expiry for ttl %d = %s, want %s", tt.ttl, got, tt.want)
		}
	}
}

func TestFCMTokenURIAllowlist(t *testing.T) {
	key := testFCMKey(t)
	tests := []struct {
		tokenURI string
		want     string
	}{
		{"https://oauth2.googleapis.com/token", "https://oauth2.googleapis.com/token"},
		{"https://sub.googleapis.com/token", "https://sub.googleapis.com/token"},
		{"http://oauth2.googleapis.com/token", fcmDefaultTokenURI},
		{"https://evil.example/token", fcmDefaultTokenURI},
		{"https://evil.googleapis.com.evil.com/token", fcmDefaultTokenURI},
		{"https://user:pass@oauth2.googleapis.com/token", fcmDefaultTokenURI},
		{"", fcmDefaultTokenURI},
	}
	for _, tt := range tests {
		svc, _, _ := newTestService(t)
		svc.Cfg.FCMServiceAccountJSON = fcmAccountJSON(t, key, tt.tokenURI)
		svc.Cfg.FCMProjectID = "sodapop-test"
		account, err := svc.fcmAccount()
		if err != nil {
			t.Fatalf("account for %q: %v", tt.tokenURI, err)
		}
		if account.TokenURI != tt.want {
			t.Errorf("token_uri %q = %q, want %q", tt.tokenURI, account.TokenURI, tt.want)
		}
	}
}

func TestDispatchEnqueuesFCMRowWhenDeviceExists(t *testing.T) {
	svc, database, ctx := newTestService(t)
	enableTestFCM(t, svc)
	u := createUser(t, database, "ada")
	if _, err := database.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetPushEnabled(true).
		Save(ctx); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := svc.RegisterDevice(ctx, u.ID, testDeviceToken("outbox"), "android"); err != nil {
		t.Fatalf("register device: %v", err)
	}
	device, err := database.Client.PushDevice.Query().Where(pushdevice.UserID(u.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("load device: %v", err)
	}

	if err := svc.Dispatch(ctx, Event{UserID: u.ID, Kind: EventCheckin, Text: "Drank a cola", URL: "/you"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	rows, err := database.Client.DeliveryOutbox.Query().
		Where(deliveryoutbox.KindEQ(deliveryoutbox.KindFcm)).
		All(ctx)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("fcm rows = %d, want 1", len(rows))
	}
	if rows[0].Target != device.ID || rows[0].UserID != u.ID {
		t.Fatalf("fcm row = %+v, want target %s", rows[0], device.ID)
	}
}

func TestDispatchSkipsFCMWithoutConfig(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")
	if _, err := database.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetPushEnabled(true).
		Save(ctx); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := svc.RegisterDevice(ctx, u.ID, testDeviceToken("no-config"), "android"); err != nil {
		t.Fatalf("register device: %v", err)
	}
	if err := svc.Dispatch(ctx, Event{UserID: u.ID, Kind: EventCheckin, Text: "quiet"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	n, err := database.Client.DeliveryOutbox.Query().
		Where(deliveryoutbox.KindEQ(deliveryoutbox.KindFcm)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count fcm rows: %v", err)
	}
	if n != 0 {
		t.Fatalf("fcm rows without config = %d, want 0", n)
	}
}

func TestOutboxPrunesUnregisteredDevice(t *testing.T) {
	svc, database, ctx := newTestService(t)
	enableTestFCM(t, svc)
	u := createUser(t, database, "ada")
	if _, err := database.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetPushEnabled(true).
		Save(ctx); err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := svc.RegisterDevice(ctx, u.ID, testDeviceToken("prune"), "android"); err != nil {
		t.Fatalf("register device: %v", err)
	}

	oldHook := fcmSendHook
	var gotToken, gotTitle, gotBody, gotURL string
	fcmSendHook = func(_ context.Context, token, title, body, url string) (bool, error) {
		gotToken, gotTitle, gotBody, gotURL = token, title, body, url
		return true, nil
	}
	t.Cleanup(func() { fcmSendHook = oldHook })

	event := Event{UserID: u.ID, Kind: EventToast, Text: "Nice toast", URL: "/checkins/1"}
	if err := svc.Dispatch(ctx, event); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	svc.processOutbox(ctx)

	if gotToken != testDeviceToken("prune") {
		t.Fatalf("hook token = %q", gotToken)
	}
	if gotTitle != eventTitle(event.Kind) || gotBody != event.Text || gotURL != event.URL {
		t.Fatalf("hook payload = %q %q %q", gotTitle, gotBody, gotURL)
	}
	if n, err := database.Client.PushDevice.Query().Count(ctx); err != nil || n != 0 {
		t.Fatalf("devices after prune = %d, err = %v; want 0", n, err)
	}
	row := onlyOutbox(t, database, ctx)
	if row.DeliveredAt == nil {
		t.Fatal("pruned fcm row was not marked delivered")
	}
	if row.Attempts != 0 {
		t.Fatalf("pruned fcm row attempts = %d, want 0", row.Attempts)
	}
}
