package checkins

import (
	"context"
	"database/sql"
	"strings"

	"sodapop/ent/userbadge"
)

// ProfileVisit classifies how a viewer relates to the profile they are reading.
type ProfileVisit int

const (
	// ProfileAnonymous is a viewer with no session.
	ProfileAnonymous ProfileVisit = iota
	// ProfileStranger is a signed-in viewer who is not the owner or a friend.
	ProfileStranger
	// ProfileFriend is an accepted friend of the owner.
	ProfileFriend
	// ProfileOwner is the owner reading their own profile.
	ProfileOwner
)

// ProfileAccess is the decision for whether a profile page may render. The web
// layer maps ProfileHidden to a 404 (never a 403) and ProfileNeedsLogin to a
// redirect, so private profiles cannot be enumerated.
type ProfileAccess int

const (
	// ProfileVisible means the page may render.
	ProfileVisible ProfileAccess = iota
	// ProfileNeedsLogin means an anonymous viewer must sign in first.
	ProfileNeedsLogin
	// ProfileHidden means the profile is indistinguishable from a missing one.
	ProfileHidden
)

// DecideProfileAccess is the pure access rule for GET /u/{handle}. Callers pass
// the owner's public_profile setting and whether the account is gone. The order
// mirrors the product rules: gone is always hidden, public is always visible,
// anonymous viewers are asked to sign in, and a signed-in non-friend is refused
// exactly like a missing profile.
func DecideProfileAccess(public, userDeleted bool, viewerID, targetID string, areFriends bool) ProfileAccess {
	if userDeleted {
		return ProfileHidden
	}
	if public {
		return ProfileVisible
	}
	if viewerID == "" {
		return ProfileNeedsLogin
	}
	if viewerID == targetID || areFriends {
		return ProfileVisible
	}
	return ProfileHidden
}

// ProfileCheckinsFilter scopes a profile feed read. ViewerID is empty for
// anonymous requests; Before is the created_at cursor from the previous page.
type ProfileCheckinsFilter struct {
	ViewerID string
	TargetID string
	Limit    int
	Before   string
}

// ProfileItem is one check-in on a public profile. It carries the full feed
// contract (author, drink, venue, photo, toast state) because a profile page
// shows other people's check-ins, unlike Item which is personal/friend scoped.
type ProfileItem struct {
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
	PosterPath  string `json:"poster_path,omitempty"`
	Toasts      int    `json:"toasts"`
	Toasted     bool   `json:"toasted"`
	CSRF        string `json:"-"`
}

// ProfileStats summarises what one viewer is allowed to see of a profile.
type ProfileStats struct {
	Pours        int
	Styles       int
	Venues       int
	Badges       int
	UniqueDrinks int
	Toasts       int
}

// ProfileList is a list visible on a profile page.
type ProfileList struct {
	ID          string
	Name        string
	Slug        string
	Description string
	Visibility  string
	Items       int
}

// ProfileVisitFor resolves the viewer/target relationship once. It treats an
// empty viewer as anonymous and never asks Social about a sessionless viewer.
func (s *Service) ProfileVisitFor(ctx context.Context, viewerID, targetID string) ProfileVisit {
	switch {
	case targetID == "" || viewerID == "":
		return ProfileAnonymous
	case viewerID == targetID:
		return ProfileOwner
	case s.Social != nil && s.Social.AreFriends(ctx, viewerID, targetID):
		return ProfileFriend
	default:
		return ProfileStranger
	}
}

// profileVisibilitySet returns the check-in/list visibility values a visitor
// may read. The set is applied inside SQL, never after loading rows.
func profileVisibilitySet(visit ProfileVisit) []string {
	switch visit {
	case ProfileOwner:
		return []string{"public", "friends", "private"}
	case ProfileFriend:
		return []string{"public", "friends"}
	default:
		return []string{"public"}
	}
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func visibilityArgs(values []string) []any {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return args
}

// ProfileCheckins returns one page of a profile's check-ins, already scoped to
// what viewerID may see. Deleted check-ins and deleted accounts are excluded in
// SQL. Non-visible rows are never loaded, so there is nothing to filter in Go.
func (s *Service) ProfileCheckins(ctx context.Context, f ProfileCheckinsFilter) ([]ProfileItem, error) {
	limit := f.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if f.TargetID == "" {
		return []ProfileItem{}, nil
	}
	visit := s.ProfileVisitFor(ctx, f.ViewerID, f.TargetID)
	vis := profileVisibilitySet(visit)

	q := `
		SELECT c.id, c.user_id, u.handle, COALESCE(u.display_name,''),
		       c.drink_id, d.name, d.slug, COALESCE(b.name,''), d.container, d.volume_ml,
		       c.rating, COALESCE(c.note,''), COALESCE(v.name,''),
		       c.visibility, c.created_at,
		       COALESCE(c.photo_media_id,''), COALESCE(m.mime,''), COALESCE(m.poster_path,''),
		       (SELECT COUNT(*) FROM checkin_likes l WHERE l.checkin_id = c.id),
		       EXISTS(SELECT 1 FROM checkin_likes l2 WHERE l2.checkin_id = c.id AND l2.user_id = ?)
		FROM checkins c
		JOIN users u ON u.id = c.user_id
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN venues v ON v.id = c.venue_id
		LEFT JOIN media m ON m.id = c.photo_media_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		  AND c.visibility IN (` + placeholders(len(vis)) + `)`
	args := []any{f.ViewerID, f.TargetID}
	args = append(args, visibilityArgs(vis)...)
	if f.Before != "" {
		q += ` AND c.created_at < ?`
		args = append(args, f.Before)
	}
	q += ` ORDER BY c.created_at DESC, c.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProfileItem{}
	for rows.Next() {
		var (
			it      ProfileItem
			rating  sql.NullInt64
			toasted int
		)
		if err := rows.Scan(&it.ID, &it.UserID, &it.Handle, &it.DisplayName,
			&it.DrinkID, &it.DrinkName, &it.DrinkSlug, &it.Brand, &it.Container, &it.VolumeML,
			&rating, &it.Note, &it.VenueName,
			&it.Visibility, &it.CreatedAt,
			&it.PhotoID, &it.PhotoMime, &it.PosterPath,
			&it.Toasts, &toasted); err != nil {
			return nil, err
		}
		if rating.Valid {
			v := int(rating.Int64)
			it.Rating = &v
		}
		it.Toasted = toasted != 0
		out = append(out, it)
	}
	return out, rows.Err()
}

// ProfileStats counts what viewerID may see on targetID's profile. Styles are
// distinct drink styles and Venues are distinct non-null venue ids across the
// visible check-ins, so both stay zero-safe for an empty history.
func (s *Service) ProfileStats(ctx context.Context, viewerID, targetID string) (*ProfileStats, error) {
	out := &ProfileStats{}
	if targetID == "" {
		return out, nil
	}
	visit := s.ProfileVisitFor(ctx, viewerID, targetID)
	vis := profileVisibilitySet(visit)
	q := `
		SELECT COUNT(*), COUNT(DISTINCT d.style_id), COUNT(DISTINCT c.venue_id),
		       COUNT(DISTINCT c.drink_id)
		FROM checkins c
		JOIN users u ON u.id = c.user_id
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		  AND c.visibility IN (` + placeholders(len(vis)) + `)`
	args := []any{targetID}
	args = append(args, visibilityArgs(vis)...)
	if err := s.SQL.QueryRowContext(ctx, q, args...).
		Scan(&out.Pours, &out.Styles, &out.Venues, &out.UniqueDrinks); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	// Toasts received are counted only on check-ins this viewer may see, so the
	// public counter can never reveal activity on private rows.
	toastQuery := `
		SELECT COUNT(*)
		FROM checkin_likes l
		JOIN checkins c ON c.id = l.checkin_id
		JOIN users u ON u.id = c.user_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		  AND c.visibility IN (` + placeholders(len(vis)) + `)`
	if err := s.SQL.QueryRowContext(ctx, toastQuery, args...).Scan(&out.Toasts); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	out.Badges, _ = s.Client.UserBadge.Query().Where(userbadge.UserID(targetID)).Count(ctx)
	return out, nil
}

// ProfileLists returns the lists on a profile that viewerID may see. System
// lists (the wishlist) are personal plumbing and never appear on a profile.
func (s *Service) ProfileLists(ctx context.Context, viewerID, targetID string) ([]ProfileList, error) {
	if targetID == "" {
		return []ProfileList{}, nil
	}
	visit := s.ProfileVisitFor(ctx, viewerID, targetID)
	vis := profileVisibilitySet(visit)
	q := `
		SELECT l.id, l.name, COALESCE(l.slug,''), COALESCE(l.description,''), l.visibility,
		       (SELECT COUNT(*) FROM list_items li WHERE li.list_id = l.id)
		FROM lists l
		JOIN users u ON u.id = l.user_id
		WHERE l.user_id = ? AND l.is_system = 0
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		  AND l.visibility IN (` + placeholders(len(vis)) + `)
		ORDER BY l.created_at DESC, l.name ASC`
	args := []any{targetID}
	args = append(args, visibilityArgs(vis)...)
	rows, err := s.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProfileList{}
	for rows.Next() {
		var l ProfileList
		if err := rows.Scan(&l.ID, &l.Name, &l.Slug, &l.Description, &l.Visibility, &l.Items); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
