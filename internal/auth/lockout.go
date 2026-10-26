package auth

import (
	"context"
	"strings"
	"time"

	"sodapop/ent/loginattempt"
	"sodapop/internal/db"
)

const (
	// LoginFailureLimit is the number of failed password attempts that locks a
	// key for LoginFailureWindow.
	LoginFailureLimit = 10
	// LoginFailureWindow is the rolling window used for the durable lockout.
	LoginFailureWindow = 15 * time.Minute

	loginAttemptRetention  = 24 * time.Hour
	loginAttemptPruneLimit = 500
)

// LoginKey derives the durable attempt key for an email/handle. The identifier
// is normalised and hashed so the ledger never stores a plaintext address.
func LoginKey(identifier string) string {
	normalized := strings.ToLower(strings.TrimSpace(identifier))
	return "email:" + HashToken(normalized)
}

// LoginLocked reports whether identifier has reached the durable failure
// threshold. A database error fails open: the in-memory limiter remains the
// first line of defence and an unavailable ledger must not lock everyone out.
func (s *Service) LoginLocked(ctx context.Context, identifier string) bool {
	if strings.TrimSpace(identifier) == "" {
		return false
	}
	cutoff := time.Now().UTC().Add(-LoginFailureWindow).Format(time.RFC3339)
	n, err := s.Client.LoginAttempt.Query().
		Where(loginattempt.Key(LoginKey(identifier)), loginattempt.AttemptedAtGTE(cutoff)).
		Count(ctx)
	if err != nil {
		return false
	}
	return n >= LoginFailureLimit
}

// RecordLoginFailure appends one failed password attempt and opportunistically
// prunes rows older than 24 hours in a bounded batch.
func (s *Service) RecordLoginFailure(ctx context.Context, identifier string) error {
	if strings.TrimSpace(identifier) == "" {
		return nil
	}
	if _, err := s.Client.LoginAttempt.Create().
		SetKey(LoginKey(identifier)).
		SetAttemptedAt(db.Now()).
		Save(ctx); err != nil {
		return err
	}
	return s.pruneLoginAttempts(ctx)
}

// ClearLoginFailures drops every attempt for identifier after a successful
// password check.
func (s *Service) ClearLoginFailures(ctx context.Context, identifier string) error {
	if strings.TrimSpace(identifier) == "" {
		return nil
	}
	_, err := s.Client.LoginAttempt.Delete().
		Where(loginattempt.Key(LoginKey(identifier))).
		Exec(ctx)
	return err
}

// pruneLoginAttempts deletes at most loginAttemptPruneLimit expired rows per
// failure so an attack cannot turn the cleanup path into an unbounded write.
func (s *Service) pruneLoginAttempts(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-loginAttemptRetention).Format(time.RFC3339)
	_, err := s.SQL.ExecContext(ctx, `
		DELETE FROM login_attempts
		WHERE id IN (
			SELECT id FROM login_attempts
			WHERE attempted_at < ?
			ORDER BY attempted_at
			LIMIT ?
		)`, cutoff, loginAttemptPruneLimit)
	return err
}
