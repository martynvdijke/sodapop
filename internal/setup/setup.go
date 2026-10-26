// Package setup implements the one-time first admin bootstrap flow.
package setup

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"sodapop/ent"
	"sodapop/ent/appsetting"
	"sodapop/ent/user"
	"sodapop/internal/auth"
	"sodapop/internal/db"
	"sodapop/internal/ratelimit"
)

const (
	setupCompleteKey = "setup_complete"
	ipLimit          = 5
	globalLimit      = 20
	limitWindow      = 15 * time.Minute
)

var (
	// ErrSetupComplete means an admin already exists or setup was completed.
	ErrSetupComplete = errors.New("setup complete")
	// ErrInvalidToken means the submitted bootstrap token did not match.
	ErrInvalidToken = errors.New("invalid setup token")
	// ErrRateLimited means too many setup attempts were made.
	ErrRateLimited = errors.New("setup rate limited")
	// ErrInvalidInput means the submitted account fields failed validation.
	ErrInvalidInput = errors.New("invalid setup input")
	// ErrConflict means the handle or email is already in use. The message is
	// deliberately generic so setup cannot be used for enumeration.
	ErrConflict = errors.New("setup conflict")
)

// Service owns the one-time bootstrap token and first-admin creation.
type Service struct {
	client  *ent.Client
	auth    *auth.Service
	limiter *ratelimit.Limiter
	log     *slog.Logger

	mu       sync.Mutex
	tokenRaw string
	tokenSum [sha256.Size]byte
	hasToken bool
}

// New prepares the setup service. If no admin exists, it resolves a bootstrap
// token (SETUP_TOKEN or a random generated value) and logs the generated token
// exactly once. Only a SHA-256 digest is kept for comparison.
func New(client *ent.Client, authSvc *auth.Service, limiter *ratelimit.Limiter, log *slog.Logger, envToken string) *Service {
	if log == nil {
		log = slog.Default()
	}
	if limiter == nil {
		limiter = ratelimit.New()
	}
	s := &Service{client: client, auth: authSvc, limiter: limiter, log: log}
	if client == nil {
		return s
	}
	n, err := client.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(context.Background())
	if err != nil {
		log.Warn("setup state check failed", "err", err)
		return s
	}
	if n > 0 {
		return s
	}

	token := strings.TrimSpace(envToken)
	generated := token == ""
	if generated {
		token = auth.RandomToken(32)
	}
	s.setToken(token)
	if generated {
		// This is the only place the generated token is ever logged.
		log.Info("setup required: open /setup and use token " + token)
	}
	return s
}

func (s *Service) setToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" {
		s.tokenRaw = ""
		s.hasToken = false
		return
	}
	s.tokenRaw = token
	s.tokenSum = sha256.Sum256([]byte(token))
	s.hasToken = true
}

// SetupToken returns the bootstrap token once for boot logging or integration
// tests, then forgets the raw value. Handlers must never call or render it.
func (s *Service) SetupToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := s.tokenRaw
	s.tokenRaw = ""
	return token
}

// Required reports whether the one-time setup flow is still available.
func (s *Service) Required(ctx context.Context) (bool, error) {
	if s.client == nil {
		return false, nil
	}
	n, err := s.client.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	exists, err := s.client.AppSetting.Query().Where(appsetting.KeyEQ(setupCompleteKey)).Exist(ctx)
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// TokenValid compares token against the stored digest in constant time.
func (s *Service) TokenValid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	has := s.hasToken
	sum := s.tokenSum
	s.mu.Unlock()
	if !has {
		return false
	}
	candidate := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(candidate[:], sum[:]) == 1
}

// Claim creates the first admin. It is the convenience form of ClaimSession
// without request metadata.
func (s *Service) Claim(ctx context.Context, token, handle, email, password, displayName string) (*ent.User, error) {
	u, _, err := s.ClaimSession(ctx, token, handle, email, password, displayName, "", "")
	return u, err
}

// ClaimSession creates the first verified admin and a signed-in session in one
// transaction. ip/ua are optional request metadata.
func (s *Service) ClaimSession(ctx context.Context, token, handle, email, password, displayName, ip, ua string) (*ent.User, *auth.SessionResult, error) {
	ipKey := strings.TrimSpace(ip)
	if host, _, err := net.SplitHostPort(ipKey); err == nil {
		ipKey = host
	}
	if ipKey == "" {
		ipKey = "unknown"
	}
	if !s.limiter.Allow("setup:ip:"+ipKey, ipLimit, limitWindow) {
		return nil, nil, ErrRateLimited
	}
	if !s.limiter.Allow("setup:global", globalLimit, limitWindow) {
		return nil, nil, ErrRateLimited
	}
	if !s.TokenValid(token) {
		return nil, nil, ErrInvalidToken
	}

	handle = strings.ToLower(strings.TrimSpace(handle))
	email = strings.ToLower(strings.TrimSpace(email))
	displayName = strings.TrimSpace(displayName)
	if err := auth.ValidateRegistration(handle, email, password); err != nil {
		return nil, nil, ErrInvalidInput
	}
	if displayName == "" {
		displayName = handle
	}

	hash, err := s.auth.HashPassword(password)
	if err != nil {
		return nil, nil, err
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		if isConflict(err) {
			return nil, nil, ErrSetupComplete
		}
		return nil, nil, err
	}
	rollback := func(err error) (*ent.User, *auth.SessionResult, error) {
		_ = tx.Rollback()
		return nil, nil, err
	}

	// Re-check completion inside the transaction. The unique setup_complete row
	// is inserted before the user so concurrent claims cannot both proceed.
	if exists, err := tx.AppSetting.Query().Where(appsetting.KeyEQ(setupCompleteKey)).Exist(ctx); err != nil {
		return rollback(err)
	} else if exists {
		return rollback(ErrSetupComplete)
	}
	n, err := tx.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(ctx)
	if err != nil {
		return rollback(err)
	}
	if n > 0 {
		return rollback(ErrSetupComplete)
	}

	if _, err := tx.AppSetting.Create().
		SetKey(setupCompleteKey).
		SetValue("1").
		SetUpdatedAt(db.Now()).
		Save(ctx); err != nil {
		if isConflict(err) {
			return rollback(ErrSetupComplete)
		}
		return rollback(err)
	}

	// One more admin check after taking the setup gate.
	if n, err := tx.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(ctx); err != nil {
		return rollback(err)
	} else if n > 0 {
		return rollback(ErrSetupComplete)
	}

	u, err := s.auth.CreateVerifiedAdmin(ctx, tx.Client(), auth.AdminAccount{
		Handle:       handle,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: hash,
	})
	if err != nil {
		if isConflict(err) {
			return rollback(ErrConflict)
		}
		return rollback(err)
	}
	if _, err := tx.UserSetting.Create().SetUserID(u.ID).Save(ctx); err != nil {
		return rollback(err)
	}
	res, err := s.auth.CreateSessionWithClient(ctx, tx.Client(), u.ID, ip, ua)
	if err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, ErrSetupComplete
	}

	// Setup cannot be replayed; drop the raw token as soon as it has been used.
	s.mu.Lock()
	s.tokenRaw = ""
	s.mu.Unlock()
	return u, res, nil
}

func isConflict(err error) bool {
	if err == nil {
		return false
	}
	if ent.IsConstraintError(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "constraint failed") ||
		strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "deadlocked") ||
		strings.Contains(msg, "sqlite_busy")
}
