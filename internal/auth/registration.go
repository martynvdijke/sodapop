package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/list"
	"sodapop/ent/user"
	"sodapop/internal/db"
)

const (
	// verificationTokenTTL is how long an email verification link stays valid.
	verificationTokenTTL = 24 * time.Hour
	// registrationAttempts is the initial try plus one retry for SQLite write
	// contention. See the race note on RegisterLocal.
	registrationAttempts = 2
)

var (
	// ErrRegistrationConflict means the handle or email is already in use. It
	// is deliberately generic so callers cannot use registration for account
	// enumeration.
	ErrRegistrationConflict = errors.New("registration conflict")

	// errRegistrationRetry marks a transient SQLite lock/busy failure that is
	// safe to retry in a fresh transaction.
	errRegistrationRetry = errors.New("registration retry")
)

// RegisterInput is a already-validated local account. The caller is
// responsible for normalising the handle/email and hashing the password.
type RegisterInput struct {
	Handle       string
	Email        string
	DisplayName  string
	PasswordHash string
}

// RegisterResult is the committed account plus the raw email verification
// token. Only the hash is stored; the caller decides whether to mail the raw
// token (it should not for an already-verified bootstrap owner).
type RegisterResult struct {
	User              *ent.User
	VerificationToken string
}

// RegisterLocal creates a local account, its settings, system wishlist and an
// email verification token in a single transaction. The first account in the
// database is bootstrapped as an active, verified admin so a fresh install
// always has an owner.
//
// SQLite race note: `client.Tx` starts a deferred transaction. Two concurrent
// registrations can both read count == 0 before either writes. The first to
// upgrade its read lock to a write lock commits; the loser receives
// SQLITE_BUSY/SQLITE_BUSY_SNAPSHOT on its write or commit. We retry the whole
// transaction once, at which point the winner's row is visible and count > 0.
// This is why count and create must stay inside the same transaction.
func (s *Service) RegisterLocal(ctx context.Context, client *ent.Client, in RegisterInput) (*RegisterResult, error) {
	if client == nil {
		client = s.Client
	}
	if client == nil {
		return nil, errors.New("auth: registration requires a database client")
	}

	var lastErr error
	for attempt := 0; attempt < registrationAttempts; attempt++ {
		res, err := s.registerLocalTx(ctx, client, in)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !errors.Is(err, errRegistrationRetry) {
			return nil, err
		}
	}

	// Retrying did not clear the lock. Report a generic conflict rather than
	// leaking driver details.
	if errors.Is(lastErr, errRegistrationRetry) {
		return nil, ErrRegistrationConflict
	}
	return nil, lastErr
}

// registerLocalTx performs one fully transactional registration attempt.
func (s *Service) registerLocalTx(ctx context.Context, client *ent.Client, in RegisterInput) (*RegisterResult, error) {
	tx, err := client.Tx(ctx)
	if err != nil {
		if sqliteBusy(err) {
			return nil, fmt.Errorf("%w: %v", errRegistrationRetry, err)
		}
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	total, err := tx.User.Query().Count(ctx)
	if err != nil {
		if sqliteBusy(err) {
			return nil, fmt.Errorf("%w: %v", errRegistrationRetry, err)
		}
		return nil, err
	}

	create := tx.User.Create().
		SetHandle(in.Handle).
		SetEmail(in.Email).
		SetDisplayName(in.DisplayName).
		SetPasswordHash(in.PasswordHash)
	if total == 0 {
		// Bootstrap owner: active and verified so they can write immediately.
		create = create.
			SetRole(user.RoleAdmin).
			SetStatus(user.StatusActive).
			SetEmailVerifiedAt(db.Now())
	}
	u, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrRegistrationConflict
		}
		if sqliteBusy(err) {
			return nil, fmt.Errorf("%w: %v", errRegistrationRetry, err)
		}
		return nil, err
	}

	if _, err := tx.UserSetting.Create().SetUserID(u.ID).Save(ctx); err != nil {
		return nil, registrationOpErr(err)
	}
	if _, err := tx.List.Create().
		SetUserID(u.ID).
		SetName("Wishlist").
		SetSlug("wishlist").
		SetVisibility(list.VisibilityPrivate).
		SetIsSystem(true).
		Save(ctx); err != nil {
		return nil, registrationOpErr(err)
	}

	raw := RandomToken(32)
	if _, err := tx.EmailVerificationToken.Create().
		SetTokenHash(HashToken(raw)).
		SetUserID(u.ID).
		SetEmail(in.Email).
		SetExpiresAt(time.Now().UTC().Add(verificationTokenTTL).Format(time.RFC3339)).
		Save(ctx); err != nil {
		return nil, registrationOpErr(err)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		if sqliteBusy(err) {
			return nil, fmt.Errorf("%w: %v", errRegistrationRetry, err)
		}
		return nil, err
	}
	committed = true

	return &RegisterResult{User: u, VerificationToken: raw}, nil
}

// registrationOpErr classifies failures after the user row is created. Unique
// violations here are not useful details for the caller, so they collapse to a
// generic conflict.
func registrationOpErr(err error) error {
	if sqliteBusy(err) {
		return fmt.Errorf("%w: %v", errRegistrationRetry, err)
	}
	if ent.IsConstraintError(err) {
		return ErrRegistrationConflict
	}
	return err
}

// sqliteBusy reports whether err is transient SQLite write contention.
func sqliteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "deadlocked")
}
