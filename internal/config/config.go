package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	AppName    string
	Env        string
	Port       string
	BaseURL    string
	DataDir    string
	DBPath     string
	Version    string
	LogLevel   string
	SessionKey []byte
	SessionTTL time.Duration
	BcryptCost int

	OIDCEnabled      bool
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	OIDCScopes       []string
	AllowedEmails    []string
	AllowedDomains   []string
	PasswordFallback bool

	SMTPHost    string
	SMTPPort    int
	SMTPUser    string
	SMTPPass    string
	SMTPTLS     string
	SMTPFrom    string
	EmailDevDir string

	OFFBaseURL   string
	OFFUserAgent string
	OFFEnabled   bool

	UmamiURL       string
	UmamiWebsiteID string

	FCMServiceAccountJSON string
	FCMProjectID          string

	RateLimitPerMin int
	TrustedProxies  []string

	// Legal identity of the instance operator. These appear verbatim in the
	// published privacy policy, so they are deployment configuration rather
	// than runtime-editable content: a self-hoster sets them once and the page
	// states only what is true for their deployment.
	LegalControllerName string
	LegalContactEmail   string
	LegalContactAddress string
	LegalHostingRegion  string
}

// Load reads configuration from the environment, applying safe local defaults.
func Load() (*Config, error) {
	c := &Config{
		AppName:          env("APP_NAME", "Sodapop"),
		Env:              env("ENV", "development"),
		Port:             env("PORT", "8097"),
		BaseURL:          strings.TrimRight(env("BASE_URL", "http://localhost:8097"), "/"),
		DataDir:          env("DATA_DIR", "data"),
		DBPath:           env("DATABASE_PATH", "data/sodapop.db"),
		Version:          env("VERSION", "0.0.0-dev"),
		LogLevel:         env("LOG_LEVEL", "info"),
		BcryptCost:       envInt("BCRYPT_COST", 12),
		OIDCEnabled:      envBool("OIDC_ENABLED", false),
		OIDCIssuer:       strings.TrimRight(env("OIDC_ISSUER_URL", ""), "/"),
		OIDCClientID:     env("OIDC_CLIENT_ID", ""),
		OIDCRedirectURL:  env("OIDC_REDIRECT_URL", env("BASE_URL", "http://localhost:8097")+"/auth/oidc/callback"),
		PasswordFallback: envBool("ALLOW_PASSWORD_FALLBACK", !envBool("OIDC_ENABLED", false)),
		SMTPHost:         env("SMTP_HOST", ""),
		SMTPPort:         envInt("SMTP_PORT", 587),
		SMTPUser:         env("SMTP_USER", ""),
		SMTPTLS:          env("SMTP_TLS", "starttls"),
		SMTPFrom:         env("SMTP_FROM", "Sodapop <no-reply@sodapop.local>"),
		EmailDevDir:      env("EMAIL_DEV_DIR", "data/outbox"),
		OFFBaseURL:       strings.TrimRight(env("OPENFOODFACTS_URL", "https://world.openfoodfacts.org"), "/"),
		OFFUserAgent:     env("OPENFOODFACTS_USER_AGENT", "Sodapop/"+env("VERSION", "0.0.0-dev")+" (admin@sodapop.local)"),
		OFFEnabled:       envBool("OPENFOODFACTS_ENABLED", true),
		UmamiURL:         strings.TrimRight(env("UMAMI_URL", ""), "/"),
		UmamiWebsiteID:   env("UMAMI_WEBSITE_ID", ""),
		FCMProjectID:     strings.TrimSpace(env("FCM_PROJECT_ID", "")),
		RateLimitPerMin:  envInt("API_RATE_LIMIT_PER_MIN", 120),
		TrustedProxies:   splitCSV(env("TRUSTED_PROXIES", "")),

		LegalControllerName: strings.TrimSpace(env("LEGAL_CONTROLLER_NAME", "")),
		LegalContactEmail:   strings.TrimSpace(env("LEGAL_CONTACT_EMAIL", "")),
		LegalContactAddress: strings.TrimSpace(env("LEGAL_CONTACT_ADDRESS", "")),
		LegalHostingRegion:  strings.TrimSpace(env("LEGAL_HOSTING_REGION", "")),
	}
	c.SessionTTL = envDuration("SESSION_TTL", 720*time.Hour)

	if err := c.loadFCM(); err != nil {
		return nil, err
	}

	secret := env("SESSION_SECRET", "")
	if secret == "" {
		if c.Env == "production" {
			return nil, fmt.Errorf("SESSION_SECRET is required in production")
		}
		secret = "dev-only-insecure-session-secret-change-me"
	}
	c.SessionKey = []byte(secret)

	c.SMTPPass = env("SMTP_PASS", "")
	if path := env("SMTP_PASS_FILE", ""); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read SMTP_PASS_FILE: %w", err)
		}
		c.SMTPPass = strings.TrimSpace(string(b))
	}
	c.OIDCClientSecret = env("OIDC_CLIENT_SECRET", "")
	if path := env("OIDC_CLIENT_SECRET_FILE", ""); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read OIDC_CLIENT_SECRET_FILE: %w", err)
		}
		c.OIDCClientSecret = strings.TrimSpace(string(b))
	}
	c.OIDCScopes = splitCSV(env("OIDC_SCOPES", "openid,email,profile,groups"))
	c.AllowedEmails = splitCSV(env("ALLOWED_EMAILS", ""))
	c.AllowedDomains = splitCSV(env("ALLOWED_DOMAINS", ""))

	if c.OIDCEnabled {
		if c.OIDCIssuer == "" || c.OIDCClientID == "" || c.OIDCClientSecret == "" {
			return nil, fmt.Errorf("OIDC_ENABLED requires OIDC_ISSUER_URL, OIDC_CLIENT_ID and OIDC_CLIENT_SECRET")
		}
	}
	if c.BcryptCost < 10 || c.BcryptCost > 15 {
		c.BcryptCost = 12
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func splitCSV(v string) []string {
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

// loadFCM resolves FCM_SERVICE_ACCOUNT_JSON, which may contain raw JSON or a
// path to a service-account file, and derives the project id when it is not
// set explicitly. A missing, unreadable or malformed service account leaves FCM
// disabled without failing startup; a configured-but-invalid value is reported
// by SendFCM at delivery time.
func (c *Config) loadFCM() error {
	raw := strings.TrimSpace(env("FCM_SERVICE_ACCOUNT_JSON", ""))
	if raw == "" {
		return nil
	}
	c.FCMServiceAccountJSON = raw
	if !strings.HasPrefix(raw, "{") {
		b, err := os.ReadFile(raw)
		if err != nil {
			slog.Default().Warn("fcm service account could not be read; FCM disabled", "err", err)
			return nil
		}
		raw = strings.TrimSpace(string(b))
		c.FCMServiceAccountJSON = raw
	}
	var meta struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		slog.Default().Warn("fcm service account is not valid JSON; FCM disabled", "err", err)
		return nil
	}
	if c.FCMProjectID == "" {
		c.FCMProjectID = strings.TrimSpace(meta.ProjectID)
	}
	return nil
}

// EmailAllowed applies ALLOWED_EMAILS / ALLOWED_DOMAINS restrictions.
func (c *Config) EmailAllowed(email string) bool {
	if len(c.AllowedEmails) == 0 && len(c.AllowedDomains) == 0 {
		return true
	}
	email = strings.ToLower(email)
	for _, allowed := range c.AllowedEmails {
		if strings.ToLower(allowed) == email {
			return true
		}
	}
	domain := ""
	if i := strings.LastIndexByte(email, '@'); i >= 0 {
		domain = email[i+1:]
	}
	for _, allowed := range c.AllowedDomains {
		if strings.EqualFold(strings.TrimPrefix(allowed, "@"), domain) {
			return true
		}
	}
	return false
}
