package checkins

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/drink"
	"sodapop/ent/user"
	"sodapop/ent/userbadge"
	"sodapop/ent/venuedrink"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrForbidden      = errors.New("forbidden")
	ErrInvalidRating  = errors.New("rating must be between 0 and 20 quarter-stars")
	ErrInvalidInput   = errors.New("invalid check-in")
	ErrIdempotencyUse = errors.New("idempotency key already used")
)

// Service owns check-in writes and personal reads.
type Service struct {
	Client *ent.Client
	SQL    *sql.DB
	Social *social.Service
}

func New(database *db.DB, soc *social.Service) *Service {
	return &Service{Client: database.Client, SQL: database.SQL, Social: soc}
}

type CreateInput struct {
	DrinkID        int
	VenueID        *int
	Rating         *int
	Serving        string
	Note           string
	Visibility     string
	IdempotencyKey string
	PhotoMediaID   string
}

// Create logs a check-in. The bool reports whether a new row was created; a
// repeated idempotency key returns the original row and false.
func (s *Service) Create(ctx context.Context, u *ent.User, in CreateInput) (*ent.Checkin, bool, error) {
	if in.DrinkID <= 0 {
		return nil, false, ErrInvalidInput
	}
	if in.Rating != nil && (*in.Rating < 0 || *in.Rating > 20) {
		return nil, false, ErrInvalidRating
	}
	if len(in.Note) > 1000 {
		return nil, false, errors.New("note is too long")
	}
	if _, err := s.Client.Drink.Query().
		Where(drink.ID(in.DrinkID), drink.StatusNEQ(drink.StatusMerged)).
		OnlyID(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, false, ErrNotFound
		}
		return nil, false, err
	}
	if in.VenueID != nil {
		if _, err := s.Client.Venue.Get(ctx, *in.VenueID); err != nil {
			if ent.IsNotFound(err) {
				return nil, false, errors.New("venue not found")
			}
			return nil, false, err
		}
	}
	if in.IdempotencyKey != "" {
		existing, err := s.Client.Checkin.Query().
			Where(checkin.IdempotencyKey(in.IdempotencyKey)).
			Only(ctx)
		if err == nil {
			if existing.UserID != u.ID {
				return nil, false, ErrIdempotencyUse
			}
			return existing, false, nil
		}
		if !ent.IsNotFound(err) {
			return nil, false, err
		}
	}

	visibility := in.Visibility
	if visibility == "" {
		visibility = string(u.DefaultVisibility)
	}
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	builder := tx.Checkin.Create().
		SetUserID(u.ID).
		SetDrinkID(in.DrinkID).
		SetServing(servingEnum(in.Serving)).
		SetNote(in.Note).
		SetVisibility(visibilityEnum(visibility))
	if in.Rating != nil {
		builder.SetRating(*in.Rating)
	}
	if in.VenueID != nil {
		builder.SetVenueID(*in.VenueID)
	}
	if in.PhotoMediaID != "" {
		builder.SetPhotoMediaID(in.PhotoMediaID)
	}
	if in.IdempotencyKey != "" {
		builder.SetIdempotencyKey(in.IdempotencyKey)
	}
	c, err := builder.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	if in.VenueID != nil {
		_ = upsertVenueDrink(ctx, tx, *in.VenueID, in.DrinkID)
	}
	if err := social.LogActivity(ctx, tx, u.ID, "checkin", c.ID, visibility); err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return c, true, nil
}

func upsertVenueDrink(ctx context.Context, tx *ent.Tx, venueID, drinkID int) error {
	existing, err := tx.VenueDrink.Query().
		Where(venuedrink.VenueID(venueID), venuedrink.DrinkID(drinkID)).
		Only(ctx)
	if err == nil {
		_, err = tx.VenueDrink.UpdateOne(existing).SetLastSeenAt(db.Now()).Save(ctx)
		return err
	}
	if !ent.IsNotFound(err) {
		return err
	}
	_, err = tx.VenueDrink.Create().SetVenueID(venueID).SetDrinkID(drinkID).Save(ctx)
	return err
}

// Item is a check-in with its joined display data.
type Item struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	DrinkID    int    `json:"drink_id"`
	DrinkName  string `json:"drink_name"`
	DrinkSlug  string `json:"drink_slug"`
	Brand      string `json:"brand,omitempty"`
	Rating     *int   `json:"rating,omitempty"`
	Serving    string `json:"serving"`
	Note       string `json:"note,omitempty"`
	VenueID    *int   `json:"venue_id,omitempty"`
	VenueName  string `json:"venue_name,omitempty"`
	Visibility string `json:"visibility"`
	CreatedAt  string `json:"created_at"`
	PhotoURL   string `json:"photo_url,omitempty"`
}

// ListForUser returns a user's check-ins. Friends may read public and
// friends-only rows; everyone else is refused. There is no global list.
func (s *Service) ListForUser(ctx context.Context, viewerID, targetID string, limit int, before string) ([]Item, error) {
	if viewerID != targetID && !s.Social.AreFriends(ctx, viewerID, targetID) {
		return nil, ErrForbidden
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q := `
		SELECT c.id, c.user_id, c.drink_id, d.name, d.slug, COALESCE(b.name,''),
		       c.rating, c.serving, c.note, c.venue_id, COALESCE(v.name,''),
		       c.visibility, c.created_at, COALESCE(m.path,'')
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN venues v ON v.id = c.venue_id
		LEFT JOIN media m ON m.id = c.photo_media_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL`
	args := []any{targetID}
	if viewerID != targetID {
		q += ` AND c.visibility IN ('public','friends')`
	}
	if before != "" {
		q += ` AND c.created_at < ?`
		args = append(args, before)
	}
	q += ` ORDER BY c.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		var rating, venueID sql.NullInt64
		var note string
		if err := rows.Scan(&it.ID, &it.UserID, &it.DrinkID, &it.DrinkName, &it.DrinkSlug, &it.Brand,
			&rating, &it.Serving, &note, &venueID, &it.VenueName, &it.Visibility, &it.CreatedAt, &it.PhotoURL); err != nil {
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
		it.Note = note
		out = append(out, it)
	}
	return out, rows.Err()
}

// Delete soft-deletes a check-in owned by userID.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	c, err := s.Client.Checkin.Get(ctx, id)
	if err != nil {
		return ErrNotFound
	}
	if c.UserID != userID {
		return ErrForbidden
	}
	_, err = c.Update().SetDeletedAt(db.Now()).SetUpdatedAt(db.Now()).Save(ctx)
	return err
}

// Stats summarises a user's own drinking history.
type Stats struct {
	Checkins     int      `json:"checkins"`
	UniqueDrinks int      `json:"unique_drinks"`
	AvgRating    *float64 `json:"avg_rating,omitempty"`
	Toasts       int      `json:"toasts_received"`
	Badges       int      `json:"badges"`
}

func (s *Service) Stats(ctx context.Context, userID string) (*Stats, error) {
	out := &Stats{}
	_ = s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT drink_id), AVG(rating)
		FROM checkins WHERE user_id = ? AND deleted_at IS NULL`, userID).
		Scan(&out.Checkins, &out.UniqueDrinks, &out.AvgRating)
	_ = s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM checkin_likes l JOIN checkins c ON c.id = l.checkin_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL`, userID).Scan(&out.Toasts)
	out.Badges, _ = s.Client.UserBadge.Query().Where(userbadge.UserID(userID)).Count(ctx)
	return out, nil
}

// ProfileLookup is a narrow public profile. It never exposes email addresses.
type ProfileLookup struct {
	ID          string
	Handle      string
	DisplayName string
	Bio         string
	CreatedAt   string
}

func LookupProfile(ctx context.Context, client *ent.Client, handle string) (*ProfileLookup, error) {
	u, err := client.User.Query().
		Where(
			user.HandleEqualFold(strings.ToLower(strings.TrimSpace(handle))),
			user.DeletedAtIsNil(),
			user.StatusNEQ(user.StatusDeleted),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &ProfileLookup{ID: u.ID, Handle: u.Handle, DisplayName: u.DisplayName, Bio: u.Bio, CreatedAt: u.CreatedAt}, nil
}

func servingEnum(v string) checkin.Serving {
	switch v {
	case "bottle":
		return checkin.ServingBottle
	case "glass":
		return checkin.ServingGlass
	case "fountain":
		return checkin.ServingFountain
	case "other":
		return checkin.ServingOther
	default:
		return checkin.ServingCan
	}
}

func visibilityEnum(v string) checkin.Visibility {
	switch v {
	case "friends":
		return checkin.VisibilityFriends
	case "private":
		return checkin.VisibilityPrivate
	default:
		return checkin.VisibilityPublic
	}
}
