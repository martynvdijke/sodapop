package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/apitoken"
	"sodapop/ent/inboundtoken"
	"sodapop/ent/passwordresettoken"
	"sodapop/ent/session"
	"sodapop/ent/user"
	"sodapop/internal/db"
	"sodapop/internal/email"
)

// resetTTL is how long an emailed password reset link stays valid.
const resetTTL = time.Hour

// ErrInvalidResetToken is returned for missing, used, or expired reset links.
var ErrInvalidResetToken = errors.New("invalid or expired reset token")

// IssuePasswordReset creates a single-use reset token and queues the email for
// active local accounts. Unknown addresses and OIDC-only accounts are silently
// ignored so the endpoint never reveals whether an account exists.
func (s *Service) IssuePasswordReset(ctx context.Context, emailAddr string) error {
	emailAddr = strings.ToLower(strings.TrimSpace(emailAddr))
	if emailAddr == "" {
		return nil
	}
	u, err := s.Client.User.Query().
		Where(
			user.Email(emailAddr),
			user.DeletedAtIsNil(),
			user.StatusEQ(user.StatusActive),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !HasPassword(u) {
		return nil
	}

	raw := RandomToken(32)
	now := time.Now().UTC()
	// Retire outstanding links before minting a new one. Failures here are not
	// fatal: the new token is still valid.
	_, _ = s.Client.PasswordResetToken.Update().
		Where(passwordresettoken.UserID(u.ID), passwordresettoken.UsedAtIsNil()).
		SetUsedAt(now.Format(time.RFC3339)).
		Save(ctx)

	if _, err := s.Client.PasswordResetToken.Create().
		SetTokenHash(HashToken(raw)).
		SetUserID(u.ID).
		SetCreatedAt(now.Format(time.RFC3339)).
		SetExpiresAt(now.Add(resetTTL).Format(time.RFC3339)).
		Save(ctx); err != nil {
		return err
	}

	// Reuse the outbox mailer so the message format stays in one place. The
	// long-running Mailer drains the same queue.
	mailer := email.New(s.Cfg, s.DB, slog.Default())
	if err := mailer.PasswordReset(ctx, u.Email, u.Handle, raw); err != nil {
		// The token is useless without the mail, so drop it rather than leave a
		// dangling row the user cannot reach.
		_, _ = s.Client.PasswordResetToken.Delete().
			Where(passwordresettoken.TokenHash(HashToken(raw))).
			Exec(ctx)
		return err
	}
	return nil
}

// PasswordResetValid reports whether the token is unused and unexpired.
func (s *Service) PasswordResetValid(ctx context.Context, token string) bool {
	row, err := s.resetToken(ctx, token)
	if err != nil {
		return false
	}
	if row.UsedAt != nil {
		return false
	}
	expires, err := time.Parse(time.RFC3339, row.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		return false
	}
	u, err := s.Client.User.Get(ctx, row.UserID)
	if err != nil || u.DeletedAt != nil || u.Status != user.StatusActive {
		return false
	}
	return true
}

// ConsumePasswordReset sets a new password with a valid token, marks the token
// used, and revokes every session. The token is single-use.
func (s *Service) ConsumePasswordReset(ctx context.Context, token, next string) error {
	if strings.TrimSpace(token) == "" {
		return ErrInvalidResetToken
	}
	if len([]rune(next)) < minPassword {
		return ErrWeakPassword
	}
	row, err := s.resetToken(ctx, token)
	if err != nil {
		return err
	}
	if row.UsedAt != nil {
		return ErrInvalidResetToken
	}
	expires, err := time.Parse(time.RFC3339, row.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		return ErrInvalidResetToken
	}
	u, err := s.Client.User.Get(ctx, row.UserID)
	if err != nil || u.DeletedAt != nil || u.Status != user.StatusActive {
		return ErrInvalidResetToken
	}
	hash, err := s.HashPassword(next)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	// Conditional update keeps the token single-use under concurrent posts.
	n, err := tx.PasswordResetToken.Update().
		Where(passwordresettoken.ID(row.ID), passwordresettoken.UsedAtIsNil()).
		SetUsedAt(now.Format(time.RFC3339)).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if n == 0 {
		_ = tx.Rollback()
		return ErrInvalidResetToken
	}
	if err := tx.User.UpdateOneID(u.ID).
		SetPasswordHash(hash).
		SetUpdatedAt(db.Now()).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Session.Delete().Where(session.UserID(u.ID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	// A reset proves control of the mailbox, not of existing tokens: retire
	// every programmatic credential so a stolen one cannot outlive the reset.
	revokedAt := now.Format(time.RFC3339)
	if _, err := tx.APIToken.Update().
		Where(apitoken.UserID(u.ID), apitoken.RevokedAtIsNil()).
		SetRevokedAt(revokedAt).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.InboundToken.Update().
		Where(inboundtoken.UserID(u.ID), inboundtoken.RevokedAtIsNil()).
		SetRevokedAt(revokedAt).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Service) resetToken(ctx context.Context, token string) (*ent.PasswordResetToken, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrInvalidResetToken
	}
	row, err := s.Client.PasswordResetToken.Query().
		Where(passwordresettoken.TokenHash(HashToken(token))).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrInvalidResetToken
		}
		return nil, err
	}
	return row, nil
}
