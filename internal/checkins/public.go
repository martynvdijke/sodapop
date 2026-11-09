package checkins

import (
	"context"
	"database/sql"
	"strings"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/ent/usersetting"
)

// PublicFeedFilter scopes the logged-out preview feed. ViewerID is optional and
// only decides whether a row is already toasted; visibility is always public.
type PublicFeedFilter struct {
	ViewerID string
	Limit    int
	Before   string
}

// PublicPerson is one row in the preview people directory. Only opted-in
// public profiles are listed, and every count is derived from public rows.
type PublicPerson struct {
	UserID      string `json:"user_id"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio,omitempty"`
	AvatarID    string `json:"avatar_id,omitempty"`
	Pours       int    `json:"pours"`
	Styles      int    `json:"styles"`
	LastPour    string `json:"last_pour,omitempty"`
}

// PublicFeed returns the newest public check-ins from active, non-deleted
// authors. It is the only global feed in the product, and it is read-only: the
// SQL filters both the row and the author, so friends-only and private rows can
// never leak into a preview page. The people directory separately requires an
// opted-in public profile.
func (s *Service) PublicFeed(ctx context.Context, f PublicFeedFilter) ([]ProfileItem, error) {
	limit := f.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q := `
		SELECT c.id, c.user_id, u.handle, COALESCE(u.display_name,''),
		       c.drink_id, d.name, d.slug, COALESCE(b.name,''), d.container, d.volume_ml,
		       c.rating, COALESCE(c.note,''), COALESCE(v.name,''), COALESCE(v.hidden,0),
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
		WHERE c.visibility = 'public' AND c.deleted_at IS NULL
		  AND u.deleted_at IS NULL AND u.status = 'active'`
	args := []any{f.ViewerID}
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
			&rating, &it.Note, &it.VenueName, &it.AtHome,
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

// PublicPeople lists users who have opted into a public profile. Ordering puts
// the most recently active public profile first, with a deterministic handle
// tie-break.
func (s *Service) PublicPeople(ctx context.Context, limit int) ([]PublicPerson, error) {
	if limit <= 0 || limit > 60 {
		limit = 24
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT u.id, u.handle, COALESCE(u.display_name,''), COALESCE(u.bio,''),
		       COALESCE(u.avatar_media_id,''),
		       (SELECT COUNT(*) FROM checkins c
		          WHERE c.user_id = u.id AND c.visibility = 'public' AND c.deleted_at IS NULL),
		       (SELECT COUNT(DISTINCT d.style_id) FROM checkins c
		          JOIN drinks d ON d.id = c.drink_id
		          WHERE c.user_id = u.id AND c.visibility = 'public'
		            AND c.deleted_at IS NULL AND d.style_id IS NOT NULL),
		       COALESCE((SELECT MAX(c.created_at) FROM checkins c
		          WHERE c.user_id = u.id AND c.visibility = 'public' AND c.deleted_at IS NULL), '') AS last_pour
		FROM users u
		JOIN user_settings s ON s.user_id = u.id AND s.public_profile = 1
		WHERE u.deleted_at IS NULL AND u.status = 'active'
		ORDER BY last_pour = '', last_pour DESC, u.handle ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicPerson{}
	for rows.Next() {
		var p PublicPerson
		if err := rows.Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.Bio, &p.AvatarID,
			&p.Pours, &p.Styles, &p.LastPour); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PublicProfile resolves an opted-in public profile. A non-public or missing
// profile returns ErrNotFound so the preview cannot enumerate accounts.
func (s *Service) PublicProfile(ctx context.Context, handle string) (*ProfileLookup, *ProfileStats, error) {
	handle = strings.ToLower(strings.TrimSpace(handle))
	if handle == "" {
		return nil, nil, ErrNotFound
	}
	u, err := s.Client.User.Query().
		Where(user.HandleEqualFold(handle), user.DeletedAtIsNil(), user.StatusEQ(user.StatusActive)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	public, err := s.Client.UserSetting.Query().
		Where(usersetting.UserID(u.ID), usersetting.PublicProfile(true)).
		Exist(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !public {
		return nil, nil, ErrNotFound
	}
	stats, err := s.ProfileStats(ctx, "", u.ID)
	if err != nil {
		return nil, nil, err
	}
	return &ProfileLookup{
		ID:          u.ID,
		Handle:      u.Handle,
		DisplayName: u.DisplayName,
		Bio:         u.Bio,
		CreatedAt:   u.CreatedAt,
	}, stats, nil
}

// PublicUserCheckins returns one page of public check-ins for an opted-in
// profile. It reuses the profile reader with an anonymous viewer.
func (s *Service) PublicUserCheckins(ctx context.Context, handle string, limit int, before string) ([]ProfileItem, error) {
	profile, _, err := s.PublicProfile(ctx, handle)
	if err != nil {
		return nil, err
	}
	return s.ProfileCheckins(ctx, ProfileCheckinsFilter{
		ViewerID: "",
		TargetID: profile.ID,
		Limit:    limit,
		Before:   before,
	})
}

// PublicCheckin loads a single check-in as an anonymous viewer. Existing
// visibility rules do the work; non-public rows become ErrNotFound.
func (s *Service) PublicCheckin(ctx context.Context, id string) (*Detail, error) {
	return s.Get(ctx, "", id)
}
