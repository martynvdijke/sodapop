package social

import (
	"context"
	"database/sql"
	"strings"
)

const (
	// DefaultFollowPageSize is the page size used when the caller does not ask.
	DefaultFollowPageSize = 20
	// MaxFollowPageSize caps one page so a list can never become a directory dump.
	MaxFollowPageSize = 50
)

// FollowUser is one row in a follower or following list. It is deliberately
// narrow: the list pages need a name, a handle, an avatar and three booleans,
// not a full profile.
type FollowUser struct {
	ID               string
	Handle           string
	DisplayName      string
	AvatarMediaID    *string
	Bio              string
	FollowedByViewer bool
	IsFriend         bool
}

// FollowPage is one keyset page of a follower/following list. NextCursor is
// empty when HasMore is false.
type FollowPage struct {
	Users      []FollowUser
	NextCursor string
	HasMore    bool
}

// FollowListOptions scopes one page read. ViewerID fills the viewer-relative
// flags; an empty viewer leaves them false, which is what anonymous callers see.
type FollowListOptions struct {
	ViewerID string
	Cursor   string
	Limit    int
}

// FollowCursor encodes the keyset position of a follow row: the time it was
// created plus the counterpart user id that breaks ties on equal timestamps.
func FollowCursor(createdAt, userID string) string {
	return createdAt + "|" + userID
}

// splitFollowCursor parses a cursor produced by FollowCursor.
func splitFollowCursor(cursor string) (createdAt, userID string, ok bool) {
	createdAt, userID, ok = strings.Cut(strings.TrimSpace(cursor), "|")
	if !ok || createdAt == "" || userID == "" {
		return "", "", false
	}
	return createdAt, userID, true
}

// Followers lists the members who follow userID, newest follow first.
func (s *Service) Followers(ctx context.Context, userID string, opts FollowListOptions) (*FollowPage, error) {
	return s.followList(ctx, userID, opts, true)
}

// Following lists the members userID follows, newest follow first.
func (s *Service) Following(ctx context.Context, userID string, opts FollowListOptions) (*FollowPage, error) {
	return s.followList(ctx, userID, opts, false)
}

// followList reads one keyset page in either direction. The two directions share
// every rule (deleted accounts hidden, per-row blocks hidden, viewer flags) and
// differ only in which side of the follow row is the counterpart.
//
// A block in either direction between the viewer and a listed member hides that
// member, matching how a profile read treats a blocked pair. The list itself is
// only reachable after the caller has applied the profile-visibility rule.
func (s *Service) followList(ctx context.Context, userID string, opts FollowListOptions, followers bool) (*FollowPage, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultFollowPageSize
	}
	if limit > MaxFollowPageSize {
		limit = MaxFollowPageSize
	}
	viewer := strings.TrimSpace(opts.ViewerID)

	counterpart, owner := "f.follower_id", "f.followee_id"
	if !followers {
		counterpart, owner = "f.followee_id", "f.follower_id"
	}

	var (
		where strings.Builder
		args  []any
	)
	// Viewer flags: does the viewer follow this member, and are they friends?
	where.WriteString(`
		SELECT u.id, u.handle, COALESCE(u.display_name, ''), u.avatar_media_id,
		       COALESCE(u.bio, ''), f.created_at,
		       EXISTS(SELECT 1 FROM follows vf
		              WHERE vf.follower_id = ? AND vf.followee_id = u.id) AS followed_by_viewer,
		       EXISTS(SELECT 1 FROM friendships fr
		              WHERE fr.status = 'accepted'
		                AND ((fr.user_a = ? AND fr.user_b = u.id)
		                  OR (fr.user_a = u.id AND fr.user_b = ?))) AS is_friend
		FROM follows f
		JOIN users u ON u.id = ` + counterpart + `
		WHERE ` + owner + ` = ?
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		  AND NOT EXISTS (
		      SELECT 1 FROM blocks bl
		      WHERE (bl.blocker_id = ? AND bl.blocked_id = u.id)
		         OR (bl.blocker_id = u.id AND bl.blocked_id = ?)
		  )`)
	args = append(args, viewer, viewer, viewer, userID, viewer, viewer)

	if strings.TrimSpace(opts.Cursor) != "" {
		createdAt, cursorID, ok := splitFollowCursor(opts.Cursor)
		if !ok {
			return nil, ErrInvalidCursor
		}
		where.WriteString(` AND (f.created_at < ? OR (f.created_at = ? AND ` + counterpart + ` < ?))`)
		args = append(args, createdAt, createdAt, cursorID)
	}
	where.WriteString(` ORDER BY f.created_at DESC, ` + counterpart + ` DESC LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.SQL.QueryContext(ctx, where.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FollowUser{}
	cursors := []string{}
	for rows.Next() {
		var (
			u        FollowUser
			avatar   sql.NullString
			created  string
			followed bool
			friend   bool
		)
		if err := rows.Scan(&u.ID, &u.Handle, &u.DisplayName, &avatar, &u.Bio, &created, &followed, &friend); err != nil {
			return nil, err
		}
		if avatar.Valid && avatar.String != "" {
			id := avatar.String
			u.AvatarMediaID = &id
		}
		u.FollowedByViewer = followed
		u.IsFriend = friend
		out = append(out, u)
		cursors = append(cursors, FollowCursor(created, u.ID))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	page := &FollowPage{Users: out}
	if len(out) > limit {
		page.Users = out[:limit]
		page.HasMore = true
		page.NextCursor = cursors[limit-1]
	}
	return page, nil
}
