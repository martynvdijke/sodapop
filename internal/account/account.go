// Package account owns the operations that act on a user's whole account:
// exporting their data and closing it. Both the HTML app and the JSON API use
// this one service so their behaviour cannot drift.
package account

import (
	"context"
	"errors"
	"strings"

	"entgo.io/ent/dialect/sql"

	"sodapop/ent"
	"sodapop/ent/apitoken"
	"sodapop/ent/inboundtoken"
	"sodapop/ent/session"
	"sodapop/ent/user"
	"sodapop/internal/db"
	"sodapop/internal/integrations"
)

var (
	// ErrLastAdmin means the account is the last active admin and closing it
	// would leave the instance without anyone able to administer it.
	ErrLastAdmin = errors.New("the last active admin cannot be deleted")
	// ErrNotFound means there is no account with the requested ID.
	ErrNotFound = errors.New("account not found")
	// ErrInvalid means the request was malformed before it reached the store.
	ErrInvalid = errors.New("invalid account request")
)

// Service performs whole-account operations.
type Service struct {
	Client       *ent.Client
	Integrations *integrations.Service
}

// New builds the shared account service. Integrations may be nil when only
// deletion is needed, but Export requires it.
func New(client *ent.Client, ints *integrations.Service) *Service {
	return &Service{Client: client, Integrations: ints}
}

// Delete soft-deletes an account and revokes every credential that could still
// act as the user: browser sessions, API tokens, and inbound webhook tokens.
// The last active admin cannot be deleted. Every write happens in one
// transaction, so a failure leaves the account untouched.
func (s *Service) Delete(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return ErrInvalid
	}
	now := db.Now()
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	// The last-admin invariant is part of the UPDATE itself. A separate count
	// before the transaction would let two admins each observe the other and
	// both delete, so the guard has to be evaluated by the write statement.
	lastAdminGuard := func(s *sql.Selector) {
		s.Where(sql.And(
			sql.EQ(user.FieldStatus, user.StatusActive),
			sql.IsNull(user.FieldDeletedAt),
			sql.Or(
				sql.NEQ(user.FieldRole, user.RoleAdmin),
				sql.ExprP(`(SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active' AND deleted_at IS NULL) > 1`),
			),
		))
	}
	n, err := tx.User.Update().
		Where(user.IDEQ(userID), lastAdminGuard).
		SetStatus(user.StatusDeleted).
		SetDeletedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if n == 0 {
		// Nothing was written. Work out whether the account is gone or whether
		// the guard refused the write, then roll back so credentials survive.
		row, err := tx.User.Get(ctx, userID)
		_ = tx.Rollback()
		if err != nil {
			if ent.IsNotFound(err) {
				return ErrNotFound
			}
			return err
		}
		if row.Status != user.StatusActive || row.DeletedAt != nil {
			return ErrNotFound
		}
		if row.Role == user.RoleAdmin {
			return ErrLastAdmin
		}
		return ErrNotFound
	}
	if _, err := tx.Session.Delete().Where(session.UserID(userID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.APIToken.Update().
		Where(apitoken.UserID(userID), apitoken.RevokedAtIsNil()).
		SetRevokedAt(now).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.InboundToken.Update().
		Where(inboundtoken.UserID(userID), inboundtoken.RevokedAtIsNil()).
		SetRevokedAt(now).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Export returns the user's own data as JSON. The serializer lives in the
// integrations package and is deliberately shared with the HTML export route.
func (s *Service) Export(ctx context.Context, userID string) ([]byte, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrInvalid
	}
	if s.Integrations == nil {
		return nil, errors.New("account export is not configured")
	}
	raw, err := s.Integrations.ExportJSON(ctx, userID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return raw, nil
}
