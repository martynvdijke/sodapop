package settings

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

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
	KeyRegistrationOpen      = "registration_open"
	KeyAIEnabled             = "ai_enabled"
	KeyAllowPasswordFallback = "allow_password_fallback"
	KeyAllowedEmails         = "allowed_emails"
	KeyAllowedDomains        = "allowed_domains"
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

// isEnvOnlyKey also matches the OTEL_* family.
func isEnvOnlyKey(key string) bool {
	key = strings.ToLower(key)
	return envOnlyKeys[key] || strings.HasPrefix(key, "otel_")
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
	KeyRegistrationOpen:      {},
	KeyAIEnabled:             {},
	KeyAllowPasswordFallback: {},
	KeyAllowedEmails:         {},
	KeyAllowedDomains:        {},
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
		KeyRegistrationOpen:      boolString(defaultRegistrationOpen),
		KeyAIEnabled:             boolString(defaultAIEnabled),
		KeyAllowPasswordFallback: boolString(defaultAllowPasswordFallback),
		KeyAllowedEmails:         "",
		KeyAllowedDomains:        "",
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
	out := scheme + "://" + strings.ToLower(u.Host) + u.EscapedPath()
	return strings.TrimRight(out, "/"), nil
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
	return scheme + "://" + strings.ToLower(u.Host), nil
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
