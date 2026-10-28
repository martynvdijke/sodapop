package checkins

import (
	"context"
	"database/sql"
)

// PassportTotals are the distinct lifetime counters shown on a passport.
type PassportTotals struct {
	Checkins   int `json:"checkins"`
	Styles     int `json:"styles"`
	VenueKinds int `json:"venue_kinds"`
	Countries  int `json:"countries"`
}

// PassportStyle is one style and how often the viewer checked it in.
type PassportStyle struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	LastAt string `json:"last_at"`
}

// PassportVenueKind counts check-ins by venue kind.
type PassportVenueKind struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// PassportCountry counts check-ins by the brand's country.
type PassportCountry struct {
	Country string `json:"country"`
	Count   int    `json:"count"`
}

// PassportRecent is one of the viewer's latest check-ins. Rating is a nullable
// quarter-star integer, not a display label.
type PassportRecent struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Style     string `json:"style"`
	CreatedAt string `json:"created_at"`
	Rating    *int   `json:"rating"`
}

// Passport is the owner-scoped collection of stamp data behind the API
// passport. It never includes another user's rows.
type Passport struct {
	Totals     PassportTotals      `json:"totals"`
	Styles     []PassportStyle     `json:"styles"`
	VenueKinds []PassportVenueKind `json:"venue_kinds"`
	Countries  []PassportCountry   `json:"countries"`
	Recent     []PassportRecent    `json:"recent"`
}

// Passport builds the stamps shown on GET /api/v1/passport. Soft-deleted
// check-ins and check-ins of merged drinks are excluded everywhere, and the
// recent list is capped at eight.
func (s *Service) Passport(ctx context.Context, userID string) (*Passport, error) {
	out := &Passport{
		Styles:     []PassportStyle{},
		VenueKinds: []PassportVenueKind{},
		Countries:  []PassportCountry{},
		Recent:     []PassportRecent{},
	}

	live := `FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'`

	if err := s.SQL.QueryRowContext(ctx, `SELECT COUNT(*) `+live, userID).Scan(&out.Totals.Checkins); err != nil {
		return nil, err
	}
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT d.style_id) `+live+` AND d.style_id IS NOT NULL`, userID).
		Scan(&out.Totals.Styles); err != nil {
		return nil, err
	}
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT v.kind)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN venues v ON v.id = c.venue_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'`, userID).
		Scan(&out.Totals.VenueKinds); err != nil {
		return nil, err
	}
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT b.country)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN brands b ON b.id = d.brand_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND b.country IS NOT NULL AND b.country != ''`, userID).
		Scan(&out.Totals.Countries); err != nil {
		return nil, err
	}

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT st.name, COUNT(*), MAX(c.created_at)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN styles st ON st.id = d.style_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		GROUP BY st.name
		ORDER BY COUNT(*) DESC, st.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var row PassportStyle
		if err := rows.Scan(&row.Name, &row.Count, &row.LastAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Styles = append(out.Styles, row)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	rows, err = s.SQL.QueryContext(ctx, `
		SELECT v.kind, COUNT(*)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN venues v ON v.id = c.venue_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		GROUP BY v.kind
		ORDER BY COUNT(*) DESC, v.kind ASC`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var row PassportVenueKind
		if err := rows.Scan(&row.Kind, &row.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out.VenueKinds = append(out.VenueKinds, row)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	rows, err = s.SQL.QueryContext(ctx, `
		SELECT b.country, COUNT(*)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN brands b ON b.id = d.brand_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND b.country IS NOT NULL AND b.country != ''
		GROUP BY b.country
		ORDER BY COUNT(*) DESC, b.country ASC`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var row PassportCountry
		if err := rows.Scan(&row.Country, &row.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out.Countries = append(out.Countries, row)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	rows, err = s.SQL.QueryContext(ctx, `
		SELECT d.name, d.slug, COALESCE(st.name,''), c.created_at, c.rating
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN styles st ON st.id = d.style_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT 8`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var row PassportRecent
		var rating sql.NullInt64
		if err := rows.Scan(&row.Name, &row.Slug, &row.Style, &row.CreatedAt, &rating); err != nil {
			rows.Close()
			return nil, err
		}
		if rating.Valid {
			n := int(rating.Int64)
			row.Rating = &n
		}
		out.Recent = append(out.Recent, row)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, nil
}
