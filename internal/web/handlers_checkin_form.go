package web

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/internal/auth"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/social"
)

// checkinFriendLimit mirrors the service cap. The picker disables further
// boxes at this number; the service still validates the submitted ids.
const checkinFriendLimit = 10

// checkinVenueLimit keeps the picker short enough to scan at a glance.
const checkinVenueLimit = 15

// checkinVenueOption is a venue row in the composer's select and in the JSON
// picker endpoint. Distance is a preformatted label, never a raw coordinate.
type checkinVenueOption struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	City     string `json:"city,omitempty"`
	Distance string `json:"distance,omitempty"`
}

// externalVenueOption is a nearby provider result before it is promoted into a
// real venue. It carries provider identifiers, never a local id.
type externalVenueOption struct {
	Provider   string  `json:"provider"`
	ProviderID string  `json:"provider_id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind,omitempty"`
	City       string  `json:"city,omitempty"`
	Country    string  `json:"country,omitempty"`
	Distance   string  `json:"distance,omitempty"`
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
}

// checkinFormData is the shared view model for the /checkins/new and /scan
// composer. Both routes render the same template, so they must agree on the
// friend list, venue options and visibility default.
func (s *Server) checkinFormData(ctx context.Context, r *http.Request, d *catalog.DrinkDetail) map[string]any {
	u := UserFromContext(r)
	venueQuery := strings.TrimSpace(r.URL.Query().Get("venue_q"))
	selectedID, _ := strconv.Atoi(r.URL.Query().Get("venue_id"))

	friends := []social.FriendUser{}
	if u != nil && d != nil {
		if detailed, err := s.Social.Friends(ctx, u.ID); err == nil {
			friends = detailed.Friends
		}
	}

	venues := []checkinVenueOption{}
	if d != nil {
		venues = s.checkinVenueOptions(ctx, venueQuery, selectedID)
	}

	defaultVisibility := "public"
	if u != nil && u.DefaultVisibility != "" {
		defaultVisibility = string(u.DefaultVisibility)
	}

	return map[string]any{
		"Drink":             d,
		"Ratings":           ratingChoices(),
		"Idempotency":       auth.RandomToken(16),
		"Venues":            venues,
		"Friends":           friends,
		"FriendLimit":       checkinFriendLimit,
		"VenueQuery":        venueQuery,
		"VenueSearchActive": len([]rune(venueQuery)) >= catalog.MinSearchLen,
		"SelectedVenueID":   selectedID,
		"DefaultVisibility": defaultVisibility,
	}
}

// checkinVenueOptions returns search hits for a typed query, otherwise the
// most recently touched venues. A selected venue is always present so picking
// one survives a search reload.
func (s *Server) checkinVenueOptions(ctx context.Context, query string, selectedID int) []checkinVenueOption {
	opts := []checkinVenueOption{}
	if len([]rune(strings.TrimSpace(query))) >= catalog.MinSearchLen {
		results, err := s.Venues.Search(ctx, query, checkinVenueLimit)
		if err == nil {
			for _, v := range results {
				opts = append(opts, checkinVenueOption{ID: v.ID, Name: v.Name, City: v.City})
			}
		}
	} else {
		opts = s.recentVenues(ctx, checkinVenueLimit)
	}
	return s.withSelectedVenue(ctx, opts, selectedID)
}

func (s *Server) recentVenues(ctx context.Context, limit int) []checkinVenueOption {
	rows, err := s.Auth.SQL.QueryContext(ctx, `
		SELECT id, name, COALESCE(city,'') FROM venues
		WHERE merged_into_id IS NULL AND hidden = 0
		ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return []checkinVenueOption{}
	}
	defer rows.Close()
	out := []checkinVenueOption{}
	for rows.Next() {
		var v checkinVenueOption
		if rows.Scan(&v.ID, &v.Name, &v.City) == nil {
			out = append(out, v)
		}
	}
	return out
}

// withSelectedVenue guarantees the currently chosen venue stays in the list,
// even when a new search no longer matches its name.
func (s *Server) withSelectedVenue(ctx context.Context, opts []checkinVenueOption, selectedID int) []checkinVenueOption {
	if selectedID <= 0 {
		return opts
	}
	for _, v := range opts {
		if v.ID == selectedID {
			return opts
		}
	}
	var v checkinVenueOption
	err := s.Auth.SQL.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(city,'') FROM venues
		WHERE id = ? AND merged_into_id IS NULL AND hidden = 0`, selectedID).
		Scan(&v.ID, &v.Name, &v.City)
	if err != nil {
		return opts
	}
	return append([]checkinVenueOption{v}, opts...)
}

// nearbyVenues ranks a capped set of venues with coordinates by distance from
// the supplied fix. Only the nearest checkinVenueLimit rows reach the picker.
func (s *Server) nearbyVenues(ctx context.Context, lat, lng float64, limit int) []checkinVenueOption {
	rows, err := s.Auth.SQL.QueryContext(ctx, `
		SELECT id, name, COALESCE(city,''), lat, lng FROM venues
		WHERE merged_into_id IS NULL AND hidden = 0 AND lat != 0 AND lng != 0
		ORDER BY updated_at DESC LIMIT ?`, 300)
	if err != nil {
		return []checkinVenueOption{}
	}
	defer rows.Close()

	type ranked struct {
		option checkinVenueOption
		metres float64
	}
	candidates := []ranked{}
	for rows.Next() {
		var (
			v        checkinVenueOption
			vLat, vL float64
		)
		if err := rows.Scan(&v.ID, &v.Name, &v.City, &vLat, &vL); err != nil {
			continue
		}
		metres := haversineMeters(lat, lng, vLat, vL)
		v.Distance = distanceLabel(metres)
		candidates = append(candidates, ranked{option: v, metres: metres})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].metres < candidates[j].metres })

	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	out := make([]checkinVenueOption, 0, limit)
	for _, c := range candidates[:limit] {
		out = append(out, c.option)
	}
	return out
}

// searchVenuesNear runs the normal venue search and, when a fix is present,
// adds distances and orders the hits nearest first.
func (s *Server) searchVenuesNear(ctx context.Context, query string, lat, lng *float64, limit int) []checkinVenueOption {
	results, err := s.Venues.Search(ctx, query, limit)
	if err != nil {
		return []checkinVenueOption{}
	}
	type ranked struct {
		option checkinVenueOption
		metres float64
	}
	rankedRows := make([]ranked, 0, len(results))
	for _, v := range results {
		opt := checkinVenueOption{ID: v.ID, Name: v.Name, City: v.City}
		metres := math.MaxFloat64
		if lat != nil && lng != nil && (v.Lat != 0 || v.Lng != 0) {
			metres = haversineMeters(*lat, *lng, v.Lat, v.Lng)
			opt.Distance = distanceLabel(metres)
		}
		rankedRows = append(rankedRows, ranked{option: opt, metres: metres})
	}
	if lat != nil && lng != nil {
		sort.SliceStable(rankedRows, func(i, j int) bool {
			return rankedRows[i].metres < rankedRows[j].metres
		})
	}
	out := make([]checkinVenueOption, 0, len(rankedRows))
	for _, row := range rankedRows {
		out = append(out, row.option)
	}
	return out
}

// checkinVenueOptionsJSON backs the progressive venue picker. It is
// authenticated, capped, and never returns a full venue directory.
func (s *Server) checkinVenueOptionsJSON(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	if s.Limiter != nil && !s.Limiter.Allow("checkin-venues:"+u.ID, 60, time.Minute) {
		http.Error(w, "too many venue searches", http.StatusTooManyRequests)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	lat, lng, err := optionalCoordinates(r.URL.Query().Get("lat"), r.URL.Query().Get("lng"))
	if err != nil {
		http.Error(w, "invalid location", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if r.URL.Query().Get("source") == "external" {
		s.externalVenuesJSON(w, r, u, lat, lng)
		return
	}

	var opts []checkinVenueOption
	switch {
	case len([]rune(query)) >= catalog.MinSearchLen:
		opts = s.searchVenuesNear(r.Context(), query, lat, lng, checkinVenueLimit)
	case lat != nil:
		opts = s.nearbyVenues(r.Context(), *lat, *lng, checkinVenueLimit)
	default:
		opts = s.recentVenues(r.Context(), checkinVenueLimit)
	}
	if opts == nil {
		opts = []checkinVenueOption{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"venues": opts})
}

// externalVenuesJSON proxies an explicit, consented Photon lookup. The
// coordinates are used for this request only; no result is stored until the
// member picks one and the check-in is saved.
func (s *Server) externalVenuesJSON(w http.ResponseWriter, r *http.Request, u *ent.User, lat, lng *float64) {
	if u == nil || !r.URL.Query().Has("consent") {
		http.Error(w, "consent_required", http.StatusBadRequest)
		return
	}
	if lat == nil || lng == nil {
		http.Error(w, "location required", http.StatusBadRequest)
		return
	}
	if s.Limiter != nil && !s.Limiter.Allow("nearby-geo:"+u.ID, 6, time.Minute) {
		_ = json.NewEncoder(w).Encode(map[string]any{"external": []externalVenueOption{}, "external_status": "rate_limited"})
		return
	}
	out := []externalVenueOption{}
	status := "ok"
	if s.Nearby == nil || !s.Nearby.Enabled {
		status = "unavailable"
	} else if places, err := s.Nearby.Nearby(r.Context(), *lat, *lng); err != nil {
		status = "unavailable"
	} else {
		for _, p := range places {
			if len(out) >= checkinVenueLimit {
				break
			}
			metres := haversineMeters(*lat, *lng, p.Lat, p.Lng)
			if metres > 2000 {
				continue
			}
			out = append(out, externalVenueOption{
				Provider: "photon", ProviderID: p.ProviderID, Name: p.Name, Kind: p.Kind,
				City: p.City, Country: p.Country, Distance: distanceLabel(metres),
				Lat: p.Lat, Lng: p.Lng,
			})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"external": out, "external_status": status})
}

// optionalCoordinates parses a lat/lng pair. Both absent means no location;
// one half, a malformed value, or an out-of-range value is an error.
func optionalCoordinates(latRaw, lngRaw string) (*float64, *float64, error) {
	latRaw = strings.TrimSpace(latRaw)
	lngRaw = strings.TrimSpace(lngRaw)
	if latRaw == "" && lngRaw == "" {
		return nil, nil, nil
	}
	if latRaw == "" || lngRaw == "" {
		return nil, nil, checkins.ErrInvalidCoords
	}
	lat, err := strconv.ParseFloat(latRaw, 64)
	if err != nil {
		return nil, nil, checkins.ErrInvalidCoords
	}
	lng, err := strconv.ParseFloat(lngRaw, 64)
	if err != nil {
		return nil, nil, checkins.ErrInvalidCoords
	}
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 {
		return nil, nil, checkins.ErrInvalidCoords
	}
	if math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180 || lng > 180 {
		return nil, nil, checkins.ErrInvalidCoords
	}
	return &lat, &lng, nil
}

// formBool reads the boolean spellings a checkbox or hidden field may send.
func formBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadius = 6371000
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthRadius * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func distanceLabel(metres float64) string {
	if metres < 1000 {
		rounded := math.Round(metres/10) * 10
		return strconv.FormatFloat(rounded, 'f', 0, 64) + " m"
	}
	km := metres / 1000
	if km < 10 {
		return strconv.FormatFloat(math.Round(km*10)/10, 'f', 1, 64) + " km"
	}
	return strconv.FormatFloat(math.Round(km), 'f', 0, 64) + " km"
}
