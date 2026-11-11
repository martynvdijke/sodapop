package catalog

import (
	"context"
	"fmt"
	"strings"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/ent/drinkbarcode"
)

// BarcodeConflictError reports that a GTIN is already attached to a different
// drink. It wraps ErrBarcodeTaken through Is so existing error checks keep
// working, while still carrying the owning drink so the UI can name it ("that
// barcode already belongs to X").
type BarcodeConflictError struct {
	DrinkID   int
	DrinkSlug string
	DrinkName string
}

// Error keeps the package sentinel's wording so logs and the shared API mapper
// stay unchanged; the detail is additive.
func (e *BarcodeConflictError) Error() string {
	if e.DrinkName != "" {
		return fmt.Sprintf("%s: %q belongs to %s", ErrBarcodeTaken.Error(), e.DrinkName, e.DrinkSlug)
	}
	return ErrBarcodeTaken.Error()
}

// Is makes errors.Is(err, ErrBarcodeTaken) true for a typed conflict.
func (e *BarcodeConflictError) Is(target error) bool { return target == ErrBarcodeTaken }

// ContributeBarcode attaches a member-submitted barcode to an existing drink.
//
// The code is normalized and checksum-validated by NormalizeGTIN, the same
// helper the scan and drink-creation paths use, so a contributed code can never
// be stored in a shape resolution would miss. The write is idempotent for the
// same drink (created=false) and a typed conflict for another drink.
//
// It deliberately does not call AttachBarcode: the admin path records no
// attribution, reports no created flag, and returns a bare ErrBarcodeTaken. This
// path records the contributing member on the revision and names the owning
// drink in the conflict.
func (s *Service) ContributeBarcode(ctx context.Context, userID, drinkSlug, rawCode, symbology string) (string, bool, error) {
	gtin, err := NormalizeGTIN(rawCode)
	if err != nil {
		return "", false, err
	}
	slug := strings.ToLower(strings.TrimSpace(drinkSlug))
	if slug == "" {
		return "", false, ErrNotFound
	}
	target, err := s.Client.Drink.Query().
		Where(drink.Slug(slug), drink.StatusNEQ(drink.StatusMerged)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", false, ErrNotFound
		}
		return "", false, err
	}

	symbology = normalizeSymbology(symbology)

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return "", false, err
	}
	existing, err := tx.DrinkBarcode.Query().
		Where(drinkbarcode.Gtin14(gtin)).
		WithDrink().
		Only(ctx)
	switch {
	case err == nil:
		_ = tx.Rollback()
		if existing.DrinkID == target.ID {
			// Re-submitting a code the drink already has is a no-op, not an
			// error, and must never rewrite the stored row's source.
			return gtin, false, nil
		}
		conflict := &BarcodeConflictError{DrinkID: existing.DrinkID, DrinkSlug: slug}
		if existing.Edges.Drink != nil {
			conflict.DrinkSlug = existing.Edges.Drink.Slug
			conflict.DrinkName = existing.Edges.Drink.Name
		}
		return gtin, false, conflict
	case !ent.IsNotFound(err):
		_ = tx.Rollback()
		return "", false, err
	}

	beforeCount, err := tx.DrinkBarcode.Query().Where(drinkbarcode.DrinkID(target.ID)).Count(ctx)
	if err != nil {
		_ = tx.Rollback()
		return "", false, err
	}
	row, err := tx.DrinkBarcode.Create().
		SetDrinkID(target.ID).
		SetGtin14(gtin).
		SetRawCode(strings.TrimSpace(rawCode)).
		SetSymbology(symbology).
		SetSource(drinkbarcode.SourceUser).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		// A concurrent contribution may have inserted the same GTIN between the
		// lookup above and this insert. Re-read and answer like the first case
		// instead of surfacing a unique-constraint failure.
		if race, qerr := s.Client.DrinkBarcode.Query().
			Where(drinkbarcode.Gtin14(gtin)).
			WithDrink().
			Only(ctx); qerr == nil {
			if race.DrinkID == target.ID {
				return gtin, false, nil
			}
			conflict := &BarcodeConflictError{DrinkID: race.DrinkID, DrinkSlug: slug}
			if race.Edges.Drink != nil {
				conflict.DrinkSlug = race.Edges.Drink.Slug
				conflict.DrinkName = race.Edges.Drink.Name
			}
			return gtin, false, conflict
		}
		return "", false, err
	}
	attributedBy := userID
	if err := recordRevisionTx(ctx, tx, "drink", target.ID, &attributedBy, "barcode",
		map[string]any{"barcode_count": beforeCount},
		map[string]any{
			"barcode_count": beforeCount + 1,
			"gtin14":        gtin,
			"symbology":     row.Symbology,
			"source":        row.Source,
		},
	); err != nil {
		_ = tx.Rollback()
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return gtin, true, nil
}

// normalizeSymbology lower-cases the optional symbology hint and drops anything
// that is not a plain identifier, so a free-text form field cannot store junk.
func normalizeSymbology(raw string) string {
	sym := strings.ToLower(strings.TrimSpace(raw))
	if len(sym) > 32 {
		sym = sym[:32]
	}
	for _, r := range sym {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return ""
		}
	}
	return sym
}
