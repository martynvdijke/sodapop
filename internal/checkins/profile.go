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
//
// From and To are RFC3339 UTC boundaries compared lexically against
// c.created_at. From is inclusive and To is exclusive, so a caller filtering a
// calendar day supplies that day's midnight as From and the next day's
// midnight as To. Sort accepts "newest", "oldest", "rating_high", "rating_low",
// or "likes"; any other value falls back to newest. Page and PageSize select
// offset-based paging. Before is honoured only on the default unfiltered newest
// cursor path: any filter, explicit sort, or Page > 1 switches the read to
// offset mode and ignores Before.
type ProfileCheckinsFilter struct {
	ViewerID  string
	TargetID  string
	Limit     int
	Before    string
	From      string
	To        string
	RatingMin *int
	RatingMax *int
	MinLikes  *int
	HasPhoto  bool
	// Visibility is an owner-only filter. It is applied only when ViewerID is
	// the target; for any other viewer it is ignored so the filter can never
	// widen the visibility set computed from the relationship.
	Visibility string
	Sort       string
	Page       int
	PageSize   int
}

// Profile paging modes reported by ProfileCheckinsPage.
const (
	ProfileModeCursor = "cursor"
	ProfileModeOffset = "offset"
)

// ProfileCheckinsResult is one profile page plus the paging metadata a handler
// needs. Items holds at most PageSize rows and HasNext reports whether another
// page follows. Filtered is true whenever offset paging was used.
type ProfileCheckinsResult struct {
	Items    []ProfileItem
	Page     int
	PageSize int
	HasNext  bool
	Filtered bool
	Mode     string
}

// Cursor encodes a keyset position for created_at-ordered lists. Stored
// timestamps have second precision, so the row id breaks ties on equal
// seconds and pagination cannot skip rows.
func Cursor(createdAt, id string) string { return createdAt + "|" + id }

func splitCursor(raw string) (createdAt, id string, ok bool) {
	createdAt, id, ok = strings.Cut(raw, "|")
	return createdAt, id, ok && createdAt != "" && id != ""
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
	VenueID     *int   `json:"venue_id,omitempty"`
	VenueName   string `json:"venue_name,omitempty"`
	Rating      *int   `json:"rating,omitempty"`
	Serving     string `json:"serving"`
	PrepMethod  string `json:"prep_method,omitempty"`
	PrepNote    string `json:"prep_note,omitempty"`
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

// profileVisibilitySet returns the list visibility values a visitor may read.
// The set is applied inside SQL, never after loading rows. It is deliberately
// NOT used for check-ins: private lists stay owner-only, while private
// check-ins are readable by accepted friends.
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

// profileCheckinVisibilitySet returns the check-in visibility values a visitor
// may read. Private check-ins are readable by accepted friends, so a friend
// sees every visibility value that the owner does.
func profileCheckinVisibilitySet(visit ProfileVisit) []string {
	switch visit {
	case ProfileOwner, ProfileFriend:
		return []string{"public", "friends", "private"}
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

// profileLikesExpr is the correlated toast counter. It is shared by the SELECT
// projection, the MinLikes filter, and the likes sort so all three agree.
const profileLikesExpr = `(SELECT COUNT(*) FROM checkin_likes l WHERE l.checkin_id = c.id)`

// profileSortClause maps a caller-supplied key to a whitelisted ORDER BY
// fragment. Unknown keys fall back to newest; the input is never interpolated.
func profileSortClause(sort string) string {
	switch sort {
	case "oldest":
		return "c.created_at ASC, c.id ASC"
	case "rating_high":
		return "c.rating IS NULL, c.rating DESC, c.created_at DESC, c.id DESC"
	case "rating_low":
		return "c.rating IS NULL, c.rating ASC, c.created_at DESC, c.id DESC"
	case "likes":
		return profileLikesExpr + " DESC, c.created_at DESC, c.id DESC"
	default:
		return "c.created_at DESC, c.id DESC"
	}
}

// ProfileCheckins returns one page of a profile's check-ins, already scoped to
// what viewerID may see. Deleted check-ins and deleted accounts are excluded in
// SQL. Non-visible rows are never loaded, so there is nothing to filter in Go.
func (s *Service) ProfileCheckins(ctx context.Context, f ProfileCheckinsFilter) ([]ProfileItem, error) {
	res, err := s.ProfileCheckinsPage(ctx, f)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

// ProfileCheckinsPage is ProfileCheckins with paging metadata. It uses the
// created_at cursor when no filter or explicit sort is requested, and offset
// paging (LIMIT/OFFSET) otherwise. At most PageSize rows are returned; HasNext
// is derived by fetching one extra row and trimming it.
func (s *Service) ProfileCheckinsPage(ctx context.Context, f ProfileCheckinsFilter) (*ProfileCheckinsResult, error) {
	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = f.Limit
	}
	if pageSize <= 0 || pageSize > 50 {
		pageSize = 20
	}
	page := f.Page
	if page < 1 {
		page = 1
	}

	from := strings.TrimSpace(f.From)
	to := strings.TrimSpace(f.To)
	visibility := strings.TrimSpace(f.Visibility)
	sortKey := strings.TrimSpace(f.Sort)
	filtered := sortKey != "" || from != "" || to != "" ||
		f.RatingMin != nil || f.RatingMax != nil || f.MinLikes != nil ||
		f.HasPhoto || visibility != "" || page > 1

	mode := ProfileModeCursor
	if filtered {
		mode = ProfileModeOffset
	}
	res := &ProfileCheckinsResult{
		Items:    []ProfileItem{},
		Page:     page,
		PageSize: pageSize,
		Filtered: filtered,
		Mode:     mode,
	}
	if f.TargetID == "" {
		return res, nil
	}
	visit := s.ProfileVisitFor(ctx, f.ViewerID, f.TargetID)
	vis := profileCheckinVisibilitySet(visit)

	q := `
		SELECT c.id, c.user_id, u.handle, COALESCE(u.display_name,''),
		       c.drink_id, d.name, d.slug, COALESCE(b.name,''), d.container, d.volume_ml,
		       c.rating, c.serving, COALESCE(c.prep_method,''), COALESCE(c.prep_note,''),
		       c.venue_id, COALESCE(c.note,''), COALESCE(v.name,''),
		       c.visibility, c.created_at,
		       COALESCE(c.photo_media_id,''), COALESCE(m.mime,''), COALESCE(m.poster_path,''),
		       ` + profileLikesExpr + `,
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

	if filtered {
		// Visibility is an owner-only refinement; the relationship-based set
		// above already scopes what another viewer may see.
		if visit == ProfileOwner && (visibility == "public" || visibility == "friends" || visibility == "private") {
			q += ` AND c.visibility = ?`
			args = append(args, visibility)
		}
		if from != "" {
			q += ` AND c.created_at >= ?`
			args = append(args, from)
		}
		if to != "" {
			q += ` AND c.created_at < ?`
			args = append(args, to)
		}
		if f.RatingMin != nil || f.RatingMax != nil {
			q += ` AND c.rating IS NOT NULL`
			if f.RatingMin != nil {
				q += ` AND c.rating >= ?`
				args = append(args, *f.RatingMin)
			}
			if f.RatingMax != nil {
				q += ` AND c.rating <= ?`
				args = append(args, *f.RatingMax)
			}
		}
		if f.MinLikes != nil {
			q += ` AND ` + profileLikesExpr + ` >= ?`
			args = append(args, *f.MinLikes)
		}
		if f.HasPhoto {
			q += ` AND c.photo_media_id IS NOT NULL AND c.photo_media_id <> ''`
		}
		q += ` ORDER BY ` + profileSortClause(sortKey) + ` LIMIT ? OFFSET ?`
		args = append(args, pageSize+1, (page-1)*pageSize)
	} else {
		if f.Before != "" {
			if createdAt, id, ok := splitCursor(f.Before); ok {
				q += ` AND (c.created_at < ? OR (c.created_at = ? AND c.id < ?))`
				args = append(args, createdAt, createdAt, id)
			} else {
				q += ` AND c.created_at < ?`
				args = append(args, f.Before)
			}
		}
		q += ` ORDER BY c.created_at DESC, c.id DESC LIMIT ?`
		args = append(args, pageSize+1)
	}

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
			venueID sql.NullInt64
			toasted int
		)
		if err := rows.Scan(&it.ID, &it.UserID, &it.Handle, &it.DisplayName,
			&it.DrinkID, &it.DrinkName, &it.DrinkSlug, &it.Brand, &it.Container, &it.VolumeML,
			&rating, &it.Serving, &it.PrepMethod, &it.PrepNote, &venueID, &it.Note, &it.VenueName,
			&it.Visibility, &it.CreatedAt,
			&it.PhotoID, &it.PhotoMime, &it.PosterPath,
			&it.Toasts, &toasted); err != nil {
			return nil, err
		}
		if rating.Valid {
			v := int(rating.Int64)
			it.Rating = &v
		}
		if venueID.Valid {
			v := int(venueID.Int64)
			it.VenueID = &v
		}
		it.Toasted = toasted != 0
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > pageSize {
		out = out[:pageSize]
		res.HasNext = true
	}
	res.Items = out
	return res, nil
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
	vis := profileCheckinVisibilitySet(visit)
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
