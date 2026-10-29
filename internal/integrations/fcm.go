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
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"sodapop/ent"
	"sodapop/ent/pushdevice"
)

const (
	fcmScope           = "https://www.googleapis.com/auth/firebase.messaging"
	fcmDefaultTokenURI = "https://oauth2.googleapis.com/token"
	fcmSendEndpoint    = "https://fcm.googleapis.com/v1/projects/%s/messages:send"
	fcmChannelID       = "sodapop_default"
	fcmCollapseKey     = "sodapop-notification"

	minDeviceTokenLen = 32
	maxDeviceTokenLen = 4096
	maxDevicesPerUser = 5
)

var (
	// ErrInvalidDeviceToken means the supplied token is not a plausible FCM
	// registration token. It is safe to return to the caller verbatim.
	ErrInvalidDeviceToken = errors.New("invalid device token")
	// ErrUnsupportedPlatform means this server only accepts Android devices.
	ErrUnsupportedPlatform = errors.New("unsupported device platform")

	fcmTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_:.-]{32,512}$`)

	fcmConfigWarnOnce sync.Once
)

// Device is a native push registration owned by one user.
type Device struct {
	ID       string
	UserID   string
	Token    string
	Platform string
}

// fcmSendHook lets tests observe FCM delivery without credentials or network.
// It is nil in production.
var fcmSendHook func(ctx context.Context, token, title, body, url string) (pruned bool, err error)

// fcmServiceAccount is the subset of a Google service-account key we need.
type fcmServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	ProjectID   string `json:"project_id"`
	TokenURI    string `json:"token_uri"`

	key *rsa.PrivateKey
}

type fcmMessage struct {
	Message fcmMessageBody `json:"message"`
}

type fcmMessageBody struct {
	Token        string            `json:"token"`
	Notification fcmNotification   `json:"notification"`
	Data         map[string]string `json:"data"`
	Android      fcmAndroid        `json:"android"`
}

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type fcmAndroid struct {
	Priority     string           `json:"priority"`
	CollapseKey  string           `json:"collapse_key"`
	Notification fcmAndroidNotice `json:"notification"`
}

type fcmAndroidNotice struct {
	ChannelID string `json:"channel_id"`
}

// RegisterDevice stores or refreshes an Android FCM token. A token is unique,
// so registering it again moves it to the current user and updates last_seen_at.
// The write and the five-device cap share one transaction so concurrent or
// repeated registrations cannot leave a user over the limit.
func (s *Service) RegisterDevice(ctx context.Context, userID, token, platform string) error {
	token = strings.TrimSpace(token)
	platform = strings.TrimSpace(platform)
	if userID == "" {
		return fmt.Errorf("user is required")
	}
	if !fcmTokenPattern.MatchString(token) {
		return ErrInvalidDeviceToken
	}
	if platform != string(pushdevice.PlatformAndroid) {
		return ErrUnsupportedPlatform
	}

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	seen := now()
	existing, err := tx.PushDevice.Query().
		Where(pushdevice.TokenEQ(token)).
		Only(ctx)
	switch {
	case err == nil:
		_, err = existing.Update().
			SetUserID(userID).
			SetPlatform(pushdevice.PlatformAndroid).
			SetLastSeenAt(seen).
			Save(ctx)
		if err != nil {
			return err
		}
	case ent.IsNotFound(err):
		_, err = tx.PushDevice.Create().
			SetUserID(userID).
			SetToken(token).
			SetPlatform(pushdevice.PlatformAndroid).
			SetCreatedAt(seen).
			SetLastSeenAt(seen).
			Save(ctx)
		if ent.IsConstraintError(err) {
			// Another request inserted the same token between the query and
			// create; adopt it instead of failing the registration.
			_, err = tx.PushDevice.Update().
				Where(pushdevice.TokenEQ(token)).
				SetUserID(userID).
				SetPlatform(pushdevice.PlatformAndroid).
				SetLastSeenAt(seen).
				Save(ctx)
		}
		if err != nil {
			return err
		}
	default:
		return err
	}

	rows, err := tx.PushDevice.Query().
		Where(pushdevice.UserID(userID)).
		Order(
			ent.Desc(pushdevice.FieldLastSeenAt),
			ent.Desc(pushdevice.FieldCreatedAt),
			ent.Desc(pushdevice.FieldID),
		).
		All(ctx)
	if err != nil {
		return err
	}
	if len(rows) > maxDevicesPerUser {
		evicted := make([]string, 0, len(rows)-maxDevicesPerUser)
		for _, row := range rows[maxDevicesPerUser:] {
			evicted = append(evicted, row.ID)
		}
		if _, err := tx.PushDevice.Delete().
			Where(pushdevice.IDIn(evicted...)).
			Exec(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UnregisterDevice removes one token owned by the user. It reports whether a
// row was deleted so the API can answer 404 for unknown tokens.
func (s *Service) UnregisterDevice(ctx context.Context, userID, token string) (bool, error) {
	token = strings.TrimSpace(token)
	if !fcmTokenPattern.MatchString(token) {
		return false, ErrInvalidDeviceToken
	}
	if userID == "" {
		return false, nil
	}
	n, err := s.Client.PushDevice.Delete().
		Where(pushdevice.UserID(userID), pushdevice.TokenEQ(token)).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("delete push device: %w", err)
	}
	return n > 0, nil
}

// DevicesForUser lists every native push registration owned by a user.
func (s *Service) DevicesForUser(ctx context.Context, userID string) ([]Device, error) {
	rows, err := s.Client.PushDevice.Query().
		Where(pushdevice.UserID(userID)).
		Order(ent.Asc(pushdevice.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(rows))
	for _, row := range rows {
		out = append(out, Device{
			ID:       row.ID,
			UserID:   row.UserID,
			Token:    row.Token,
			Platform: string(row.Platform),
		})
	}
	return out, nil
}

// SendFCM delivers one notification token. It is a no-op when credentials are
// unset. Invalid configured credentials and delivery failures return an error
// so the outbox records them. The bool reports a token FCM has permanently
// rejected so the caller can prune the device row.
func (s *Service) SendFCM(ctx context.Context, token, title, body, link string) (bool, error) {
	if fcmSendHook != nil {
		return fcmSendHook(ctx, token, title, body, link)
	}
	account, err := s.fcmAccount()
	if err != nil {
		fcmConfigWarnOnce.Do(func() {
			s.logger().Warn("fcm configuration is invalid; delivery disabled", "err", err)
		})
		return false, err
	}
	if account == nil {
		return false, nil
	}

	payload, err := json.Marshal(fcmMessage{Message: fcmMessageBody{
		Token:        token,
		Notification: fcmNotification{Title: title, Body: body},
		Data:         map[string]string{"url": link},
		Android: fcmAndroid{
			Priority:     "high",
			CollapseKey:  fcmCollapseKey,
			Notification: fcmAndroidNotice{ChannelID: fcmChannelID},
		},
	}})
	if err != nil {
		return false, err
	}
	endpoint := fmt.Sprintf(fcmSendEndpoint, url.PathEscape(account.ProjectID))

	// A cached OAuth token can be revoked before its nominal expiry. Retry
	// exactly once with a freshly minted token on an authentication failure.
	for attempt := 0; attempt < 2; attempt++ {
		accessToken, err := s.fcmAccessToken(ctx, account)
		if err != nil {
			return false, err
		}
		status, raw, err := s.postFCMMessage(ctx, endpoint, accessToken, payload)
		if err != nil {
			return false, err
		}
		if status >= 200 && status < 300 {
			return false, nil
		}
		if fcmTokenRejected(status, raw) {
			return true, nil
		}
		if attempt == 0 && fcmAuthFailure(status, raw) {
			s.resetFCMToken()
			continue
		}
		return false, fmt.Errorf("fcm returned HTTP %d", status)
	}
	return false, fmt.Errorf("fcm returned an authentication error")
}

func (s *Service) postFCMMessage(ctx context.Context, endpoint, accessToken string, payload []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, raw, nil
}

// fcmErrorEnvelope is the subset of the FCM error response needed to tell a
// dead token apart from a payload or authentication failure.
type fcmErrorEnvelope struct {
	Error struct {
		Status  string `json:"status"`
		Details []struct {
			ErrorCode string `json:"errorCode"`
		} `json:"details"`
	} `json:"error"`
}

func parseFCMError(body []byte) (fcmErrorEnvelope, bool) {
	var envelope fcmErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fcmErrorEnvelope{}, false
	}
	return envelope, true
}

// fcmTokenRejected reports whether FCM says the device token is no longer
// usable and the stored row should be removed.
func fcmTokenRejected(status int, body []byte) bool {
	if status != http.StatusForbidden && status != http.StatusNotFound {
		return false
	}
	if envelope, ok := parseFCMError(body); ok {
		for _, detail := range envelope.Error.Details {
			if detail.ErrorCode == "UNREGISTERED" || detail.ErrorCode == "SENDER_ID_MISMATCH" {
				return true
			}
		}
		return false
	}
	// Fall back to the documented error string only when the body is not JSON.
	text := string(body)
	return strings.Contains(text, "UNREGISTERED") || strings.Contains(text, "SENDER_ID_MISMATCH")
}

// fcmAuthFailure reports whether a response could be fixed by minting a fresh
// OAuth token.
func fcmAuthFailure(status int, body []byte) bool {
	if status == http.StatusUnauthorized {
		return true
	}
	if status != http.StatusForbidden {
		return false
	}
	if envelope, ok := parseFCMError(body); ok {
		if envelope.Error.Status == "UNAUTHENTICATED" {
			return true
		}
		for _, detail := range envelope.Error.Details {
			if detail.ErrorCode == "UNAUTHENTICATED" {
				return true
			}
		}
		return false
	}
	return strings.Contains(string(body), "UNAUTHENTICATED")
}

// resetFCMToken drops the cached OAuth token so the next lookup mints a new one.
func (s *Service) resetFCMToken() {
	s.fcmMu.Lock()
	s.fcmToken = ""
	s.fcmTokenExp = time.Time{}
	s.fcmMu.Unlock()
}

// fcmEnabled reports whether FCM credentials are present and usable.
func (s *Service) fcmEnabled() bool {
	account, err := s.fcmAccount()
	return err == nil && account != nil
}

func (s *Service) fcmAccount() (*fcmServiceAccount, error) {
	if s == nil || s.Cfg == nil || strings.TrimSpace(s.Cfg.FCMServiceAccountJSON) == "" {
		return nil, nil
	}
	var account fcmServiceAccount
	if err := json.Unmarshal([]byte(s.Cfg.FCMServiceAccountJSON), &account); err != nil {
		return nil, fmt.Errorf("parse fcm service account: %w", err)
	}
	project := strings.TrimSpace(s.Cfg.FCMProjectID)
	if project == "" {
		project = strings.TrimSpace(account.ProjectID)
	}
	if strings.TrimSpace(account.ClientEmail) == "" || strings.TrimSpace(account.PrivateKey) == "" || project == "" {
		return nil, fmt.Errorf("fcm service account is incomplete")
	}
	account.ProjectID = project
	account.TokenURI = fcmTokenURI(account.TokenURI)
	key, err := parseFCMPrivateKey(account.PrivateKey)
	if err != nil {
		return nil, err
	}
	account.key = key
	return &account, nil
}

// fcmTokenURI only honours a service-account token_uri that points at an HTTPS
// Google endpoint. Anything else falls back to the production default so a
// crafted service account cannot redirect the assertion to another host.
func fcmTokenURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fcmDefaultTokenURI
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return fcmDefaultTokenURI
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".googleapis.com") {
		return fcmDefaultTokenURI
	}
	return value
}

func parseFCMPrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, fmt.Errorf("fcm private key is not PEM")
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("fcm private key is not RSA")
		}
		return key, nil
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse fcm private key: %w", err)
	}
	return key, nil
}

// fcmAccessToken mints an OAuth assertion and exchanges it for a bearer token,
// reusing the cached token until shortly before it expires.
func (s *Service) fcmAccessToken(ctx context.Context, account *fcmServiceAccount) (string, error) {
	s.fcmMu.Lock()
	defer s.fcmMu.Unlock()
	if s.fcmToken != "" && time.Now().UTC().Before(s.fcmTokenExp) {
		return s.fcmToken, nil
	}
	assertion, err := fcmAssertion(account, time.Now().UTC())
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, account.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fcm token exchange returned %s", resp.Status)
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tokenResp); err != nil {
		return "", fmt.Errorf("decode fcm token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("fcm token response is missing an access token")
	}
	ttl := tokenResp.ExpiresIn
	s.fcmToken = tokenResp.AccessToken
	s.fcmTokenExp = fcmTokenExpiry(time.Now().UTC(), ttl)
	return s.fcmToken, nil
}

// fcmTokenExpiry leaves a safety margin before the token's nominal expiry. A
// short or zero TTL is treated as the usual hour so a bad response cannot force
// an exchange on every send.
func fcmTokenExpiry(at time.Time, ttlSeconds int) time.Time {
	if ttlSeconds < 300 {
		ttlSeconds = 3600
	}
	margin := ttlSeconds / 10
	if margin < 60 {
		margin = 60
	}
	return at.Add(time.Duration(ttlSeconds)*time.Second - time.Duration(margin)*time.Second)
}

// fcmAssertion builds the signed RS256 JWT assertion used for the token
// exchange. It is separate from the exchange so tests can verify it offline.
func fcmAssertion(account *fcmServiceAccount, at time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"iss":   account.ClientEmail,
		"scope": fcmScope,
		"aud":   account.TokenURI,
		"iat":   at.Unix(),
		"exp":   at.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}
	encode := base64.RawURLEncoding.EncodeToString
	signingInput := encode(header) + "." + encode(claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, account.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + encode(signature), nil
}
