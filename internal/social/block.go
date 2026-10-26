package social

import (
	"context"
	"strings"

	"sodapop/ent"
	"sodapop/ent/block"
	"sodapop/ent/follow"
	"sodapop/ent/friendship"
	"sodapop/ent/user"
	"sodapop/internal/db"
)

// Note on search: the product has no user-returning search endpoint. The only
// search surfaces are catalog (drinks) and venue search, which contain no user
// rows, so blocking needs no search filter. If a user search is ever added, it
// must exclude both BlockedUserIDs and users who blocked the viewer.

// BlockedUser is a template/API-friendly row for a user the viewer blocked.
type BlockedUser struct {
	ID          string
	Handle      string
	DisplayName string
	CreatedAt   string
}

// Block records that blockerID no longer wants contact with the account behind
// handle. It is idempotent: blocking the same account twice is a no-op. An
// unknown handle is a silent no-op so the action cannot enumerate accounts.
// Blocking yourself returns ErrSelfAction.
//
// Blocking ends any accepted or pending friendship and every follow in either
// direction in a single transaction, so follower/following counts cannot
// include a blocked account. Unblocking does not restore those rows. Other
// content (check-ins, comments, toasts, notifications) is deliberately never
// deleted: the feed, comment and notification read paths filter blocked pairs
// instead.
func (s *Service) Block(ctx context.Context, blockerID, handle string) error {
	target, err := s.activeUserByHandle(ctx, handle)
	if err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	if target.ID == blockerID {
		return ErrSelfAction
	}

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	exists, err := tx.Block.Query().
		Where(block.BlockerID(blockerID), block.BlockedID(target.ID)).
		Exist(ctx)
	if err != nil {
		return rollback(err)
	}
	if !exists {
		if _, err := tx.Block.Create().
			SetBlockerID(blockerID).
			SetBlockedID(target.ID).
			SetCreatedAt(db.Now()).
			Save(ctx); err != nil && !ent.IsConstraintError(err) {
			return rollback(err)
		}
	}

	a, b := order(blockerID, target.ID)
	if _, err := tx.Friendship.Delete().
		Where(friendship.UserA(a), friendship.UserB(b)).
		Exec(ctx); err != nil {
		return rollback(err)
	}

	// Follows are symmetric for enforcement, so remove both directions.
	if _, err := tx.Follow.Delete().
		Where(follow.Or(
			follow.And(follow.FollowerID(blockerID), follow.FolloweeID(target.ID)),
			follow.And(follow.FollowerID(target.ID), follow.FolloweeID(blockerID)),
		)).
		Exec(ctx); err != nil {
		return rollback(err)
	}

	return tx.Commit()
}

// Unblock removes only the blocker's own block row. Unknown handles and
// already-absent rows are silent no-ops.
func (s *Service) Unblock(ctx context.Context, blockerID, handle string) error {
	target, err := s.activeUserByHandle(ctx, handle)
	if err != nil {
		return err
	}
	if target == nil || target.ID == blockerID {
		return nil
	}
	_, err = s.Client.Block.Delete().
		Where(block.BlockerID(blockerID), block.BlockedID(target.ID)).
		Exec(ctx)
	return err
}

// AreBlocked reports whether either user has blocked the other. Blocks are
// symmetric for enforcement: one side blocking hides both directions. A user
// is never considered blocked from themselves.
func (s *Service) AreBlocked(ctx context.Context, a, b string) (bool, error) {
	if a == "" || b == "" || a == b {
		return false, nil
	}
	n, err := s.Client.Block.Query().
		Where(block.Or(
			block.And(block.BlockerID(a), block.BlockedID(b)),
			block.And(block.BlockerID(b), block.BlockedID(a)),
		)).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// BlockedUserIDs returns every account in a block relationship with userID,
// regardless of direction. It backs SQL/Ent filters (for example notification
// hiding) without an N+1 per row.
func (s *Service) BlockedUserIDs(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return []string{}, nil
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT CASE WHEN blocker_id = ? THEN blocked_id ELSE blocker_id END
		FROM blocks
		WHERE blocker_id = ? OR blocked_id = ?`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// BlockedUsers lists the accounts userID has blocked, newest first. It is for
// the settings page and never includes users only blocked by the other side.
func (s *Service) BlockedUsers(ctx context.Context, userID string) ([]BlockedUser, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT u.id, u.handle, COALESCE(u.display_name,''), b.created_at
		FROM blocks b
		JOIN users u ON u.id = b.blocked_id
		WHERE b.blocker_id = ?
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		ORDER BY b.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlockedUser{}
	for rows.Next() {
		var b BlockedUser
		if err := rows.Scan(&b.ID, &b.Handle, &b.DisplayName, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// activeUserByHandle resolves a handle to an active account. A missing or
// soft-deleted account returns (nil, nil) so callers can stay anti-enumerating.
func (s *Service) activeUserByHandle(ctx context.Context, handle string) (*ent.User, error) {
	handle = strings.ToLower(strings.TrimSpace(handle))
	if handle == "" {
		return nil, nil
	}
	u, err := s.Client.User.Query().
		Where(user.HandleEqualFold(handle), user.DeletedAtIsNil(), user.StatusNEQ(user.StatusDeleted)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}
