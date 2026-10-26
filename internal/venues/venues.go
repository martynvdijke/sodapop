package venues

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"

	"sodapop/ent"
	"sodapop/ent/venue"
	"sodapop/ent/venuesave"
	"sodapop/internal/catalog"
	"sodapop/internal/db"
)

var (
	ErrNotFound = errors.New("venue not found")
	ErrInvalid  = errors.New("invalid venue")
)

type Service struct {
	Client *ent.Client
	SQL    *sql.DB
}

func New(database *db.DB) *Service {
	return &Service{Client: database.Client, SQL: database.SQL}
}

// Venue is a compact venue payload.
type Venue struct {
	ID      int     `json:"id"`
	Slug    string  `json:"slug"`
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	Address string  `json:"address,omitempty"`
	City    string  `json:"city,omitempty"`
	Region  string  `json:"region,omitempty"`
	Country string  `json:"country,omitempty"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Website string  `json:"website,omitempty"`
}

// Search returns a capped venue search. It is never a full directory export.
func (s *Service) Search(ctx context.Context, q string, limit int) ([]Venue, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return nil, catalog.ErrQueryTooShort
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT id, slug, name, kind, address, city, region, country, lat, lng, COALESCE(website,'')
		FROM venues
		WHERE merged_into_id IS NULL AND (lower(name) LIKE lower(?) OR lower(city) LIKE lower(?))
		ORDER BY name LIMIT ?`, "%"+q+"%", "%"+q+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Venue{}
	for rows.Next() {
		var v Venue
		if err := rows.Scan(&v.ID, &v.Slug, &v.Name, &v.Kind, &v.Address, &v.City, &v.Region, &v.Country, &v.Lat, &v.Lng, &v.Website); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Get returns a single venue and the drinks recently seen there.
func (s *Service) Get(ctx context.Context, id int) (*Venue, []VenueDrink, error) {
	var v Venue
	err := s.SQL.QueryRowContext(ctx, `
		SELECT id, slug, name, kind, address, city, region, country, lat, lng, COALESCE(website,'')
		FROM venues WHERE id = ? AND merged_into_id IS NULL`, id).
		Scan(&v.ID, &v.Slug, &v.Name, &v.Kind, &v.Address, &v.City, &v.Region, &v.Country, &v.Lat, &v.Lng, &v.Website)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT d.slug, d.name, COALESCE(b.name,''), vd.last_seen_at, vd.on_offer
		FROM venue_drinks vd
		JOIN drinks d ON d.id = vd.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		WHERE vd.venue_id = ? AND d.status != 'merged'
		ORDER BY vd.last_seen_at DESC LIMIT 20`, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	drinks := []VenueDrink{}
	for rows.Next() {
		var vd VenueDrink
		if err := rows.Scan(&vd.DrinkSlug, &vd.DrinkName, &vd.Brand, &vd.LastSeenAt, &vd.OnOffer); err != nil {
			return nil, nil, err
		}
		drinks = append(drinks, vd)
	}
	return &v, drinks, rows.Err()
}

type VenueDrink struct {
	DrinkSlug  string `json:"drink_slug"`
	DrinkName  string `json:"drink_name"`
	Brand      string `json:"brand,omitempty"`
	LastSeenAt string `json:"last_seen_at"`
	OnOffer    bool   `json:"on_offer"`
}

// CreateInput is a community venue submission.
type CreateInput struct {
	Name    string
	Kind    string
	Address string
	City    string
	Region  string
	Country string
	Lat     float64
	Lng     float64
	Website string
}

// Create deduplicates venues within 150 metres or by identical name+city.
func (s *Service) Create(ctx context.Context, userID string, in CreateInput) (*ent.Venue, bool, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, false, ErrInvalid
	}
	existing, err := s.findNearby(ctx, in)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}
	slug := catalog.Slugify(in.Name)
	if in.City != "" {
		slug = catalog.Slugify(in.Name + " " + in.City)
	}
	unique := slug
	for i := 2; i < 100; i++ {
		n, err := s.Client.Venue.Query().Where(venue.Slug(unique)).Count(ctx)
		if err == nil && n == 0 {
			break
		}
		unique = slug + "-" + itoa(i)
	}
	v, err := s.Client.Venue.Create().
		SetName(in.Name).
		SetSlug(unique).
		SetKind(kindEnum(in.Kind)).
		SetAddress(in.Address).
		SetCity(in.City).
		SetRegion(in.Region).
		SetCountry(in.Country).
		SetLat(in.Lat).
		SetLng(in.Lng).
		SetWebsite(in.Website).
		SetCreatedBy(userID).
		Save(ctx)
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

func (s *Service) findNearby(ctx context.Context, in CreateInput) (*ent.Venue, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT id, name, city, lat, lng FROM venues
		WHERE merged_into_id IS NULL
		  AND (lower(name) = lower(?) OR (abs(lat - ?) < 0.003 AND abs(lng - ?) < 0.003))
		LIMIT 25`, in.Name, in.Lat, in.Lng)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var name, city string
		var lat, lng float64
		if err := rows.Scan(&id, &name, &city, &lat, &lng); err != nil {
			return nil, err
		}
		sameNameCity := strings.EqualFold(strings.TrimSpace(name), in.Name) && strings.EqualFold(strings.TrimSpace(city), strings.TrimSpace(in.City))
		close := in.Lat != 0 && in.Lng != 0 && lat != 0 && lng != 0 && haversine(in.Lat, in.Lng, lat, lng) <= 150
		if sameNameCity || close {
			return s.Client.Venue.Get(ctx, id)
		}
	}
	return nil, rows.Err()
}

// Saved lists the venues a user bookmarked, newest first.
func (s *Service) Saved(ctx context.Context, userID string) ([]Venue, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT v.id, v.slug, v.name, v.kind, COALESCE(v.address,''), COALESCE(v.city,''),
		       COALESCE(v.region,''), COALESCE(v.country,''), v.lat, v.lng, COALESCE(v.website,'')
		FROM venue_saves vs
		JOIN venues v ON v.id = vs.venue_id
		WHERE vs.user_id = ? AND v.merged_into_id IS NULL
		ORDER BY vs.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Venue{}
	for rows.Next() {
		var v Venue
		if err := rows.Scan(&v.ID, &v.Slug, &v.Name, &v.Kind, &v.Address, &v.City,
			&v.Region, &v.Country, &v.Lat, &v.Lng, &v.Website); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Save bookmarks a venue for a user.
func (s *Service) Save(ctx context.Context, userID string, venueID int) error {
	exists, err := s.Client.VenueSave.Query().
		Where(venuesave.UserID(userID), venuesave.VenueID(venueID)).Exist(ctx)
	if err != nil || exists {
		return err
	}
	_, err = s.Client.VenueSave.Create().SetUserID(userID).SetVenueID(venueID).Save(ctx)
	return err
}

// Unsave removes a bookmark.
func (s *Service) Unsave(ctx context.Context, userID string, venueID int) error {
	_, err := s.Client.VenueSave.Delete().
		Where(venuesave.UserID(userID), venuesave.VenueID(venueID)).Exec(ctx)
	return err
}

func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const earth = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earth * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func kindEnum(v string) venue.Kind {
	switch v {
	case "bar":
		return venue.KindBar
	case "restaurant":
		return venue.KindRestaurant
	case "cafe":
		return venue.KindCafe
	case "shop":
		return venue.KindShop
	case "hotel":
		return venue.KindHotel
	case "festival":
		return venue.KindFestival
	case "vending":
		return venue.KindVending
	default:
		return venue.KindOther
	}
}

func itoa(n int) string {
	const digits = "0123456789"
	if n < 10 {
		return string(digits[n])
	}
	return itoa(n/10) + string(digits[n%10])
}
