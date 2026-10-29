package settings

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// DefaultVAPIDSubject is used when no subject is configured at runtime or in
// the environment. It only has to be a contact the push service can reach.
const DefaultVAPIDSubject = "mailto:admin@sodapop.local"

// Runtime-safe setting keys. env-only configuration is intentionally not in
// this list and can never be stored in runtime_settings.
const (
	KeyAppName               = "app_name"
	KeyBaseURL               = "base_url"
	KeySMTPHost              = "smtp_host"
	KeySMTPPort              = "smtp_port"
	KeySMTPUsername          = "smtp_username"
	KeySMTPPassword          = "smtp_password"
	KeySMTPFrom              = "smtp_from"
	KeySMTPTLS               = "smtp_tls"
	KeyUmamiURL              = "umami_url"
	KeyUmamiWebsiteID        = "umami_website_id"
	KeyVAPIDPublicKey        = "vapid_public_key"
	KeyVAPIDPrivateKey       = "vapid_private_key"
	KeyVAPIDSubject          = "vapid_subject"
	KeyRegistrationOpen      = "registration_open"
	KeyAIEnabled             = "ai_enabled"
	KeyAllowPasswordFallback = "allow_password_fallback"
	KeyAllowedEmails         = "allowed_emails"
	KeyAllowedDomains        = "allowed_domains"
	KeyOTELEndpoint          = "otel_exporter_otlp_endpoint"
	KeyOTELServiceName       = "otel_service_name"
	KeyOTELSampler           = "otel_sampler"
	KeyOTELDisabled          = "otel_sdk_disabled"
)

// Errors returned by validation and write paths.
var (
	ErrUnknownKey   = errors.New("settings: unknown key")
	ErrEnvOnlyKey   = errors.New("settings: key is controlled by the environment")
	ErrSecretKey    = errors.New("settings: key cannot hold a secret")
	ErrInvalidValue = errors.New("settings: invalid value")
)

// envOnlyKeys are never overridable from the database. They are listed for
// diagnostics and to document the boundary; Set rejects them before this
// package ever writes a row.
var envOnlyKeys = map[string]bool{
	"env":            true,
	"port":           true,
	"database_path":  true,
	"data_dir":       true,
	"session_secret": true,
	"email_dev_dir":  true,
	"seed_demo":      true,
}

func isEnvOnlyKey(key string) bool {
	return envOnlyKeys[strings.ToLower(key)]
}

// Validate checks a non-secret value against the same rules Set applies. It is
// exported so callers can validate a multi-field form before persisting any of
// it. Secret values are not validated here; Set treats blank as "keep".
func Validate(key, value string) error {
	spec, ok := specs[key]
	if !ok {
		if isEnvOnlyKey(key) {
			return fmt.Errorf("%w: %s", ErrEnvOnlyKey, key)
		}
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if spec.secret {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		_, err := normalizeValue(key, value)
		return err
	}
	_, err := normalizeValue(key, value)
	return err
}

type keySpec struct {
	secret bool
}

var specs = map[string]keySpec{
	KeyAppName:               {},
	KeyBaseURL:               {},
	KeySMTPHost:              {},
	KeySMTPPort:              {},
	KeySMTPUsername:          {},
	KeySMTPPassword:          {secret: true},
	KeySMTPFrom:              {},
	KeySMTPTLS:               {},
	KeyUmamiURL:              {},
	KeyUmamiWebsiteID:        {},
	KeyVAPIDPublicKey:        {},
	KeyVAPIDPrivateKey:       {secret: true},
	KeyVAPIDSubject:          {},
	KeyRegistrationOpen:      {},
	KeyAIEnabled:             {},
	KeyAllowPasswordFallback: {},
	KeyAllowedEmails:         {},
	KeyAllowedDomains:        {},
	KeyOTELEndpoint:          {},
	KeyOTELServiceName:       {},
	KeyOTELSampler:           {},
	KeyOTELDisabled:          {},
}

const (
	defaultAppName               = "Sodapop"
	defaultBaseURL               = "http://localhost:8097"
	defaultSMTPPort              = 587
	defaultSMTPFrom              = "Sodapop <no-reply@sodapop.local>"
	defaultSMTPTLS               = "starttls"
	defaultRegistrationOpen      = true
	defaultAIEnabled             = false
	defaultAllowPasswordFallback = true
	defaultOTELSampler           = "parentbased_traceidratio"
)

func builtinDefaults() map[string]string {
	return map[string]string{
		KeyAppName:               defaultAppName,
		KeyBaseURL:               defaultBaseURL,
		KeySMTPHost:              "",
		KeySMTPPort:              strconv.Itoa(defaultSMTPPort),
		KeySMTPUsername:          "",
		KeySMTPPassword:          "",
		KeySMTPFrom:              defaultSMTPFrom,
		KeySMTPTLS:               defaultSMTPTLS,
		KeyUmamiURL:              "",
		KeyUmamiWebsiteID:        "",
		KeyVAPIDPublicKey:        "",
		KeyVAPIDPrivateKey:       "",
		KeyVAPIDSubject:          "",
		KeyRegistrationOpen:      boolString(defaultRegistrationOpen),
		KeyAIEnabled:             boolString(defaultAIEnabled),
		KeyAllowPasswordFallback: boolString(defaultAllowPasswordFallback),
		KeyAllowedEmails:         "",
		KeyAllowedDomains:        "",
		KeyOTELEndpoint:          "",
		KeyOTELServiceName:       "",
		KeyOTELSampler:           defaultOTELSampler,
		KeyOTELDisabled:          "0",
	}
}

// normalizeValue validates and canonicalises a value before it is stored or
// merged into a snapshot. It never handles secret plaintext storage.
func normalizeValue(key, raw string) (string, error) {
	switch key {
	case KeyAppName:
		v := strings.TrimSpace(raw)
		if v == "" {
			return "", invalid("%s is required", key)
		}
		if len(v) > 100 {
			return "", invalid("%s is too long", key)
		}
		if hasControl(v) {
			return "", invalid("%s contains control characters", key)
		}
		return v, nil

	case KeyBaseURL:
		return normalizeBaseURL(raw)

	case KeySMTPHost:
		v := strings.TrimSpace(raw)
		if len(v) > 255 || hasControl(v) || strings.ContainsAny(v, " \t") {
			return "", invalid("%s is not a valid host", key)
		}
		return v, nil

	case KeySMTPPort:
		v := strings.TrimSpace(raw)
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return "", invalid("%s must be between 1 and 65535", key)
		}
		return strconv.Itoa(port), nil

	case KeySMTPUsername:
		v := strings.TrimSpace(raw)
		if len(v) > 320 || hasControl(v) {
			return "", invalid("%s is invalid", key)
		}
		return v, nil

	case KeySMTPPassword:
		// Secrets are encrypted, never normalised. Empty is handled by Set.
		return raw, nil

	case KeySMTPFrom:
		v := strings.TrimSpace(raw)
		if v == "" {
			return "", invalid("%s is required", key)
		}
		if len(v) > 320 || strings.ContainsAny(v, "\r\n") {
			return "", invalid("%s is invalid", key)
		}
		return v, nil

	case KeySMTPTLS:
		v := strings.ToLower(strings.TrimSpace(raw))
		switch v {
		case "starttls", "tls", "none":
			return v, nil
		case "ssl", "465":
			return "tls", nil
		default:
			return "", invalid("%s must be starttls, tls or none", key)
		}

	case KeyUmamiURL:
		return normalizeOrigin(raw)

	case KeyUmamiWebsiteID:
		v := strings.TrimSpace(raw)
		if len(v) > 100 || hasControl(v) || strings.ContainsAny(v, " \t") {
			return "", invalid("%s is invalid", key)
		}
		return v, nil

	case KeyVAPIDPublicKey, KeyVAPIDPrivateKey:
		return normalizeVAPIDKey(key, raw)

	case KeyVAPIDSubject:
		return normalizeVAPIDSubject(raw)

	case KeyRegistrationOpen, KeyAIEnabled, KeyAllowPasswordFallback:
		v, err := parseBool(raw)
		if err != nil {
			return "", invalid("%s must be a boolean", key)
		}
		return boolString(v), nil

	case KeyAllowedEmails:
		return normalizeList(raw, true)

	case KeyAllowedDomains:
		return normalizeList(raw, false)

	case KeyOTELEndpoint:
		return normalizeOTELEndpoint(raw)

	case KeyOTELServiceName:
		v := strings.TrimSpace(raw)
		if v == "" {
			return "", nil
		}
		if len(v) > 100 || hasControl(v) {
			return "", invalid("%s must be 1-100 characters without control characters", key)
		}
		return v, nil

	case KeyOTELSampler:
		v := strings.ToLower(strings.TrimSpace(raw))
		switch v {
		case "always_on", "always_off", "traceidratio",
			"parentbased_always_on", "parentbased_always_off", "parentbased_traceidratio":
			return v, nil
		default:
			return "", invalid("%s must be a supported sampler", key)
		}

	case KeyOTELDisabled:
		v, err := parseBool(raw)
		if err != nil {
			return "", invalid("%s must be a boolean", key)
		}
		return boolString(v), nil
	}
	return "", fmt.Errorf("%w: %s", ErrUnknownKey, key)
}

func normalizeBaseURL(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", invalid("%s is required", KeyBaseURL)
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", invalid("%s must be an absolute http(s) URL", KeyBaseURL)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", invalid("%s must be http or https", KeyBaseURL)
	}
	if !validHost(u.Hostname()) {
		return "", invalid("%s must use a valid host", KeyBaseURL)
	}
	out := scheme + "://" + strings.ToLower(u.Host) + u.EscapedPath()
	return strings.TrimRight(out, "/"), nil
}

// normalizeOTELEndpoint accepts an empty value (export disabled) or an
// absolute http(s) URL. OTLP endpoints may include a path but never credentials
// or fragment markers.
func normalizeOTELEndpoint(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	if hasControl(v) {
		return "", invalid("%s contains control characters", KeyOTELEndpoint)
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", invalid("%s must be an absolute http(s) URL", KeyOTELEndpoint)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", invalid("%s must be http or https", KeyOTELEndpoint)
	}
	return u.String(), nil
}

// normalizeOrigin accepts only an http(s) origin: no credentials, path, query
// or fragment. This value is rendered into script tags and CSP headers.
func normalizeOrigin(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", invalid("%s must be an http(s) origin", KeyUmamiURL)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", invalid("%s must be http or https", KeyUmamiURL)
	}
	if u.Path != "" && u.Path != "/" {
		return "", invalid("%s must not include a path", KeyUmamiURL)
	}
	if u.Opaque != "" {
		return "", invalid("%s must be an http(s) origin", KeyUmamiURL)
	}
	if !validHost(u.Hostname()) {
		return "", invalid("%s must use a valid host", KeyUmamiURL)
	}
	return scheme + "://" + strings.ToLower(u.Host), nil
}

// normalizeVAPIDKey accepts an empty value (unset) or an unpadded URL-safe
// base64 value that decodes to a P-256 key. A public key is 65 bytes and a
// private key is a 32-byte scalar; both lengths are accepted in either field
// because the generated key pair is pasted as a pair.
func normalizeVAPIDKey(key, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	if hasControl(v) || strings.ContainsAny(v, " \t=+/") {
		return "", invalid("%s must be unpadded base64url", key)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return "", invalid("%s must be unpadded base64url", key)
	}
	if len(decoded) != 32 && len(decoded) != 65 {
		return "", invalid("%s is not a P-256 key", key)
	}
	return v, nil
}

// normalizeVAPIDSubject accepts an empty value (use the default) or a mailto:
// address / absolute https URL that a push service can use as a contact.
func normalizeVAPIDSubject(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	if hasControl(v) || strings.ContainsAny(v, " \t\r\n") {
		return "", invalid("%s is invalid", KeyVAPIDSubject)
	}
	lower := strings.ToLower(v)
	if strings.HasPrefix(lower, "mailto:") {
		if strings.TrimSpace(v[len("mailto:"):]) == "" {
			return "", invalid("%s must include a mailto address", KeyVAPIDSubject)
		}
		return v, nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil {
		return "", invalid("%s must start with mailto: or https://", KeyVAPIDSubject)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", invalid("%s must start with mailto: or https://", KeyVAPIDSubject)
	}
	return u.String(), nil
}

// validHost restricts hostnames to characters safe to interpolate into CSP
// directives and URLs. IPv6 literals are accepted when net.ParseIP parses the
// colon-containing hostname.
func validHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if strings.Contains(host, ":") {
		return net.ParseIP(host) != nil
	}
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizeList(raw string, emails bool) (string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(fields) > 200 {
		return "", invalid("too many list entries (maximum 200)")
	}
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		v := strings.ToLower(strings.TrimSpace(f))
		if v == "" {
			continue
		}
		if len(v) > 320 || hasControl(v) {
			return "", invalid("list entry is invalid")
		}
		if emails {
			at := strings.LastIndexByte(v, '@')
			if at <= 0 || at == len(v)-1 || strings.Count(v, "@") != 1 {
				return "", invalid("%q is not a valid email address", v)
			}
		} else {
			v = strings.TrimPrefix(v, "@")
			if v == "" || strings.ContainsAny(v, "@/\t ") {
				return "", invalid("%q is not a valid domain", f)
			}
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return strings.Join(out, ","), nil
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, ErrInvalidValue
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func hasControl(v string) bool {
	for _, r := range v {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidValue, fmt.Sprintf(format, args...))
}
