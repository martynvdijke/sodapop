package checkins

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/internal/db"
)

// EditWindow is how long after creation an author may still edit a check-in.
// The window is measured from created_at, never from updated_at, so a repeated
// edit cannot extend it.
const EditWindow = 24 * time.Hour

var (
	// ErrEditWindowClosed is returned when an edit arrives after the author's
	// window has passed. It is distinct from ErrInvalidInput so the UI can say
	// why the form refused instead of blaming the field values.
	ErrEditWindowClosed = errors.New("check-ins can only be edited within 24 hours")
	// ErrInvalidCurrency is returned when a price currency is not an ISO-4217
	// shaped three-letter code.
	ErrInvalidCurrency = errors.New("currency must be a three-letter ISO-4217 code")
	// ErrInvalidPrice is returned when a price is outside the accepted range.
	ErrInvalidPrice = errors.New("price must be between 0 and 100000 cents")
)

// UpdateInput carries the fields an author may change. A nil pointer leaves the
// stored value untouched, which lets the API treat the body as a partial patch.
// The Clear flags exist because rating, venue and price are nillable columns: a
// caller needs a way to ask for "no value" that is not "leave it alone".
type UpdateInput struct {
	Rating        *int
	ClearRating   bool
	Serving       *string
	PrepMethod    *string
	PrepNote      *string
	Note          *string
	VenueID       *int
	ClearVenue    bool
	Visibility    *string
	PriceCents    *int
	ClearPrice    bool
	PriceCurrency *string
}

// Update applies an author edit inside the edit window. The bool reports that
// the row was written; a refused edit returns an error and false.
//
// A check-in that is missing, soft-deleted, or owned by someone else is
// ErrNotFound: the edit surface must not reveal that another member's row
// exists, matching Get and Delete.
func (s *Service) Update(ctx context.Context, actorID, checkinID string, in UpdateInput) (bool, error) {
	if strings.TrimSpace(checkinID) == "" {
		return false, ErrNotFound
	}
	c, err := s.Client.Checkin.Query().
		Where(checkin.ID(checkinID), checkin.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return false, ErrNotFound
		}
		return false, err
	}
	if c.UserID != actorID {
		return false, ErrNotFound
	}

	now := time.Now().UTC()
	created, err := time.Parse(time.RFC3339, c.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("%w: created_at is not RFC 3339", ErrInvalidInput)
	}
	if now.Sub(created) > EditWindow {
		return false, ErrEditWindowClosed
	}

	upd := c.Update().SetUpdatedAt(db.Now()).SetEditedAt(db.Now())

	if in.ClearRating {
		upd.ClearRating()
	} else if in.Rating != nil {
		if *in.Rating < 0 || *in.Rating > 20 {
			return false, ErrInvalidRating
		}
		upd.SetRating(*in.Rating)
	}

	if in.Serving != nil && strings.TrimSpace(*in.Serving) != "" {
		serving, ok := parseServing(*in.Serving)
		if !ok {
			return false, fmt.Errorf("%w: unknown serving", ErrInvalidInput)
		}
		upd.SetServing(serving)
	}

	if in.PrepMethod != nil && strings.TrimSpace(*in.PrepMethod) != "" {
		method, ok := parsePrepMethod(*in.PrepMethod)
		if !ok {
			return false, fmt.Errorf("%w: unknown preparation method", ErrInvalidInput)
		}
		upd.SetPrepMethod(method)
	}

	if in.PrepNote != nil {
		upd.SetPrepNote(truncateRunes(strings.TrimSpace(*in.PrepNote), maxPrepNoteLength))
	}

	if in.Note != nil {
		note := strings.TrimSpace(*in.Note)
		if len(note) > maxNoteLength {
			return false, fmt.Errorf("%w: note is too long", ErrInvalidInput)
		}
		upd.SetNote(note)
	}

	if in.ClearVenue {
		upd.ClearVenueID()
	} else if in.VenueID != nil {
		if _, err := s.Client.Venue.Get(ctx, *in.VenueID); err != nil {
			if ent.IsNotFound(err) {
				return false, fmt.Errorf("%w: venue not found", ErrInvalidInput)
			}
			return false, err
		}
		upd.SetVenueID(*in.VenueID)
	}

	if in.Visibility != nil && strings.TrimSpace(*in.Visibility) != "" {
		visibility, ok := parseVisibility(*in.Visibility)
		if !ok {
			return false, fmt.Errorf("%w: unknown visibility", ErrInvalidInput)
		}
		upd.SetVisibility(visibility)
	}

	currencyTouched := false
	if in.PriceCurrency != nil {
		currency, err := NormalizeCurrency(*in.PriceCurrency)
		if err != nil {
			return false, err
		}
		upd.SetPriceCurrency(currency)
		currencyTouched = true
	}

	switch {
	case in.ClearPrice:
		upd.ClearPriceCents()
		if !currencyTouched {
			upd.SetPriceCurrency("")
		}
	case in.PriceCents != nil:
		if *in.PriceCents < 0 || *in.PriceCents > 100000 {
			return false, ErrInvalidPrice
		}
		upd.SetPriceCents(*in.PriceCents)
	}

	if _, err := upd.Save(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// CanEdit reports whether a check-in created at createdAt is still inside the
// author edit window. An unparsable timestamp is treated as closed so the UI
// never offers an action the server would refuse.
func CanEdit(createdAt string, now time.Time) bool {
	created, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return false
	}
	return now.UTC().Sub(created.UTC()) <= EditWindow
}

// NormalizeCurrency upper-cases a currency code and requires it to be either
// empty or exactly three ASCII letters (the ISO-4217 shape). An empty code on a
// priced check-in is accepted and simply means the unit is unknown.
func NormalizeCurrency(raw string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return "", nil
	}
	if len(code) != 3 {
		return "", ErrInvalidCurrency
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return "", ErrInvalidCurrency
		}
	}
	return code, nil
}

// parseServing validates a serving string against the enum the schema allows.
// It reuses servingEnum for the mapping so the list is not duplicated here; an
// unknown value maps to the "can" default and is therefore rejected.
func parseServing(raw string) (checkin.Serving, bool) {
	v := strings.TrimSpace(strings.ToLower(raw))
	got := servingEnum(v)
	return got, string(got) == v
}

// parsePrepMethod validates a preparation method against the schema enum,
// reusing prepMethodEnum for the mapping.
func parsePrepMethod(raw string) (checkin.PrepMethod, bool) {
	v := strings.TrimSpace(strings.ToLower(raw))
	got := prepMethodEnum(v)
	return got, string(got) == v
}

// parseVisibility validates a visibility against the schema enum, reusing
// visibilityEnum for the mapping.
func parseVisibility(raw string) (checkin.Visibility, bool) {
	v := strings.TrimSpace(strings.ToLower(raw))
	got := visibilityEnum(v)
	return got, string(got) == v
}
