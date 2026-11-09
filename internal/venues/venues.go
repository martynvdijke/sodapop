package venues

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"
	"strings"

	"sodapop/ent"
	"sodapop/ent/venue"
	"sodapop/ent/venuesave"
	"sodapop/internal/catalog"
	"sodapop/internal/db"
)

const (
	defaultVenueRadiusM = 2000
	maxVenueRadiusM     = 25000
	// geoSearchCandidateCap bounds the rows considered before distance sorting
	// so a GPS-aware search never becomes an unbounded scan.
	geoSearchCandidateCap = 200
)

var (
	ErrNotFound = errors.New("venue not found")
	ErrInvalid  = errors.New("invalid venue")
)

// homeSlug is the fixed slug of the shared hidden @home venue.
const homeSlug = "at-home"

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
	// AtHome marks the single shared hidden @home marker.
	AtHome bool `json:"at_home,omitempty"`
	// DistanceM is only set when the search was given a reference point.
	DistanceM *int `json:"distance_m,omitempty"`
}

// Search returns a capped venue search. It is never a full directory export.
func (s *Service) Search(ctx context.Context, q string, limit int) ([]Venue, error) {
	return s.SearchNear(ctx, q, limit, nil, nil, 0)
}

// SearchNear is Search with optional GPS ordering. When lat/lng are present,
// venues within radiusM metres are returned nearest first and carry
// DistanceM. An empty q is allowed in that mode so a venue picker can show
// "near me" before the user types; without GPS the 2-character minimum still
// applies. A non-empty q is applied as the usual text filter in either mode.
func (s *Service) SearchNear(ctx context.Context, q string, limit int, lat, lng *float64, radiusM int) ([]Venue, error) {
	q = strings.TrimSpace(q)
	haveGeo := lat != nil && lng != nil
	if !haveGeo && len([]rune(q)) < 2 {
		return nil, catalog.ErrQueryTooShort
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	candidates := limit
	var box venueSearchBox
	if haveGeo {
		candidates = geoSearchCandidateCap
		if radiusM <= 0 {
			radiusM = defaultVenueRadiusM
		}
		if radiusM > maxVenueRadiusM {
			radiusM = maxVenueRadiusM
		}
		box = venueSearchBoxFor(*lat, *lng, float64(radiusM)/1000)
	}

	where := []string{"v.merged_into_id IS NULL", "v.hidden = 0"}
	args := []any{}
	if q != "" {
		where = append(where, `(
		    lower(v.name) LIKE lower(?)
		    OR lower(v.city) LIKE lower(?)
		    OR EXISTS (
		      SELECT 1 FROM venue_names n
		      WHERE n.venue_id = v.id AND lower(n.name) LIKE lower(?)
		    )
		  )`)
		args = append(args, "%"+q+"%", "%"+q+"%", "%"+q+"%")
	}
	order := "lower(v.name), v.id"
	if haveGeo {
		where = append(where, "v.lat BETWEEN ? AND ?", box.lngCondition())
		args = append(args, box.latMin, box.latMax, box.lngMin, box.lngMax)
		// Cheap planar ordering inside the bounding box. Exact haversine
		// filtering and ordering happens in Go below; this only ensures the
		// bounded candidate set is the nearest one rather than the first
		// alphabetically.
		cos := math.Cos(*lat * math.Pi / 180)
		order = "((v.lat - ?)*(v.lat - ?) + ((v.lng - ?)*(v.lng - ?))*?), v.id"
		args = append(args, *lat, *lat, *lng, *lng, cos*cos)
	}
	args = append(args, candidates)

	query := `
		SELECT v.id, v.slug, v.name, v.kind, v.address, v.city, v.region, v.country, v.lat, v.lng, COALESCE(v.website,'')
		FROM venues v
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY ` + order + ` LIMIT ?`
	rows, err := s.SQL.QueryContext(ctx, query, args...)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !haveGeo {
		return out, nil
	}

	type scoredVenue struct {
		venue    Venue
		distance float64
	}
	scored := make([]scoredVenue, 0, len(out))
	for _, v := range out {
		d := distanceMeters(*lat, *lng, v.Lat, v.Lng)
		if d > float64(radiusM) {
			continue
		}
		rounded := int(math.Round(d))
		v.DistanceM = &rounded
		scored = append(scored, scoredVenue{venue: v, distance: d})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].distance != scored[j].distance {
			return scored[i].distance < scored[j].distance
		}
		if scored[i].venue.Name != scored[j].venue.Name {
			return scored[i].venue.Name < scored[j].venue.Name
		}
		return scored[i].venue.ID < scored[j].venue.ID
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out = make([]Venue, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.venue)
	}
	return out, nil
}

// venueSearchBox is a latitude/longitude bounding box for the GPS prefilter.
// It keeps the query on the existing (lat, lng) index instead of scanning every
// venue when q is empty.
type venueSearchBox struct {
	latMin, latMax float64
	lngMin, lngMax float64
	wrap           bool // box crosses the antimeridian
}

func (b venueSearchBox) lngCondition() string {
	if b.wrap {
		return "(v.lng >= ? OR v.lng <= ?)"
	}
	return "v.lng BETWEEN ? AND ?"
}

func venueSearchBoxFor(lat, lng, radiusKM float64) venueSearchBox {
	const kmPerDegree = 111.32
	latDelta := radiusKM / kmPerDegree
	box := venueSearchBox{
		latMin: math.Max(-90, lat-latDelta),
		latMax: math.Min(90, lat+latDelta),
	}

	lngDelta := 180.0
	if cos := math.Cos(lat * math.Pi / 180); cos > 1e-9 {
		lngDelta = radiusKM / (kmPerDegree * cos)
	}
	if lngDelta >= 180 {
		box.lngMin, box.lngMax = -180, 180
		return box
	}
	lngMin, lngMax := lng-lngDelta, lng+lngDelta
	switch {
	case lngMax > 180:
		box.wrap = true
		box.lngMin, box.lngMax = lngMin, lngMax-360
	case lngMin < -180:
		box.wrap = true
		box.lngMin, box.lngMax = lngMin+360, lngMax
	default:
		box.lngMin, box.lngMax = lngMin, lngMax
	}
	return box
}

// distanceMeters is the haversine distance between two WGS-84 points.
func distanceMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthRadiusM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// Get returns a single venue and the drinks recently seen there.
func (s *Service) Get(ctx context.Context, id int) (*Venue, []VenueDrink, error) {
	var v Venue
	var hidden bool
	err := s.SQL.QueryRowContext(ctx, `
		SELECT id, slug, name, kind, address, city, region, country, lat, lng, COALESCE(website,''), hidden
		FROM venues WHERE id = ? AND merged_into_id IS NULL`, id).
		Scan(&v.ID, &v.Slug, &v.Name, &v.Kind, &v.Address, &v.City, &v.Region, &v.Country, &v.Lat, &v.Lng, &v.Website, &hidden)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	v.AtHome = hidden || v.Slug == homeSlug
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

// DrinksForAdmin lists every drink seen at a venue for the admin offer tools.
// Get caps its list at the recent 20, which is not enough to manage offers on
// a busy venue.
func (s *Service) DrinksForAdmin(ctx context.Context, venueID int, limit int) ([]VenueDrink, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT d.slug, d.name, COALESCE(b.name,''), vd.last_seen_at, vd.on_offer
		FROM venue_drinks vd
		JOIN drinks d ON d.id = vd.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		WHERE vd.venue_id = ? AND d.status != 'merged'
		ORDER BY vd.on_offer DESC, vd.last_seen_at DESC, lower(d.name)
		LIMIT ?`, venueID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	drinks := []VenueDrink{}
	for rows.Next() {
		var vd VenueDrink
		if err := rows.Scan(&vd.DrinkSlug, &vd.DrinkName, &vd.Brand, &vd.LastSeenAt, &vd.OnOffer); err != nil {
			return nil, err
		}
		drinks = append(drinks, vd)
	}
	return drinks, rows.Err()
}

// SetOnOffer marks a drink as on offer (or clears it) at a venue. It reports
// whether a matching venue_drinks row existed.
func (s *Service) SetOnOffer(ctx context.Context, venueID int, drinkSlug string, onOffer bool) (bool, error) {
	res, err := s.SQL.ExecContext(ctx, `
		UPDATE venue_drinks SET on_offer = ?
		WHERE venue_id = ?
		  AND drink_id = (SELECT id FROM drinks WHERE slug = ? AND status != 'merged')`,
		onOffer, venueID, drinkSlug)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
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
	name, err := normalizeVenueName(in.Name)
	if err != nil {
		return nil, false, err
	}
	in.Name = name
	existing, err := s.findNearby(ctx, in)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		// The submitted name is an alternate spelling/alias of an existing
		// venue. Record it instead of creating a duplicate.
		if err := s.AddName(ctx, existing.ID, in.Name); err != nil {
			return nil, false, err
		}
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
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	v, err := tx.Venue.Create().
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
	if err := addVenueName(ctx, tx.VenueName, v.ID, in.Name); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return v, true, nil
}

// EnsureHome returns the id of the single shared hidden @home venue, creating
// it on first use. It re-selects on a unique conflict so two concurrent first
// check-ins converge on one row.
func (s *Service) EnsureHome(ctx context.Context) (int, error) {
	var id int
	err := s.SQL.QueryRowContext(ctx, `SELECT id FROM venues WHERE slug = ? ORDER BY id LIMIT 1`, homeSlug).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	v, err := s.Client.Venue.Create().
		SetName("@home").
		SetSlug(homeSlug).
		SetKind(venue.KindOther).
		SetHidden(true).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			if e2 := s.SQL.QueryRowContext(ctx, `SELECT id FROM venues WHERE slug = ? ORDER BY id LIMIT 1`, homeSlug).Scan(&id); e2 == nil {
				return id, nil
			}
		}
		return 0, err
	}
	return v.ID, nil
}

// CreateFromExternal promotes a nearby result from an external provider into a
// real, selectable venue. It is keyed by (provider, providerID) so repeated
// prompts reuse the same row. Run inside the existing transaction style: a
// unique violation re-selects the winner.
func (s *Service) CreateFromExternal(ctx context.Context, provider, providerID, name, kind, city, country string, lat, lng float64) (int, error) {
	provider = strings.TrimSpace(provider)
	providerID = strings.TrimSpace(providerID)
	if provider == "" || providerID == "" {
		return 0, ErrInvalid
	}
	find := func() (int, bool, error) {
		var id int
		err := s.SQL.QueryRowContext(ctx, `
			SELECT id FROM venues
			WHERE external_provider = ? AND external_id = ? AND merged_into_id IS NULL
			ORDER BY id LIMIT 1`, provider, providerID).Scan(&id)
		if err == nil {
			return id, true, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if id, ok, err := find(); err != nil {
		return 0, err
	} else if ok {
		return id, nil
	}

	cleanName, err := normalizeVenueName(name)
	if err != nil {
		cleanName = provider + " venue"
	}
	slug := catalog.Slugify(cleanName + " " + provider + " " + providerID)
	if slug == "" {
		slug = provider + "-" + providerID
	}
	unique := slug
	for i := 2; i < 100; i++ {
		n, err := s.Client.Venue.Query().Where(venue.Slug(unique)).Count(ctx)
		if err == nil && n == 0 {
			break
		}
		unique = slug + "-" + itoa(i)
	}
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	v, err := tx.Venue.Create().
		SetName(cleanName).
		SetSlug(unique).
		SetKind(kindEnum(kind)).
		SetCity(city).
		SetCountry(country).
		SetLat(lat).
		SetLng(lng).
		SetHidden(false).
		SetExternalProvider(provider).
		SetExternalID(providerID).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			_ = tx.Rollback()
			if id, ok, e2 := find(); e2 == nil && ok {
				return id, nil
			}
		}
		return 0, err
	}
	if err := addVenueName(ctx, tx.VenueName, v.ID, cleanName); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return v.ID, nil
}

func (s *Service) findNearby(ctx context.Context, in CreateInput) (*ent.Venue, error) {
	named, err := s.findByName(ctx, in.Name, in.City)
	if err != nil {
		return nil, err
	}
	if named != nil {
		return named, nil
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT id, name, city, lat, lng FROM venues
		WHERE merged_into_id IS NULL AND hidden = 0
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
		WHERE vs.user_id = ? AND v.merged_into_id IS NULL AND v.hidden = 0
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

// Save bookmarks a venue for a user. Hidden rows (the @home marker and
// external discovery stubs before promotion) cannot be bookmarked.
func (s *Service) Save(ctx context.Context, userID string, venueID int) error {
	v, err := s.Client.Venue.Query().Where(venue.ID(venueID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if v.Hidden || v.MergedIntoID != nil {
		return ErrInvalid
	}
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
