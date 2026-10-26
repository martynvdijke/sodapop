package social

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/activity"
	"sodapop/ent/checkinlike"
	"sodapop/ent/follow"
	"sodapop/ent/friendship"
	"sodapop/ent/notification"
	"sodapop/ent/user"
	"sodapop/internal/db"
)

var (
	ErrSelfAction = errors.New("cannot perform that action on yourself")
	ErrNotFriend  = errors.New("not friends")
	ErrNotFound   = errors.New("not found")
	ErrNotAllowed = errors.New("not allowed")
)

// Service owns the friend graph, follows and the friends feed.
type Service struct {
	Client *ent.Client
	SQL    *sql.DB
}

func New(database *db.DB) *Service {
	return &Service{Client: database.Client, SQL: database.SQL}
}

// AreFriends reports whether two users have an accepted friendship.
func (s *Service) AreFriends(ctx context.Context, a, b string) bool {
	if a == b {
		return true
	}
	x, y := order(a, b)
	n, err := s.Client.Friendship.Query().
		Where(friendship.UserA(x), friendship.UserB(y), friendship.StatusEQ(friendship.StatusAccepted)).
		Count(ctx)
	return err == nil && n > 0
}

// FriendIDs returns accepted friend IDs.
func (s *Service) FriendIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT CASE WHEN user_a = ? THEN user_b ELSE user_a END AS friend_id
		FROM friendships
		WHERE status = 'accepted' AND (user_a = ? OR user_b = ?)`, userID, userID, userID)
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

// ListFriends returns accepted friends with basic profile fields.
func (s *Service) ListFriends(ctx context.Context, userID string) ([]*ent.User, error) {
	ids, err := s.FriendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []*ent.User{}, nil
	}
	return s.Client.User.Query().
		Where(user.IDIn(ids...), user.DeletedAtIsNil()).
		Order(ent.Asc(user.FieldHandle)).
		All(ctx)
}

// FriendUser is a friend plus relationship metadata.
type FriendUser struct {
	ID          string `json:"id"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	Since       string `json:"since"`
}

// FriendsDetailed is used by the HTML friends page.
type FriendsDetailed struct {
	Friends  []FriendUser
	Incoming []PendingRequest
	Outgoing []PendingRequest
}

type PendingRequest struct {
	ID          string `json:"id"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

// Friends returns accepted friends and pending requests for a user.
func (s *Service) Friends(ctx context.Context, userID string) (*FriendsDetailed, error) {
	out := &FriendsDetailed{Friends: []FriendUser{}, Incoming: []PendingRequest{}, Outgoing: []PendingRequest{}}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT u.id, u.handle, u.display_name, f.updated_at
		FROM friendships f
		JOIN users u ON u.id = CASE WHEN f.user_a = ? THEN f.user_b ELSE f.user_a END
		WHERE f.status = 'accepted' AND (f.user_a = ? OR f.user_b = ?)
		ORDER BY u.handle`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f FriendUser
		if err := rows.Scan(&f.ID, &f.Handle, &f.DisplayName, &f.Since); err != nil {
			rows.Close()
			return nil, err
		}
		out.Friends = append(out.Friends, f)
	}
	rows.Close()

	prows, err := s.SQL.QueryContext(ctx, `
		SELECT f.id, u.handle, u.display_name, f.created_at, f.requested_by
		FROM friendships f
		JOIN users u ON u.id = CASE WHEN f.requested_by = ? THEN (CASE WHEN f.user_a = ? THEN f.user_b ELSE f.user_a END) ELSE f.requested_by END
		WHERE f.status = 'pending' AND (f.user_a = ? OR f.user_b = ?)
		ORDER BY f.created_at DESC`, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var p PendingRequest
		var requestedBy string
		if err := prows.Scan(&p.ID, &p.Handle, &p.DisplayName, &p.CreatedAt, &requestedBy); err != nil {
			return nil, err
		}
		if requestedBy == userID {
			out.Outgoing = append(out.Outgoing, p)
		} else {
			out.Incoming = append(out.Incoming, p)
		}
	}
	return out, prows.Err()
}

// Request creates a pending friendship. Unknown handles return a nil row and
// no error, so the API cannot be used to enumerate accounts.
func (s *Service) Request(ctx context.Context, fromID, handle string) (*ent.Friendship, error) {
	target, err := s.Client.User.Query().
		Where(user.HandleEqualFold(strings.ToLower(strings.TrimSpace(handle))), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if target.ID == fromID {
		return nil, ErrSelfAction
	}
	a, b := order(fromID, target.ID)
	existing, err := s.Client.Friendship.Query().
		Where(friendship.UserA(a), friendship.UserB(b)).
		Only(ctx)
	if err == nil {
		if existing.Status == friendship.StatusAccepted {
			return existing, nil
		}
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	f, err := s.Client.Friendship.Create().
		SetUserA(a).SetUserB(b).
		SetStatus(friendship.StatusPending).
		SetRequestedBy(fromID).Save(ctx)
	if err != nil {
		return nil, err
	}
	// Best effort: a failed notification must not fail the friend request.
	_ = s.Notify(ctx, target.ID, "friend_request", fromID, strconv.Itoa(f.ID), "wants to be friends")
	return f, nil
}

// Accept accepts a pending request addressed to userID.
func (s *Service) Accept(ctx context.Context, userID, friendshipID string) error {
	id, err := strconv.Atoi(friendshipID)
	if err != nil {
		return ErrNotFound
	}
	f, err := s.Client.Friendship.Get(ctx, id)
	if err != nil {
		return ErrNotFound
	}
	if f.UserA != userID && f.UserB != userID {
		return ErrNotAllowed
	}
	if f.RequestedBy == userID {
		return ErrNotAllowed
	}
	if f.Status != friendship.StatusPending {
		return nil
	}
	_, err = f.Update().SetStatus(friendship.StatusAccepted).SetUpdatedAt(db.Now()).Save(ctx)
	if err != nil {
		return err
	}
	// Notify the original requester; failures are never surfaced to the accepter.
	requester := f.RequestedBy
	if requester == "" {
		if f.UserA == userID {
			requester = f.UserB
		} else {
			requester = f.UserA
		}
	}
	if requester != "" && requester != userID {
		_ = s.Notify(ctx, requester, "friend_accept", userID, strconv.Itoa(f.ID), "accepted your friend request")
	}
	return nil
}

// Remove deletes an accepted or pending friendship.
func (s *Service) Remove(ctx context.Context, userID, friendshipID string) error {
	id, err := strconv.Atoi(friendshipID)
	if err != nil {
		return ErrNotFound
	}
	f, err := s.Client.Friendship.Get(ctx, id)
	if err != nil {
		return ErrNotFound
	}
	if f.UserA != userID && f.UserB != userID {
		return ErrNotAllowed
	}
	return s.Client.Friendship.DeleteOne(f).Exec(ctx)
}

// FolloweeIDs returns the users userID follows.
func (s *Service) FolloweeIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT followee_id FROM follows
		WHERE follower_id = ?
		ORDER BY followee_id`, userID)
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

// IncomingPendingRequests maps requester ID to friendship ID for pending
// requests addressed to userID. Outgoing and accepted requests are excluded.
func (s *Service) IncomingPendingRequests(ctx context.Context, userID string) (map[string]string, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT requested_by, id FROM friendships
		WHERE status = 'pending'
		  AND (user_a = ? OR user_b = ?)
		  AND requested_by <> ?`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var requester, id string
		if err := rows.Scan(&requester, &id); err != nil {
			return nil, err
		}
		out[requester] = id
	}
	return out, rows.Err()
}

// Follow creates a one-way follow.
func (s *Service) Follow(ctx context.Context, followerID, handle string) error {
	target, err := s.Client.User.Query().Where(user.HandleEqualFold(strings.ToLower(handle))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if target.ID == followerID {
		return ErrSelfAction
	}
	exists, err := s.Client.Follow.Query().Where(follow.FollowerID(followerID), follow.FolloweeID(target.ID)).Exist(ctx)
	if err != nil || exists {
		return err
	}
	_, err = s.Client.Follow.Create().SetFollowerID(followerID).SetFolloweeID(target.ID).Save(ctx)
	return err
}

// Unfollow removes a one-way follow.
func (s *Service) Unfollow(ctx context.Context, followerID, handle string) error {
	target, err := s.Client.User.Query().Where(user.HandleEqualFold(strings.ToLower(handle))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	_, err = s.Client.Follow.Delete().
		Where(follow.FollowerID(followerID), follow.FolloweeID(target.ID)).Exec(ctx)
	return err
}

// FeedItem is one check-in in the friends feed.
type FeedItem struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	DrinkID     int    `json:"drink_id"`
	DrinkName   string `json:"drink_name"`
	DrinkSlug   string `json:"drink_slug"`
	Brand       string `json:"brand,omitempty"`
	Container   string `json:"container"`
	VolumeML    int    `json:"volume_ml"`
	VenueName   string `json:"venue_name,omitempty"`
	Rating      *int   `json:"rating,omitempty"`
	Note        string `json:"note,omitempty"`
	Visibility  string `json:"visibility"`
	CreatedAt   string `json:"created_at"`
	PhotoID     string `json:"photo_id,omitempty"`
	PhotoMime   string `json:"photo_mime,omitempty"`
	PhotoKind   string `json:"photo_kind,omitempty"`
	PosterPath  string `json:"poster_path,omitempty"`
	Duration    int    `json:"duration_seconds,omitempty"`
	Toasts      int    `json:"toasts"`
	Toasted     bool   `json:"toasted"`
	Comments    int    `json:"comments"`
	CSRF        string `json:"-"`
}

// Feed returns check-ins from the user, accepted friends and followed users,
// newest first. A followed user who is not a friend only contributes public
// check-ins. There is no public/global feed endpoint by design.
func (s *Service) Feed(ctx context.Context, userID string, limit int, before string) ([]FeedItem, error) {
	return s.feed(ctx, userID, limit, before, false)
}

// FeedFollowing returns the viewer's own check-ins plus those of users they
// follow. It backs the explicit "following" tab on the home feed.
func (s *Service) FeedFollowing(ctx context.Context, userID string, limit int, before string) ([]FeedItem, error) {
	return s.feed(ctx, userID, limit, before, true)
}

func (s *Service) feed(ctx context.Context, userID string, limit int, before string, followingOnly bool) ([]FeedItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	// Relationship scope is expressed with SQL subqueries rather than
	// interpolated IN lists so a very large follow or friend set cannot exceed
	// SQLite's bound-variable limit.
	const friendOfViewer = `EXISTS (
		SELECT 1 FROM friendships f
		WHERE f.status = 'accepted'
		  AND (
			(f.user_a = ? AND f.user_b = c.user_id)
			OR (f.user_b = ? AND f.user_a = c.user_id)
		  )
	)`
	args := []any{userID}
	query := `
		SELECT c.id, c.user_id, u.handle, u.display_name,
		       d.id, d.name, d.slug, COALESCE(b.name,''), d.container, d.volume_ml,
		       COALESCE(v.name,''), c.rating, c.note, c.visibility, c.created_at,
		       COALESCE(c.photo_media_id,''), COALESCE(m.mime,''), COALESCE(m.kind,''),
		       COALESCE(m.poster_path,''), COALESCE(m.duration_seconds,0),
		       (SELECT COUNT(*) FROM checkin_likes l WHERE l.checkin_id = c.id),
		       EXISTS(SELECT 1 FROM checkin_likes l2 WHERE l2.checkin_id = c.id AND l2.user_id = ?),
		       (SELECT COUNT(*) FROM comments cm
		        JOIN users cu ON cu.id = cm.user_id
		        WHERE cm.checkin_id = c.id AND cm.deleted_at IS NULL
		          AND cu.deleted_at IS NULL AND cu.status <> 'deleted')
		FROM checkins c
		JOIN users u ON u.id = c.user_id
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN venues v ON v.id = c.venue_id
		LEFT JOIN media m ON m.id = c.photo_media_id
		WHERE c.deleted_at IS NULL
		  AND (
			c.user_id = ?
			OR (
				c.user_id IN (SELECT followee_id FROM follows WHERE follower_id = ?)
				AND (
					c.visibility = 'public'
					OR (c.visibility = 'friends' AND ` + friendOfViewer + `)
				)
			)`
	args = append(args, userID, userID, userID, userID)
	if !followingOnly {
		// The main feed also includes accepted friends who are not followed.
		query += `
			OR (
				c.visibility IN ('public', 'friends')
				AND ` + friendOfViewer + `
			)`
		args = append(args, userID, userID)
	}
	query += `
		  )`
	if before != "" {
		query += ` AND c.created_at < ?`
		args = append(args, before)
	}
	query += ` ORDER BY c.created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedItem{}
	for rows.Next() {
		var f FeedItem
		var rating sql.NullInt64
		if err := rows.Scan(&f.ID, &f.UserID, &f.Handle, &f.DisplayName,
			&f.DrinkID, &f.DrinkName, &f.DrinkSlug, &f.Brand, &f.Container, &f.VolumeML,
			&f.VenueName, &rating, &f.Note, &f.Visibility, &f.CreatedAt,
			&f.PhotoID, &f.PhotoMime, &f.PhotoKind, &f.PosterPath, &f.Duration,
			&f.Toasts, &f.Toasted, &f.Comments); err != nil {
			return nil, err
		}
		if rating.Valid {
			v := int(rating.Int64)
			f.Rating = &v
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Toast adds a like. Self-toasting is ignored. The check-in must be visible to
// the toaster and its author must still be an active account.
func (s *Service) Toast(ctx context.Context, userID, checkinID string) error {
	ownerID, err := s.visibleCheckinOwner(ctx, userID, checkinID)
	if err != nil {
		return err
	}
	exists, err := s.Client.CheckinLike.Query().
		Where(checkinlike.CheckinID(checkinID), checkinlike.UserID(userID)).Exist(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.Client.CheckinLike.Create().SetCheckinID(checkinID).SetUserID(userID).Save(ctx)
	if err != nil {
		return err
	}
	if ownerID != userID {
		_, _ = s.Client.Notification.Create().
			SetUserID(ownerID).
			SetKind(notification.KindToast).
			SetActorID(userID).
			SetSubjectID(checkinID).
			SetText("toasted your check-in").
			Save(ctx)
	}
	return nil
}

// Untoast removes a like. The check-in must be visible to the user; unlike
// Toast, an already-absent like is not an error, so the action is idempotent.
func (s *Service) Untoast(ctx context.Context, userID, checkinID string) error {
	ownerID, err := s.visibleCheckinOwner(ctx, userID, checkinID)
	if err != nil {
		return err
	}
	if _, err := s.Client.CheckinLike.Delete().
		Where(checkinlike.CheckinID(checkinID), checkinlike.UserID(userID)).Exec(ctx); err != nil {
		return err
	}
	// Best effort, idempotent: removing an absent notification is a no-op.
	_, _ = s.Client.Notification.Delete().
		Where(
			notification.UserID(ownerID),
			notification.KindEQ(notification.KindToast),
			notification.ActorID(userID),
			notification.SubjectID(checkinID),
		).Exec(ctx)
	return nil
}

// visibleCheckinOwner returns the author of a check-in the viewer may interact
// with, or ErrNotFound when it is private to a stranger or the author is gone.
func (s *Service) visibleCheckinOwner(ctx context.Context, userID, checkinID string) (string, error) {
	var ownerID string
	err := s.SQL.QueryRowContext(ctx, `
		SELECT c.user_id
		FROM checkins c
		JOIN users u ON u.id = c.user_id
		WHERE c.id = ? AND c.deleted_at IS NULL
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		  AND (
			c.visibility = 'public'
			OR c.user_id = ?
			OR (
				c.visibility = 'friends'
				AND ? <> ''
				AND EXISTS (
					SELECT 1 FROM friendships f
					WHERE f.status = 'accepted'
					  AND (
						(f.user_a = ? AND f.user_b = c.user_id)
						OR (f.user_b = ? AND f.user_a = c.user_id)
					  )
				)
			)
		  )`, checkinID, userID, userID, userID, userID).Scan(&ownerID)
	if err != nil {
		return "", ErrNotFound
	}
	return ownerID, nil
}

// Notifications returns unread-first notifications.
func (s *Service) Notifications(ctx context.Context, userID string, limit int) ([]*ent.Notification, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.Client.Notification.Query().
		Where(notification.UserID(userID)).
		Order(ent.Desc(notification.FieldCreatedAt)).
		Limit(limit).All(ctx)
}

// Notify records a notification for userID. actorID/subjectID may be empty.
func (s *Service) Notify(ctx context.Context, userID, kind, actorID, subjectID, text string) error {
	k := notification.Kind(kind)
	if notification.KindValidator(k) != nil {
		k = notification.KindSystem
	}
	b := s.Client.Notification.Create().
		SetUserID(userID).
		SetKind(k).
		SetSubjectID(subjectID).
		SetText(text)
	if actorID != "" {
		b = b.SetActorID(actorID)
	}
	_, err := b.Save(ctx)
	return err
}

// UnreadCount returns the number of unread notifications for userID.
func (s *Service) UnreadCount(ctx context.Context, userID string) (int, error) {
	return s.Client.Notification.Query().
		Where(notification.UserID(userID), notification.ReadAtIsNil()).
		Count(ctx)
}

// MarkRead marks one notification (owned by userID) as read; empty id marks all.
func (s *Service) MarkRead(ctx context.Context, userID, id string) error {
	q := s.Client.Notification.Update().
		Where(notification.UserID(userID), notification.ReadAtIsNil())
	if id != "" {
		q = q.Where(notification.ID(id))
	}
	_, err := q.SetReadAt(time.Now().UTC().Format(time.RFC3339)).Save(ctx)
	return err
}

// LogActivity writes a feed activity row inside an existing transaction.
func LogActivity(ctx context.Context, tx *ent.Tx, userID, kind, subjectID, visibility string) error {
	k := activity.KindCheckin
	switch kind {
	case "badge":
		k = activity.KindBadge
	case "list":
		k = activity.KindList
	case "venue":
		k = activity.KindVenue
	case "friend":
		k = activity.KindFriend
	}
	v := activity.VisibilityPublic
	switch visibility {
	case "friends":
		v = activity.VisibilityFriends
	case "private":
		v = activity.VisibilityPrivate
	}
	_, err := tx.Activity.Create().
		SetUserID(userID).SetKind(k).SetSubjectID(subjectID).SetVisibility(v).Save(ctx)
	return err
}

func order(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}
