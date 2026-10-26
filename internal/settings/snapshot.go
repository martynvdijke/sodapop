package settings

import (
	"log/slog"
	"os"
	"strconv"
	"strings"

	"sodapop/ent"
	"sodapop/internal/config"
	"sodapop/internal/secretbox"
)

// SMTPSettings is the effective outbound mail configuration.
type SMTPSettings struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLS      string
}

// UmamiSettings is the effective analytics configuration.
type UmamiSettings struct {
	URL       string
	WebsiteID string
}

// Snapshot is an immutable view of every runtime setting. Consumers hold the
// pointer returned by Service.Snapshot; it is never mutated after publication.
type Snapshot struct {
	siteName              string
	baseURL               string
	smtp                  SMTPSettings
	umami                 UmamiSettings
	registrationOpen      bool
	aiEnabled             bool
	allowPasswordFallback bool
	allowedEmails         []string
	allowedDomains        []string
	otelEndpoint          string
	otelServiceName       string
	otelSampler           string
	otelDisabled          bool
	sources               map[string]string
	secretLast4           map[string]string
}

// SiteName returns the configured site name.
func (s *Snapshot) SiteName() string { return s.siteName }

// BaseURL returns the configured public base URL without a trailing slash.
func (s *Snapshot) BaseURL() string { return s.baseURL }

// SMTP returns the effective SMTP settings, including the decrypted password.
func (s *Snapshot) SMTP() SMTPSettings { return s.smtp }

// Umami returns the effective analytics settings.
func (s *Snapshot) Umami() UmamiSettings { return s.umami }

// RegistrationOpen reports whether local registration is enabled.
func (s *Snapshot) RegistrationOpen() bool { return s.registrationOpen }

// AIEnabled reports whether AI features are globally enabled.
func (s *Snapshot) AIEnabled() bool { return s.aiEnabled }

// AllowPasswordFallback reports whether local password login is allowed.
func (s *Snapshot) AllowPasswordFallback() bool { return s.allowPasswordFallback }

// AllowedEmails returns a copy of the email allow-list.
func (s *Snapshot) AllowedEmails() []string {
	return append([]string(nil), s.allowedEmails...)
}

// AllowedDomains returns a copy of the domain allow-list.
func (s *Snapshot) AllowedDomains() []string {
	return append([]string(nil), s.allowedDomains...)
}

// OTELEndpoint returns the configured OTLP endpoint. An empty value means
// traces are not exported.
func (s *Snapshot) OTELEndpoint() string { return s.otelEndpoint }

// OTELServiceName returns the configured OpenTelemetry service name.
func (s *Snapshot) OTELServiceName() string { return s.otelServiceName }

// OTELSampler returns the configured trace sampler name.
func (s *Snapshot) OTELSampler() string { return s.otelSampler }

// OTELDisabled reports whether the OpenTelemetry SDK is disabled outright.
func (s *Snapshot) OTELDisabled() bool { return s.otelDisabled }

// Source reports where the effective value came from: default, env or db.
// Unknown keys return an empty string.
func (s *Snapshot) Source(key string) string {
	if s == nil {
		return ""
	}
	return s.sources[key]
}

// SecretState exposes only whether a secret is configured and its last four
// characters. It never returns plaintext or ciphertext.
func (s *Snapshot) SecretState(key string) (bool, string) {
	if s == nil {
		return false, ""
	}
	last4, ok := s.secretLast4[key]
	return ok, last4
}

// buildSnapshot merges built-in defaults < config (env) < database rows.
// Invalid or undecryptable database values are skipped with a warning so a bad
// row can never prevent startup or serve a partially trusted value.
func buildSnapshot(cfg *config.Config, rows []*ent.RuntimeSetting, box *secretbox.Box, log *slog.Logger) *Snapshot {
	values := builtinDefaults()
	sources := make(map[string]string, len(values))
	for key := range values {
		sources[key] = "default"
	}

	applyEnv(cfg, values, sources)
	applyOTELEnv(values, sources)

	for _, row := range rows {
		if row == nil {
			continue
		}
		key := row.Key
		spec, known := specs[key]
		if !known {
			// Rows for env-only or unknown keys are inert, never overrides.
			continue
		}

		raw := row.Value
		if spec.secret || row.IsSecret || row.EncryptedValue != "" {
			if row.EncryptedValue == "" {
				log.Warn("settings secret row has no ciphertext; keeping env/default", "key", key)
				continue
			}
			plaintext, err := box.Decrypt(row.EncryptedValue)
			if err != nil {
				log.Warn("settings secret could not be decrypted; keeping env/default", "key", key, "err", err)
				continue
			}
			raw = plaintext
		}

		normalized, err := normalizeValue(key, raw)
		if err != nil {
			log.Warn("settings row is invalid; keeping env/default", "key", key, "err", err)
			continue
		}
		values[key] = normalized
		sources[key] = "db"
	}

	// Canonicalise env/default values too; a malformed env value falls back to
	// the built-in default rather than poisoning the snapshot.
	defaults := builtinDefaults()
	for key := range values {
		normalized, err := normalizeValue(key, values[key])
		if err != nil {
			values[key] = defaults[key]
			if sources[key] == "db" {
				sources[key] = "default"
			}
			continue
		}
		values[key] = normalized
	}

	port, _ := strconv.Atoi(values[KeySMTPPort])
	snap := &Snapshot{
		siteName: values[KeyAppName],
		baseURL:  values[KeyBaseURL],
		smtp: SMTPSettings{
			Host:     values[KeySMTPHost],
			Port:     port,
			Username: values[KeySMTPUsername],
			Password: values[KeySMTPPassword],
			From:     values[KeySMTPFrom],
			TLS:      values[KeySMTPTLS],
		},
		umami: UmamiSettings{
			URL:       values[KeyUmamiURL],
			WebsiteID: values[KeyUmamiWebsiteID],
		},
		registrationOpen:      values[KeyRegistrationOpen] == "1",
		aiEnabled:             values[KeyAIEnabled] == "1",
		allowPasswordFallback: values[KeyAllowPasswordFallback] == "1",
		allowedEmails:         splitList(values[KeyAllowedEmails]),
		allowedDomains:        splitList(values[KeyAllowedDomains]),
		otelEndpoint:          values[KeyOTELEndpoint],
		otelServiceName:       values[KeyOTELServiceName],
		otelSampler:           values[KeyOTELSampler],
		otelDisabled:          values[KeyOTELDisabled] == "1",
		sources:               sources,
		secretLast4:           make(map[string]string),
	}
	for key, spec := range specs {
		if !spec.secret {
			continue
		}
		if last4, ok := lastFour(values[key]); ok {
			snap.secretLast4[key] = last4
		}
	}
	return snap
}

// applyEnv overlays resolved config values that differ from built-in defaults.
// config.Load applies its own defaults, so equality is what distinguishes an
// explicit environment value from an untouched one.
func applyEnv(cfg *config.Config, values, sources map[string]string) {
	if cfg == nil {
		return
	}
	set := func(key, value string) {
		values[key] = value
		sources[key] = "env"
	}

	if cfg.AppName != "" && cfg.AppName != defaultAppName {
		set(KeyAppName, cfg.AppName)
	}
	if base := strings.TrimRight(cfg.BaseURL, "/"); base != "" && base != defaultBaseURL {
		set(KeyBaseURL, base)
	}
	if cfg.SMTPHost != "" {
		set(KeySMTPHost, cfg.SMTPHost)
	}
	if cfg.SMTPPort != 0 && cfg.SMTPPort != defaultSMTPPort {
		set(KeySMTPPort, strconv.Itoa(cfg.SMTPPort))
	}
	if cfg.SMTPUser != "" {
		set(KeySMTPUsername, cfg.SMTPUser)
	}
	if cfg.SMTPPass != "" {
		set(KeySMTPPassword, cfg.SMTPPass)
	}
	if cfg.SMTPFrom != "" && cfg.SMTPFrom != defaultSMTPFrom {
		set(KeySMTPFrom, cfg.SMTPFrom)
	}
	if cfg.SMTPTLS != "" && cfg.SMTPTLS != defaultSMTPTLS {
		set(KeySMTPTLS, cfg.SMTPTLS)
	}
	if cfg.UmamiURL != "" {
		set(KeyUmamiURL, cfg.UmamiURL)
	}
	if cfg.UmamiWebsiteID != "" {
		set(KeyUmamiWebsiteID, cfg.UmamiWebsiteID)
	}
	if cfg.PasswordFallback != defaultAllowPasswordFallback {
		set(KeyAllowPasswordFallback, boolString(cfg.PasswordFallback))
	}
	if len(cfg.AllowedEmails) > 0 {
		set(KeyAllowedEmails, strings.Join(cfg.AllowedEmails, ","))
	}
	if len(cfg.AllowedDomains) > 0 {
		set(KeyAllowedDomains, strings.Join(cfg.AllowedDomains, ","))
	}
}

// applyOTELEnv mirrors the standard OTEL_* variables read by telemetry.FromEnv.
// They remain a fallback layer: a database row still wins, and an unset variable
// leaves the built-in default in place.
func applyOTELEnv(values, sources map[string]string) {
	set := func(key, value string) {
		if value == "" {
			return
		}
		values[key] = value
		sources[key] = "env"
	}

	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	}
	set(KeyOTELEndpoint, endpoint)
	set(KeyOTELServiceName, strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")))
	set(KeyOTELSampler, strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	set(KeyOTELDisabled, strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")))
}

func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func lastFour(secret string) (string, bool) {
	if secret == "" {
		return "", false
	}
	runes := []rune(secret)
	if len(runes) <= 4 {
		return "****", true
	}
	return string(runes[len(runes)-4:]), true
}
