package social

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"

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
	return s.Client.Friendship.Create().
		SetUserA(a).SetUserB(b).
		SetStatus(friendship.StatusPending).
		SetRequestedBy(fromID).Save(ctx)
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
	return err
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
	Toasts      int    `json:"toasts"`
	Toasted     bool   `json:"toasted"`
	CSRF        string `json:"-"`
}

// Feed returns check-ins from the user and accepted friends, newest first.
// There is no public/global feed endpoint by design.
func (s *Service) Feed(ctx context.Context, userID string, limit int, before string) ([]FeedItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	ids, err := s.FriendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids = append(ids, userID)
	sort.Strings(ids)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{userID}
	for _, id := range ids {
		args = append(args, id)
	}
	query := `
		SELECT c.id, c.user_id, u.handle, u.display_name,
		       d.id, d.name, d.slug, COALESCE(b.name,''), d.container, d.volume_ml,
		       COALESCE(v.name,''), c.rating, c.note, c.visibility, c.created_at,
		       (SELECT COUNT(*) FROM checkin_likes l WHERE l.checkin_id = c.id),
		       EXISTS(SELECT 1 FROM checkin_likes l2 WHERE l2.checkin_id = c.id AND l2.user_id = ?)
		FROM checkins c
		JOIN users u ON u.id = c.user_id
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN venues v ON v.id = c.venue_id
		WHERE c.deleted_at IS NULL
		  AND c.user_id IN (` + placeholders + `)
		  AND (c.user_id = ? OR c.visibility IN ('public','friends'))`
	args = append(args, userID)
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
			&f.VenueName, &rating, &f.Note, &f.Visibility, &f.CreatedAt, &f.Toasts, &f.Toasted); err != nil {
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

// Toast adds a like. Self-toasting is ignored.
func (s *Service) Toast(ctx context.Context, userID, checkinID string) error {
	var ownerID string
	if err := s.SQL.QueryRowContext(ctx, `SELECT user_id FROM checkins WHERE id = ? AND deleted_at IS NULL`, checkinID).Scan(&ownerID); err != nil {
		return ErrNotFound
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

// Untoast removes a like.
func (s *Service) Untoast(ctx context.Context, userID, checkinID string) error {
	_, err := s.Client.CheckinLike.Delete().
		Where(checkinlike.CheckinID(checkinID), checkinlike.UserID(userID)).Exec(ctx)
	return err
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
