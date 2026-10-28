package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"

	"sodapop/ent"
	"sodapop/ent/pushsubscription"
	"sodapop/ent/usersetting"
	"sodapop/internal/db"
)

const defaultVAPIDSubject = "mailto:admin@sodapop.local"

// maxPushEndpointLen caps the stored endpoint. Browser endpoints are far
// shorter; the limit exists to bound storage and request work.
const maxPushEndpointLen = 2048

// pushServiceHosts are the only hostnames a push endpoint may use. Entries are
// base domains: the host must equal the base or be a subdomain of it. Apple
// uses web.push.apple.com and Mozilla uses
// updates.push.services.mozilla.com, which both match as subdomains.
var pushServiceHosts = []string{
	"fcm.googleapis.com",
	"push.apple.com",
	"mozilla.com",
	"notify.windows.com",
}

// validatePushEndpoint rejects endpoints that the server would later POST to
// but that are not operated by a known push service. This prevents a member
// from registering an internal or cloud-metadata URL and turning a
// notification into a server-side request (SSRF). Errors deliberately omit the
// endpoint so it never reaches the logs.
func validatePushEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("push endpoint is required")
	}
	if len(endpoint) > maxPushEndpointLen {
		return fmt.Errorf("push endpoint is invalid")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("push endpoint is invalid")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("push endpoint is invalid")
	}
	if u.Host == "" || u.Opaque != "" {
		return fmt.Errorf("push endpoint is invalid")
	}
	if u.User != nil {
		return fmt.Errorf("push endpoint is invalid")
	}
	if u.Fragment != "" || u.RawFragment != "" {
		return fmt.Errorf("push endpoint is invalid")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return fmt.Errorf("push endpoint is invalid")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("push endpoint is invalid")
	}
	// IP literals are never push services; reject them before host matching so
	// loopback, link-local, private and unique-local addresses cannot slip in.
	if net.ParseIP(host) != nil {
		return fmt.Errorf("push endpoint is invalid")
	}
	for _, allowed := range pushServiceHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return nil
		}
	}
	return fmt.Errorf("push endpoint is invalid")
}

// pushDeliveryHook lets tests observe push delivery without VAPID keys. It is
// nil in production.
var pushDeliveryHook func(ctx context.Context, subs []*ent.PushSubscription, title, body, url string) error

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

// SubscribePush registers or refreshes a browser push subscription. An
// endpoint belongs to exactly one user. A successful subscribe also enables
// the user's push preference: subscribing is an explicit opt-in, and delivery
// is otherwise skipped while push_enabled is false.
func (s *Service) SubscribePush(ctx context.Context, userID, endpoint, p256dh, auth string) error {
	endpoint = strings.TrimSpace(endpoint)
	p256dh = strings.TrimSpace(p256dh)
	auth = strings.TrimSpace(auth)
	if endpoint == "" || p256dh == "" || auth == "" {
		return fmt.Errorf("endpoint, p256dh and auth are required")
	}
	if err := s.upsertPushSubscription(ctx, userID, endpoint, p256dh, auth); err != nil {
		return err
	}
	return s.enablePush(ctx, userID)
}

// upsertPushSubscription stores a registration idempotently. Refreshing an
// existing endpoint keeps one row; the preference is updated by the caller.
func (s *Service) upsertPushSubscription(ctx context.Context, userID, endpoint, p256dh, auth string) error {
	if err := validatePushEndpoint(endpoint); err != nil {
		return err
	}
	existing, err := s.Client.PushSubscription.Query().
		Where(pushsubscription.Endpoint(endpoint)).
		Only(ctx)
	switch {
	case err == nil:
		if existing.UserID != userID {
			return fmt.Errorf("push subscription belongs to another user")
		}
		if existing.P256dh == p256dh && existing.Auth == auth {
			return nil
		}
		_, err = existing.Update().SetP256dh(p256dh).SetAuth(auth).Save(ctx)
		return err
	case ent.IsNotFound(err):
		_, err = s.Client.PushSubscription.Create().
			SetUserID(userID).
			SetEndpoint(endpoint).
			SetP256dh(p256dh).
			SetAuth(auth).
			SetCreatedAt(now()).
			Save(ctx)
		return err
	default:
		return err
	}
}

// enablePush flips push_enabled on for a user in the same request as the
// subscribe. A preference row may not exist yet, so create it when absent.
func (s *Service) enablePush(ctx context.Context, userID string) error {
	n, err := s.Client.UserSetting.Update().
		Where(usersetting.UserID(userID)).
		SetPushEnabled(true).
		SetUpdatedAt(db.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.Client.UserSetting.Create().
		SetUserID(userID).
		SetPushEnabled(true).
		SetUpdatedAt(db.Now()).
		Save(ctx)
	if ent.IsConstraintError(err) {
		// Another request created the row between our update and create.
		_, err = s.Client.UserSetting.Update().
			Where(usersetting.UserID(userID)).
			SetPushEnabled(true).
			SetUpdatedAt(db.Now()).
			Save(ctx)
	}
	return err
}

// UnsubscribePush removes one browser push registration.
func (s *Service) UnsubscribePush(ctx context.Context, userID, endpoint string) error {
	_, err := s.Client.PushSubscription.Delete().
		Where(pushsubscription.UserID(userID), pushsubscription.Endpoint(endpoint)).
		Exec(ctx)
	return err
}

// SendPush delivers a notification to every push subscription owned by a
// user. It is a no-op when VAPID is not configured.
func (s *Service) SendPush(ctx context.Context, userID, title, body, url string) error {
	subs, err := s.Client.PushSubscription.Query().
		Where(pushsubscription.UserID(userID)).
		All(ctx)
	if err != nil {
		return err
	}
	return s.sendPushToSubscriptions(ctx, subs, title, body, url)
}

func (s *Service) sendPushToSubscriptions(ctx context.Context, subs []*ent.PushSubscription, title, body, url string) error {
	if pushDeliveryHook != nil {
		return pushDeliveryHook(ctx, subs, title, body, url)
	}
	if len(subs) == 0 {
		return nil
	}
	pub, priv, subject, enabled, err := vapidConfig()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	message, err := json.Marshal(pushPayload{Title: title, Body: body, URL: url})
	if err != nil {
		return err
	}
	options := &webpush.Options{
		Subscriber:      subject,
		VAPIDPublicKey:  pub,
		VAPIDPrivateKey: priv,
		TTL:             24 * 60 * 60,
		HTTPClient:      s.httpClient(),
	}

	var firstErr error
	for _, sub := range subs {
		resp, sendErr := webpush.SendNotificationWithContext(ctx, message, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				Auth:   sub.Auth,
				P256dh: sub.P256dh,
			},
		}, options)
		if resp != nil {
			if resp.Body != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
				_ = resp.Body.Close()
			}
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
				_, _ = s.Client.PushSubscription.Delete().
					Where(pushsubscription.ID(sub.ID)).
					Exec(ctx)
			}
			if sendErr == nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
				sendErr = fmt.Errorf("push endpoint returned %s", resp.Status)
			}
		}
		if sendErr != nil && firstErr == nil {
			firstErr = sendErr
		}
	}
	return firstErr
}

// vapidConfig reads VAPID credentials from the environment. Keys are
// validated as base64url; when either key is absent push is disabled.
func vapidConfig() (pub, priv, subject string, enabled bool, err error) {
	pub = strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY"))
	priv = strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY"))
	if pub == "" || priv == "" {
		return "", "", "", false, nil
	}
	if _, err := decodeBase64URL(pub); err != nil {
		return "", "", "", false, fmt.Errorf("VAPID_PUBLIC_KEY: %w", err)
	}
	if _, err := decodeBase64URL(priv); err != nil {
		return "", "", "", false, fmt.Errorf("VAPID_PRIVATE_KEY: %w", err)
	}
	subject = strings.TrimSpace(os.Getenv("VAPID_SUBJECT"))
	if subject == "" {
		subject = defaultVAPIDSubject
	}
	return pub, priv, subject, true, nil
}

func decodeBase64URL(v string) ([]byte, error) {
	if raw, err := base64.RawURLEncoding.DecodeString(v); err == nil {
		return raw, nil
	}
	return base64.URLEncoding.DecodeString(v)
}
