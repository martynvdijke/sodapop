// Package account owns the operations that act on a user's whole account:
// exporting their data and closing it. Both the HTML app and the JSON API use
// this one service so their behaviour cannot drift.
package account

import (
	"context"
	stdSQL "database/sql"
	"errors"
	"strings"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"

	"sodapop/ent"
	"sodapop/ent/apitoken"
	"sodapop/ent/block"
	"sodapop/ent/follow"
	"sodapop/ent/friendship"
	"sodapop/ent/inboundtoken"
	"sodapop/ent/notification"
	"sodapop/ent/pushdevice"
	"sodapop/ent/session"
	"sodapop/ent/user"
	"sodapop/ent/useridentity"
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
	// SQL is the raw connection used for tables outside Ent (share_links) so
	// account closure can revoke them in the same transaction as Ent writes.
	SQL *stdSQL.DB
}

// New builds the shared account service. Integrations may be nil when only
// deletion is needed, but Export requires it. An optional database handle lets
// callers without an integrations service still revoke raw-only rows.
func New(client *ent.Client, ints *integrations.Service, database ...*stdSQL.DB) *Service {
	s := &Service{Client: client, Integrations: ints}
	if ints != nil {
		s.SQL = ints.SQL
	}
	if len(database) > 0 && database[0] != nil {
		s.SQL = database[0]
	}
	return s
}

// Delete soft-deletes an account and revokes every credential that could still
// act as the user: browser sessions, API tokens, inbound webhook tokens, push
// devices, and share links. The last active admin cannot be deleted. Every
// write happens in one transaction, so a failure leaves the account untouched.
func (s *Service) Delete(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return ErrInvalid
	}
	if s.SQL != nil {
		return s.deleteSQL(ctx, userID)
	}
	return s.deleteEnt(ctx, userID)
}

// lastAdminGuard makes the last-admin invariant part of the UPDATE itself. A
// separate count before the transaction would let two admins each observe the
// other and both delete, so the guard has to be evaluated by the write.
func lastAdminGuard(s *sql.Selector) {
	s.Where(sql.And(
		sql.EQ(user.FieldStatus, user.StatusActive),
		sql.IsNull(user.FieldDeletedAt),
		sql.Or(
			sql.NEQ(user.FieldRole, user.RoleAdmin),
			sql.ExprP(`(SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active' AND deleted_at IS NULL) > 1`),
		),
	))
}

// deleteSQL runs the whole closure on one database transaction, binding Ent to
// that transaction for its writes and using the same transaction for the
// raw-only share_links table.
func (s *Service) deleteSQL(ctx context.Context, userID string) error {
	now := db.Now()
	tx, err := s.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	client := ent.NewClient(ent.Driver(sql.NewDriver(dialect.SQLite, sql.Conn{ExecQuerier: tx})))
	n, err := client.User.Update().
		Where(user.IDEQ(userID), lastAdminGuard).
		SetStatus(user.StatusDeleted).
		SetDeletedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		row, err := client.User.Get(ctx, userID)
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
	if _, err := client.Session.Delete().Where(session.UserID(userID)).Exec(ctx); err != nil {
		return err
	}
	if _, err := client.APIToken.Update().
		Where(apitoken.UserID(userID), apitoken.RevokedAtIsNil()).
		SetRevokedAt(now).
		Save(ctx); err != nil {
		return err
	}
	if _, err := client.InboundToken.Update().
		Where(inboundtoken.UserID(userID), inboundtoken.RevokedAtIsNil()).
		SetRevokedAt(now).
		Save(ctx); err != nil {
		return err
	}
	if _, err := client.PushDevice.Delete().Where(pushdevice.UserID(userID)).Exec(ctx); err != nil {
		return err
	}
	if err := anonymise(ctx, client, userID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE share_links SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		now, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) deleteEnt(ctx context.Context, userID string) error {
	now := db.Now()
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
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
	if _, err := tx.PushDevice.Delete().Where(pushdevice.UserID(userID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := anonymise(ctx, tx.Client(), userID, now); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// anonymise strips every direct identifier from a closed account in the same
// transaction that closes it, so deleting means erasing rather than hiding. The
// user row itself has to survive because check-ins, comments and media point at
// it; what remains is an anonymous marker that no longer identifies anyone.
func anonymise(ctx context.Context, client *ent.Client, userID, now string) error {
	suffix := strings.TrimPrefix(userID, "-")
	suffix = strings.ReplaceAll(suffix, "-", "")
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	if _, err := client.User.Update().
		Where(user.IDEQ(userID)).
		SetEmail("deleted-" + suffix + "@deleted.invalid").
		SetHandle("deleted-" + suffix).
		SetDisplayName("Deleted user").
		SetBio("").
		ClearCountry().
		ClearAvatarMediaID().
		ClearPasswordHash().
		ClearEmailVerifiedAt().
		SetUpdatedAt(now).
		Save(ctx); err != nil {
		return err
	}
	// The external identity carries the IdP subject and often a second copy of
	// the address, so it goes with the account.
	if _, err := client.UserIdentity.Delete().Where(useridentity.UserID(userID)).Exec(ctx); err != nil {
		return err
	}
	if _, err := client.Follow.Delete().
		Where(follow.Or(follow.FollowerID(userID), follow.FolloweeID(userID))).Exec(ctx); err != nil {
		return err
	}
	if _, err := client.Friendship.Delete().
		Where(friendship.Or(friendship.UserA(userID), friendship.UserB(userID))).Exec(ctx); err != nil {
		return err
	}
	if _, err := client.Block.Delete().
		Where(block.Or(block.BlockerID(userID), block.BlockedID(userID))).Exec(ctx); err != nil {
		return err
	}
	if _, err := client.Notification.Delete().Where(notification.UserID(userID)).Exec(ctx); err != nil {
		return err
	}
	return nil
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
