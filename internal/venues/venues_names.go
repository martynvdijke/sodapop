package venues

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"sodapop/ent"
	"sodapop/ent/venuename"
	"sodapop/internal/db"
)

const maxVenueName = 120

// ErrMergeCycle is returned when a venue merge would create a loop.
var ErrMergeCycle = errors.New("venue merge cycle")

// normalizeVenueName trims, collapses internal whitespace, and enforces the
// rune cap. Names are stored in this canonical form but compared
// case-insensitively.
func normalizeVenueName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || len([]rune(name)) > maxVenueName {
		return "", ErrInvalid
	}
	return name, nil
}

// AddName records an alternate or historical name for a venue. It is
// idempotent case-insensitively and does not rely on a unique index.
func (s *Service) AddName(ctx context.Context, venueID int, name string) error {
	name, err := normalizeVenueName(name)
	if err != nil {
		return err
	}
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := activeVenue(ctx, tx.Venue, venueID); err != nil {
		return err
	}
	if err := addVenueName(ctx, tx.VenueName, venueID, name); err != nil {
		return err
	}
	return tx.Commit()
}

// Names lists a venue's recorded names, ordered case-insensitively.
func (s *Service) Names(ctx context.Context, venueID int) ([]string, error) {
	if err := activeVenue(ctx, s.Client.Venue, venueID); err != nil {
		return nil, err
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT name FROM venue_names
		WHERE venue_id = ?
		ORDER BY lower(name), id`, venueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// findByName finds an active venue by canonical name or alias, optionally
// qualified by city (case-insensitive). It returns nil, nil when nothing
// matches. Merged venues are never returned.
func (s *Service) findByName(ctx context.Context, name, city string) (*ent.Venue, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	city = strings.TrimSpace(city)
	var id int
	err := s.SQL.QueryRowContext(ctx, `
		SELECT v.id
		FROM venues v
		WHERE v.merged_into_id IS NULL
		  AND (
		    lower(v.name) = lower(?)
		    OR EXISTS (
		      SELECT 1 FROM venue_names n
		      WHERE n.venue_id = v.id AND lower(n.name) = lower(?)
		    )
		  )
		  AND lower(v.city) = lower(?)
		ORDER BY v.id
		LIMIT 1`, name, name, city).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v, err := s.Client.Venue.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return v, err
}

// Merge folds fromID into intoID. The source venue's canonical and alternate
// names become aliases of the target, then the source is marked merged. It is
// idempotent and rejects self-merges and loops. userID is reserved for the
// admin audit wiring; no route calls this yet.
func (s *Service) Merge(ctx context.Context, fromID, intoID int, userID string) error {
	_ = userID
	if fromID <= 0 || intoID <= 0 || fromID == intoID {
		return ErrInvalid
	}
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	from, err := tx.Venue.Get(ctx, fromID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	into, err := tx.Venue.Get(ctx, intoID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if from.MergedIntoID != nil {
		if *from.MergedIntoID == intoID {
			return nil
		}
		return ErrInvalid
	}
	if into.MergedIntoID != nil {
		return ErrInvalid
	}
	for cur := into; cur.MergedIntoID != nil; {
		if *cur.MergedIntoID == fromID {
			return ErrMergeCycle
		}
		next, err := tx.Venue.Get(ctx, *cur.MergedIntoID)
		if err != nil {
			if ent.IsNotFound(err) {
				return ErrInvalid
			}
			return err
		}
		cur = next
	}

	names, err := tx.VenueName.Query().
		Where(venuename.VenueID(fromID)).
		All(ctx)
	if err != nil {
		return err
	}
	if err := addVenueName(ctx, tx.VenueName, intoID, from.Name); err != nil {
		return err
	}
	for _, n := range names {
		if err := addVenueName(ctx, tx.VenueName, intoID, n.Name); err != nil {
			return err
		}
	}
	if _, err := tx.VenueName.Delete().
		Where(venuename.VenueID(fromID)).
		Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Venue.UpdateOneID(fromID).
		SetMergedIntoID(intoID).
		SetUpdatedAt(db.Now()).
		Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// addVenueName inserts name for a venue unless an equivalent name already
// exists. Callers normalize the name; comparison is case-insensitive because
// venue_names has no unique index.
func addVenueName(ctx context.Context, client *ent.VenueNameClient, venueID int, name string) error {
	names, err := client.Query().
		Where(venuename.VenueID(venueID)).
		All(ctx)
	if err != nil {
		return err
	}
	for _, existing := range names {
		if strings.EqualFold(existing.Name, name) {
			return nil
		}
	}
	_, err = client.Create().
		SetVenueID(venueID).
		SetName(name).
		Save(ctx)
	return err
}
