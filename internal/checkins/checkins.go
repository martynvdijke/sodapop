package checkins

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/drink"
	"sodapop/ent/notification"
	"sodapop/ent/user"
	"sodapop/ent/userbadge"
	"sodapop/ent/venuedrink"
	"sodapop/internal/badges"
	"sodapop/internal/db"
	"sodapop/internal/social"
	"sodapop/internal/streak"
	"sodapop/internal/venues"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrForbidden      = errors.New("forbidden")
	ErrInvalidRating  = errors.New("rating must be between 0 and 20 quarter-stars")
	ErrInvalidInput   = errors.New("invalid check-in")
	ErrIdempotencyUse = errors.New("idempotency key already used")
	ErrInvalidCoords  = errors.New("lat and lng must be provided together and in range")
	ErrInvalidFriend  = errors.New("tagged user is not an accepted friend")
	ErrTooManyFriends = errors.New("too many tagged friends")
	// ErrAtHomeConflict is returned when a request asks for both the shared
	// @home marker and an explicit venue.
	ErrAtHomeConflict = errors.New("choose either at home or a venue")
)

// maxTaggedFriends caps friend tags per check-in so one row cannot fan out an
// unbounded number of notifications.
const maxTaggedFriends = 10

// maxNoteLength caps the free-text note on a check-in. Moderated edits share
// the limit so they cannot store what create would reject.
const maxNoteLength = 1000

// maxPrepNoteLength caps the preparation note. It is deliberately short: it is
// a grinder setting or steep time, not a second free-form review.
const maxPrepNoteLength = 280

// Service owns check-in writes and personal reads.
type Service struct {
	Client *ent.Client
	SQL    *sql.DB
	Social *social.Service
	// Badges is the recap's badge read. It is optional so a Service can be
	// built without it in tests; nil means the recap carries no badges.
	Badges BadgeLister
}

// BadgeLister is the narrow badge read the recap needs: the same list the badge
// endpoint serves, so the recap can filter by award year without a second
// definition of "earned".
type BadgeLister interface {
	ListForUser(ctx context.Context, userID string) ([]badges.Status, error)
}

func New(database *db.DB, soc *social.Service) *Service {
	return &Service{Client: database.Client, SQL: database.SQL, Social: soc}
}

type CreateInput struct {
	DrinkID        int
	VenueID        *int
	Rating         *int
	Serving        string
	PrepMethod     string
	PrepNote       string
	Note           string
	Visibility     string
	IdempotencyKey string
	// PriceCents is the optional amount paid in minor units, with an
	// ISO-4217 PriceCurrency. Currency normalization happens here so both
	// entry points store the same shape.
	PriceCents    *int
	PriceCurrency string
	PhotoMediaID  string
	Lat           *float64
	Lng           *float64
	FriendIDs     []string
	// AtHome marks a check-in at the shared hidden @home venue. It is mutually
	// exclusive with VenueID; coordinates are withheld so a home fix is never
	// stored.
	AtHome bool
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
	if len(in.Note) > maxNoteLength {
		return nil, false, errors.New("note is too long")
	}
	priceCurrency, err := NormalizeCurrency(in.PriceCurrency)
	if err != nil {
		return nil, false, err
	}
	if in.PriceCents != nil && (*in.PriceCents < 0 || *in.PriceCents > 100000) {
		return nil, false, ErrInvalidPrice
	}
	if in.AtHome && in.VenueID != nil {
		return nil, false, ErrAtHomeConflict
	}
	if in.AtHome {
		homeID, err := (&venues.Service{Client: s.Client, SQL: s.SQL}).EnsureHome(ctx)
		if err != nil {
			return nil, false, err
		}
		in.VenueID = &homeID
		// Never persist a coordinate on an @home row.
		in.Lat, in.Lng = nil, nil
	}
	if err := validateCoords(in.Lat, in.Lng); err != nil {
		return nil, false, err
	}
	friendIDs, err := s.normalizeFriendIDs(ctx, u, in.FriendIDs)
	if err != nil {
		return nil, false, err
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
	in.PrepNote = truncateRunes(strings.TrimSpace(in.PrepNote), maxPrepNoteLength)
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	builder := tx.Checkin.Create().
		SetUserID(u.ID).
		SetDrinkID(in.DrinkID).
		SetServing(servingEnum(in.Serving)).
		SetPrepMethod(prepMethodEnum(in.PrepMethod)).
		SetPrepNote(in.PrepNote).
		SetNote(in.Note).
		SetVisibility(visibilityEnum(visibility)).
		SetPriceCurrency(priceCurrency)
	if in.Rating != nil {
		builder.SetRating(*in.Rating)
	}
	if in.PriceCents != nil {
		builder.SetPriceCents(*in.PriceCents)
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
	if in.Lat != nil && in.Lng != nil {
		builder.SetLat(*in.Lat).SetLng(*in.Lng)
	}
	c, err := builder.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	for _, friendID := range friendIDs {
		if _, err := tx.CheckinTag.Create().
			SetCheckinID(c.ID).
			SetUserID(friendID).
			Save(ctx); err != nil {
			_ = tx.Rollback()
			return nil, false, err
		}
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
	// Notifications are best effort and happen only for a real create, so an
	// idempotent replay cannot fan out duplicates.
	if s.Social != nil {
		for _, friendID := range friendIDs {
			_ = s.Social.Notify(ctx, friendID, string(notification.KindMention), u.ID, c.ID, "tagged you in a check-in")
		}
	}
	// Wishlist availability alerts are best effort and in-app only. They run
	// after the commit so a notification error can never undo the check-in.
	s.notifyWishlistFriends(ctx, u, c, visibility)
	return c, true, nil
}

// wishlistFriendWindow is the per-(recipient, drink) cooldown for friend
// check-in alerts. A second friend checking in the same drink within a week is
// not a new reason to interrupt anyone.
const wishlistFriendWindow = 7 * 24 * time.Hour

// notifyWishlistFriends tells accepted friends who have this drink on their
// wishlist. Only check-ins visible to friends qualify, and a recipient who has
// since checked the drink in themselves is skipped because their wishlist entry
// is stale. Every failure is logged and never returned: the check-in is already
// committed and this is a convenience notification.
func (s *Service) notifyWishlistFriends(ctx context.Context, actor *ent.User, c *ent.Checkin, visibility string) {
	if s.Social == nil || actor == nil {
		return
	}
	// Private check-ins are not visible to friends, so they cannot be a reason
	// to send them anything.
	if visibility == "private" {
		return
	}
	friends, err := s.Social.FriendIDs(ctx, actor.ID)
	if err != nil {
		slog.ErrorContext(ctx, "wishlist friend alert: friend ids", "err", err, "user_id", actor.ID)
		return
	}
	if len(friends) == 0 {
		return
	}
	blocked, err := s.Social.BlockedUserIDs(ctx, actor.ID)
	if err != nil {
		slog.ErrorContext(ctx, "wishlist friend alert: blocked ids", "err", err, "user_id", actor.ID)
		return
	}
	blockedSet := make(map[string]bool, len(blocked))
	for _, id := range blocked {
		blockedSet[id] = true
	}
	candidates := make([]any, 0, len(friends))
	for _, id := range friends {
		if id == actor.ID || blockedSet[id] {
			continue
		}
		candidates = append(candidates, id)
	}
	if len(candidates) == 0 {
		return
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(candidates)), ",")
	query := `
		SELECT DISTINCT l.user_id
		FROM lists l
		JOIN list_items li ON li.list_id = l.id
		JOIN user_settings us ON us.user_id = l.user_id
		WHERE l.is_system = 1 AND l.slug = 'wishlist'
		  AND li.drink_id = ? AND us.wishlist_friend_alerts = 1
		  AND l.user_id IN (` + placeholders + `)`
	args := append([]any{c.DrinkID}, candidates...)
	rows, err := s.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		slog.ErrorContext(ctx, "wishlist friend alert: wishlist query", "err", err, "drink_id", c.DrinkID)
		return
	}
	recipients := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			slog.ErrorContext(ctx, "wishlist friend alert: scan", "err", err)
			return
		}
		recipients = append(recipients, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "wishlist friend alert: rows", "err", err)
		return
	}
	if len(recipients) == 0 {
		return
	}

	// Drop anyone who has already logged this drink: a wishlist entry that
	// predates their own check-in is no longer an open wish.
	own, err := s.checkedInUsers(ctx, c.DrinkID, recipients)
	if err != nil {
		slog.ErrorContext(ctx, "wishlist friend alert: existing check-ins", "err", err, "drink_id", c.DrinkID)
		return
	}

	var drinkName string
	if err := s.SQL.QueryRowContext(ctx, `SELECT name FROM drinks WHERE id = ?`, c.DrinkID).Scan(&drinkName); err != nil {
		slog.ErrorContext(ctx, "wishlist friend alert: drink name", "err", err, "drink_id", c.DrinkID)
		return
	}
	text := fmt.Sprintf("%s checked in %s", displayName(actor), drinkName)
	subject := strconv.Itoa(c.DrinkID)
	for _, recipient := range recipients {
		if own[recipient] {
			continue
		}
		if _, err := s.Social.NotifyOnce(ctx, recipient, string(notification.KindWishlistFriend), actor.ID, subject, text, wishlistFriendWindow); err != nil {
			slog.ErrorContext(ctx, "wishlist friend alert: notify", "err", err, "user_id", recipient)
		}
	}
}

// checkedInUsers reports which of recipients have a live check-in for drinkID.
func (s *Service) checkedInUsers(ctx context.Context, drinkID int, recipients []string) (map[string]bool, error) {
	out := make(map[string]bool, len(recipients))
	if len(recipients) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(recipients)+1)
	args = append(args, drinkID)
	for _, id := range recipients {
		args = append(args, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(recipients)), ",")
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT DISTINCT user_id FROM checkins
		WHERE drink_id = ? AND deleted_at IS NULL AND user_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// displayName falls back to the handle so an alert never names someone blank.
func displayName(u *ent.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Handle
}

// validateCoords enforces the all-or-nothing GPS contract. Absent coordinates
// are fine; one half or an out-of-range value is not.
func validateCoords(lat, lng *float64) error {
	if lat == nil && lng == nil {
		return nil
	}
	if lat == nil || lng == nil {
		return ErrInvalidCoords
	}
	if math.IsNaN(*lat) || math.IsInf(*lat, 0) || *lat < -90 || *lat > 90 {
		return ErrInvalidCoords
	}
	if math.IsNaN(*lng) || math.IsInf(*lng, 0) || *lng < -180 || *lng > 180 {
		return ErrInvalidCoords
	}
	return nil
}

// normalizeFriendIDs drops blanks and duplicates, ignores the author, enforces
// the cap and requires every surviving id to be an accepted friend.
func (s *Service) normalizeFriendIDs(ctx context.Context, u *ent.User, raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || id == u.ID || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) > maxTaggedFriends {
		return nil, ErrTooManyFriends
	}
	if len(out) == 0 {
		return nil, nil
	}
	if s.Social == nil {
		return nil, ErrInvalidFriend
	}
	for _, id := range out {
		if !s.Social.AreFriends(ctx, u.ID, id) {
			return nil, ErrInvalidFriend
		}
	}
	return out, nil
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
	PrepMethod string `json:"prep_method,omitempty"`
	PrepNote   string `json:"prep_note,omitempty"`
	Note       string `json:"note,omitempty"`
	VenueID    *int   `json:"venue_id,omitempty"`
	VenueName  string `json:"venue_name,omitempty"`
	AtHome     bool   `json:"at_home,omitempty"`
	Visibility string `json:"visibility"`
	CreatedAt  string `json:"created_at"`
	PhotoID    string `json:"photo_id,omitempty"`
	PhotoURL   string `json:"photo_url,omitempty"`
}

// ListForUser returns a user's check-ins. Friends may read public, friends-only
// and private rows; everyone else is refused. There is no global list.
func (s *Service) ListForUser(ctx context.Context, viewerID, targetID string, limit int, before string) ([]Item, error) {
	if viewerID != targetID && !s.Social.AreFriends(ctx, viewerID, targetID) {
		return nil, ErrForbidden
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q := `
		SELECT c.id, c.user_id, c.drink_id, d.name, d.slug, COALESCE(b.name,''),
		       c.rating, c.serving, COALESCE(c.prep_method,''), COALESCE(c.prep_note,''),
		       c.note, c.venue_id, COALESCE(v.name,''), COALESCE(v.hidden,0),
		       c.visibility, c.created_at, COALESCE(m.id,''), COALESCE(m.mime,'')
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN venues v ON v.id = c.venue_id
		LEFT JOIN media m ON m.id = c.photo_media_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL`
	args := []any{targetID}
	if viewerID != targetID {
		// The caller already refused unless viewer is owner or accepted friend.
		q += ` AND c.visibility IN ('public','friends','private')`
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
		var note, photoMime string
		if err := rows.Scan(&it.ID, &it.UserID, &it.DrinkID, &it.DrinkName, &it.DrinkSlug, &it.Brand,
			&rating, &it.Serving, &it.PrepMethod, &it.PrepNote, &note, &venueID, &it.VenueName, &it.AtHome,
			&it.Visibility, &it.CreatedAt, &it.PhotoID, &photoMime); err != nil {
			return nil, err
		}
		// The API media route is the only authenticated way to fetch a photo.
		// Returning a relative path lets clients prepend their own base URL
		// instead of leaking a server filesystem path.
		if it.PhotoID != "" {
			it.PhotoURL = "/api/v1/media/" + it.PhotoID + "/thumb"
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

// DeleteAsModerator soft-deletes a check-in on behalf of an admin or moderator.
// The ownership check is skipped because the caller already holds the role, but
// a missing or already-deleted row is still ErrNotFound so moderation answers
// 404 like every other read.
func (s *Service) DeleteAsModerator(ctx context.Context, id string) error {
	c, err := s.Client.Checkin.Query().
		Where(checkin.ID(id), checkin.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	_, err = c.Update().SetDeletedAt(db.Now()).SetUpdatedAt(db.Now()).Save(ctx)
	return err
}

// SetNoteAsModerator rewrites the free-text note on any check-in. The cap
// matches the one Create enforces so a moderated edit cannot introduce a note
// the app itself would have refused.
func (s *Service) SetNoteAsModerator(ctx context.Context, id, note string) error {
	note = strings.TrimSpace(note)
	if len(note) > maxNoteLength {
		return ErrInvalidInput
	}
	c, err := s.Client.Checkin.Query().
		Where(checkin.ID(id), checkin.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	_, err = c.Update().SetNote(note).SetUpdatedAt(db.Now()).Save(ctx)
	return err
}

// Stats summarises a user's own drinking history.
type Stats struct {
	Checkins      int      `json:"checkins"`
	UniqueDrinks  int      `json:"unique_drinks"`
	AvgRating     *float64 `json:"avg_rating,omitempty"`
	Toasts        int      `json:"toasts_received"`
	Badges        int      `json:"badges"`
	CurrentStreak int      `json:"current_streak"`
	LongestStreak int      `json:"longest_streak"`
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
	out.CurrentStreak, _ = streak.Current(ctx, s.SQL, userID, time.Now().UTC())
	out.LongestStreak, _ = streak.Longest(ctx, s.SQL, userID)
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

func prepMethodEnum(v string) checkin.PrepMethod {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "tea_bag":
		return checkin.PrepMethodTeaBag
	case "loose_leaf":
		return checkin.PrepMethodLooseLeaf
	case "cold_brew":
		return checkin.PrepMethodColdBrew
	case "matcha":
		return checkin.PrepMethodMatcha
	case "espresso":
		return checkin.PrepMethodEspresso
	case "pour_over":
		return checkin.PrepMethodPourOver
	case "french_press":
		return checkin.PrepMethodFrenchPress
	case "aeropress":
		return checkin.PrepMethodAeropress
	case "moka":
		return checkin.PrepMethodMoka
	case "drip":
		return checkin.PrepMethodDrip
	case "other":
		return checkin.PrepMethodOther
	default:
		return checkin.PrepMethodNone
	}
}

// truncateRunes caps a free-text field at n runes without splitting a
// multibyte character.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
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
