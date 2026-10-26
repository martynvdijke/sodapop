package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sodapop/ent"
	"sodapop/ent/apitoken"
	"sodapop/ent/session"
	"sodapop/ent/user"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

const (
	SessionCookie = "sd_session"
	CSRFHeader    = "X-CSRF-Token"
	CSRFForm      = "csrf_token"
	TokenPrefix   = "sd_"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailUnverified    = errors.New("email not verified")
	ErrAccountDisabled    = errors.New("account disabled")
)

// Service owns sessions, local passwords and personal API tokens.
type Service struct {
	Cfg    *config.Config
	DB     *db.DB
	Client *ent.Client
	SQL    *sql.DB
	OIDC   *OIDC
}

func New(cfg *config.Config, database *db.DB) *Service {
	return &Service{Cfg: cfg, DB: database, Client: database.Client, SQL: database.SQL}
}

// HashPassword hashes a plaintext password with the configured bcrypt cost.
func (s *Service) HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), s.Cfg.BcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword compares a plaintext password against a stored hash.
func CheckPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// RandomToken returns a URL-safe random string with n bytes of entropy.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken returns the SHA-256 hex digest stored at rest.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SessionResult is returned when a new browser session is created.
type SessionResult struct {
	Token     string
	CSRF      string
	ExpiresAt time.Time
}

// CreateSession stores a hashed session token and returns the raw cookie value.
func (s *Service) CreateSession(ctx context.Context, userID, ip, ua string) (*SessionResult, error) {
	return s.CreateSessionWithClient(ctx, s.Client, userID, ip, ua)
}

// CreateSessionWithClient creates a session through the supplied client, which
// may be a transaction client. It keeps cookie/TTL behaviour identical to
// CreateSession.
func (s *Service) CreateSessionWithClient(ctx context.Context, client *ent.Client, userID, ip, ua string) (*SessionResult, error) {
	if client == nil {
		client = s.Client
	}
	token := RandomToken(32)
	csrf := RandomToken(32)
	expires := time.Now().UTC().Add(s.Cfg.SessionTTL)
	_, err := client.Session.Create().
		SetTokenHash(HashToken(token)).
		SetUserID(userID).
		SetCsrfToken(csrf).
		SetNillableIP(optString(ip)).
		SetNillableUserAgent(optString(truncate(ua, 300))).
		SetExpiresAt(expires.Format(time.RFC3339)).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return &SessionResult{Token: token, CSRF: csrf, ExpiresAt: expires}, nil
}

// SetSessionCookie writes the session cookie using the project-wide flags.
func (s *Service) SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie expires the browser session cookie.
func (s *Service) ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// CurrentSession resolves the session cookie into a live session row and user.
func (s *Service) CurrentSession(ctx context.Context, r *http.Request) (*ent.Session, *ent.User, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil, sql.ErrNoRows
	}
	sess, err := s.Client.Session.Query().
		Where(session.TokenHash(HashToken(c.Value))).
		Only(ctx)
	if err != nil {
		return nil, nil, err
	}
	expires, err := time.Parse(time.RFC3339, sess.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		_ = s.Client.Session.DeleteOne(sess).Exec(ctx)
		return nil, nil, sql.ErrNoRows
	}
	u, err := s.Client.User.Query().
		Where(user.ID(sess.UserID), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, nil, err
	}
	if u.Status != "active" {
		return nil, nil, ErrAccountDisabled
	}
	// Avoid a write on every request.
	if last, err := time.Parse(time.RFC3339, sess.LastSeenAt); err == nil && time.Since(last) > 5*time.Minute {
		_, _ = s.Client.Session.UpdateOne(sess).SetLastSeenAt(db.Now()).Save(ctx)
	}
	return sess, u, nil
}

// DestroySession deletes the current session row.
func (s *Service) DestroySession(ctx context.Context, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
		_, _ = s.Client.Session.Delete().Where(session.TokenHash(HashToken(c.Value))).Exec(ctx)
	}
}

// ValidCSRF compares the submitted token with the session's CSRF token.
func ValidCSRF(r *http.Request, sess *ent.Session) bool {
	if sess == nil {
		return false
	}
	got := r.Header.Get(CSRFHeader)
	if got == "" {
		got = r.FormValue(CSRFForm)
	}
	return got != "" && subtleEqual(got, sess.CsrfToken)
}

// APIToken bundles a token row with its user and scopes.
type APIToken struct {
	Row    *ent.APIToken
	User   *ent.User
	Scopes map[string]bool
}

// HasScope reports whether the token grants scope ("read", "write", "feed").
func (t *APIToken) HasScope(scope string) bool {
	return t != nil && (t.Scopes[scope] || t.Scopes["admin"])
}

// AuthenticateToken validates a Bearer personal token.
func (s *Service) AuthenticateToken(ctx context.Context, r *http.Request) (*APIToken, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, sql.ErrNoRows
	}
	raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if !strings.HasPrefix(raw, TokenPrefix) {
		return nil, sql.ErrNoRows
	}
	row, err := s.Client.APIToken.Query().
		Where(
			apitoken.TokenHash(HashToken(raw)),
			apitoken.RevokedAtIsNil(),
		).
		WithUser().
		Only(ctx)
	if err != nil {
		return nil, err
	}
	if row.Edges.User == nil || row.Edges.User.Status != "active" {
		return nil, ErrAccountDisabled
	}
	scopes := map[string]bool{}
	for _, sc := range strings.Split(row.Scopes, ",") {
		if sc = strings.TrimSpace(sc); sc != "" {
			scopes[sc] = true
		}
	}
	_, _ = s.Client.APIToken.UpdateOneID(row.ID).SetLastUsedAt(db.Now()).Save(ctx)
	return &APIToken{Row: row, User: row.Edges.User, Scopes: scopes}, nil
}

// CreateAPIToken mints a new personal token and returns the plaintext once.
func (s *Service) CreateAPIToken(ctx context.Context, userID, name string, scopes []string) (string, *ent.APIToken, error) {
	token := TokenPrefix + RandomToken(32)
	row, err := s.Client.APIToken.Create().
		SetUserID(userID).
		SetName(name).
		SetTokenHash(HashToken(token)).
		SetScopes(strings.Join(scopes, ",")).
		Save(ctx)
	if err != nil {
		return "", nil, err
	}
	return token, row, nil
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func optString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
