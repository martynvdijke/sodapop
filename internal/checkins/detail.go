package checkins

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Detail is the full read model for a single check-in. Visibility rules are
// applied by Get: callers must treat a refused row exactly like a missing one.
type Detail struct {
	ID            string
	UserID        string
	UserHandle    string
	UserName      string
	UserInitial   string
	DrinkID       int
	DrinkSlug     string
	DrinkName     string
	Brand         string
	Style         string
	Rating        *int
	Note          string
	CreatedAt     string
	Visibility    string
	VenueID       *int
	VenueName     string
	MediaID       string
	MediaMime     string
	Toasts        int
	Toasted       bool
	CommentsCount int
}

// Initial exposes the avatar initial under the name the detail template uses.
func (d *Detail) Initial() string {
	if d == nil || d.UserInitial == "" {
		return "?"
	}
	return d.UserInitial
}

// Get loads one check-in, scoped to what viewerID is allowed to see. An empty
// viewerID is anonymous. Non-visible rows return ErrNotFound, never a 403, so
// the endpoint cannot be used to probe for private check-ins.
func (s *Service) Get(ctx context.Context, viewerID, id string) (*Detail, error) {
	d, err := s.load(ctx, viewerID, id)
	if err != nil {
		return nil, err
	}

	// A block in either direction hides the row from an authenticated viewer
	// before visibility is even considered. Owners always see their own rows.
	if viewerID != "" && viewerID != d.UserID && s.blocked(ctx, viewerID, d.UserID) {
		return nil, ErrNotFound
	}

	if !s.visible(ctx, viewerID, d) {
		return nil, ErrNotFound
	}
	return d, nil
}

// GetForModeration loads one check-in for a moderator or admin acting on a
// report. It deliberately skips the block-only and visibility denials so a
// moderation queue can still show a reported row after the reporter or owner
// blocks the other party. It still rejects soft-deleted check-ins and rows
// whose owner is deleted. It is not a substitute for Get: normal readers must
// keep using Get, which stays fully block- and visibility-scoped.
//
// The admin report queue currently renders only a short context string and
// never loads the check-in body, so this helper has no UI call site yet. Wire
// it in only from an admin/moderator-gated path.
func (s *Service) GetForModeration(ctx context.Context, id string) (*Detail, error) {
	return s.load(ctx, "", id)
}

// load reads the row and its display fields without applying viewer rules.
func (s *Service) load(ctx context.Context, viewerID, id string) (*Detail, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrNotFound
	}
	var (
		d       Detail
		rating  sql.NullInt64
		venueID sql.NullInt64
		toasted int
	)
	err := s.SQL.QueryRowContext(ctx, `
		SELECT c.id, c.user_id, u.handle, COALESCE(u.display_name,''),
		       d.id, d.slug, d.name, COALESCE(b.name,''), COALESCE(st.name,''),
		       c.rating, COALESCE(c.note,''), c.created_at, c.visibility,
		       c.venue_id, COALESCE(v.name,''),
		       COALESCE(c.photo_media_id,''), COALESCE(m.mime,''),
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
		LEFT JOIN styles st ON st.id = d.style_id
		LEFT JOIN venues v ON v.id = c.venue_id
		LEFT JOIN media m ON m.id = c.photo_media_id
		WHERE c.id = ? AND c.deleted_at IS NULL
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'`, viewerID, id).Scan(
		&d.ID, &d.UserID, &d.UserHandle, &d.UserName,
		&d.DrinkID, &d.DrinkSlug, &d.DrinkName, &d.Brand, &d.Style,
		&rating, &d.Note, &d.CreatedAt, &d.Visibility,
		&venueID, &d.VenueName,
		&d.MediaID, &d.MediaMime,
		&d.Toasts, &toasted, &d.CommentsCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if rating.Valid {
		v := int(rating.Int64)
		d.Rating = &v
	}
	if venueID.Valid {
		v := int(venueID.Int64)
		d.VenueID = &v
	}
	d.Toasted = toasted != 0

	name := d.UserName
	if name == "" {
		name = d.UserHandle
	}
	d.UserInitial = initialOf(name)
	return &d, nil
}

// blocked reports whether either user has blocked the other. It queries the
// blocks table directly to avoid an import cycle with internal/social.
func (s *Service) blocked(ctx context.Context, viewerID, ownerID string) bool {
	if viewerID == "" || ownerID == "" || viewerID == ownerID {
		return false
	}
	if s.SQL != nil {
		var exists int
		err := s.SQL.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM blocks
				WHERE (blocker_id = ? AND blocked_id = ?)
				   OR (blocker_id = ? AND blocked_id = ?)
			)`, viewerID, ownerID, ownerID, viewerID).Scan(&exists)
		if err == nil {
			return exists != 0
		}
	}
	if s.Social != nil {
		ok, err := s.Social.AreBlocked(ctx, viewerID, ownerID)
		return err == nil && ok
	}
	return false
}

func (s *Service) visible(ctx context.Context, viewerID string, d *Detail) bool {
	if viewerID != "" && viewerID == d.UserID {
		return true
	}
	switch d.Visibility {
	case "public":
		return true
	case "friends":
		if viewerID == "" {
			return false
		}
		return s.Social != nil && s.Social.AreFriends(ctx, viewerID, d.UserID)
	default: // private, or any unknown value
		return false
	}
}

func initialOf(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(s)[0]))
}
