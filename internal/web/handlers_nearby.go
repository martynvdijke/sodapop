package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"sodapop/internal/social"
)

const (
	nearbyDefaultRadius = 2.0
	nearbyPageSize      = 20
	// nearbyShelfVenueLimit bounds the wishlist-shelf venues considered.
	nearbyShelfVenueLimit = 10
	// wishlistListSlug is the slug of the per-member system wishlist.
	wishlistListSlug = "wishlist"
)

// wishlistShelfVenue is one nearby venue carrying drinks from the viewer's
// wishlist, shaped for the template.
type wishlistShelfVenue struct {
	VenueID       int
	VenueName     string
	DistanceLabel string
	Matches       []wishlistShelfMatch

	// distanceM is the raw distance, kept for ordering only.
	distanceM int
}

// wishlistShelfMatch is one wishlist drink currently on a venue's shelf.
type wishlistShelfMatch struct {
	DrinkID       int
	DrinkName     string
	Brand         string
	PriceLabel    string
	DistanceLabel string
	AgeLabel      string
	Reporters     int
}

// nearbyPage renders the location picker and, after a search, the visible
// pours nearest to the submitted point. The coordinates live only in this
// request: they are never stored, logged, or echoed back into the page.
func (s *Server) nearbyPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.nearbySearch(w, r)
		return
	}
	s.renderNearby(w, r, http.StatusOK, nearbyDefaultRadius, "", "", "", nil, false, nil)
}

func (s *Server) nearbySearch(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if s.Limiter != nil && !s.Limiter.Allow("nearby:"+u.ID, 30, time.Minute) {
		s.renderNearby(w, r, http.StatusTooManyRequests, nearbyDefaultRadius, "", "Too many nearby searches. Wait a minute and try again.", "", nil, false, nil)
		return
	}

	radius := nearbyDefaultRadius
	if raw := strings.TrimSpace(r.PostFormValue("radius")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			radius = v
		}
	}
	if radius < 0.1 || radius > 25 {
		s.renderNearby(w, r, http.StatusOK, nearbyDefaultRadius, "", "Choose a radius between 0.1 and 25 km.", "", nil, false, nil)
		return
	}

	latRaw := strings.TrimSpace(r.PostFormValue("lat"))
	lngRaw := strings.TrimSpace(r.PostFormValue("lng"))
	if latRaw == "" || lngRaw == "" {
		s.renderNearby(w, r, http.StatusOK, radius, "", "Set your location first, then show nearby pours.", "", nil, false, nil)
		return
	}
	lat, err := strconv.ParseFloat(latRaw, 64)
	if err != nil {
		s.renderNearby(w, r, http.StatusOK, radius, "", "That location could not be read. Try again.", "", nil, false, nil)
		return
	}
	lng, err := strconv.ParseFloat(lngRaw, 64)
	if err != nil {
		s.renderNearby(w, r, http.StatusOK, radius, "", "That location could not be read. Try again.", "", nil, false, nil)
		return
	}

	cursor := strings.TrimSpace(r.PostFormValue("cursor"))
	items, hasMore, nextCursor, err := s.Social.Nearby(r.Context(), u.ID, lat, lng, radius, nearbyPageSize, cursor)
	if err != nil {
		switch {
		case errors.Is(err, social.ErrInvalidLocation):
			s.renderNearby(w, r, http.StatusOK, radius, "", "That location could not be read. Try again.", "", nil, false, nil)
		case errors.Is(err, social.ErrInvalidRadius):
			s.renderNearby(w, r, http.StatusOK, nearbyDefaultRadius, "", "Choose a radius between 0.1 and 25 km.", "", nil, false, nil)
		case errors.Is(err, social.ErrInvalidCursor):
			s.renderNearby(w, r, http.StatusOK, radius, "", "Those results expired. Try the search again.", "", nil, false, nil)
		default:
			s.fail(w, r, err)
		}
		return
	}

	s.renderNearby(w, r, http.StatusOK, radius, nextCursor, "", cursor, items, hasMore,
		s.wishlistShelf(r.Context(), u.ID, lat, lng, radius))
}

// wishlistShelf loads nearby venues that currently carry drinks from the
// viewer's wishlist. It is deliberately best-effort: a failure here is logged
// and the page renders without the section, because the check-in results are
// the primary content. The coordinates are used for this call only.
func (s *Server) wishlistShelf(ctx context.Context, viewerID string, lat, lng, radiusKM float64) []wishlistShelfVenue {
	wanted, err := s.wishlistDrinkIDs(ctx, viewerID)
	if err != nil {
		logNearbyShelfError(ctx, s.Log, "nearby: wishlist lookup", err)
		return nil
	}
	if len(wanted) == 0 {
		return nil
	}
	shelves, err := s.Venues.NearbyShelf(ctx, viewerID, lat, lng, radiusKM, nearbyShelfVenueLimit)
	if err != nil {
		logNearbyShelfError(ctx, s.Log, "nearby: wishlist shelf", err)
		return nil
	}

	out := make([]wishlistShelfVenue, 0, len(shelves))
	for _, shelf := range shelves {
		matches := make([]wishlistShelfMatch, 0, len(shelf.Entries))
		for _, entry := range shelf.Entries {
			if _, ok := wanted[entry.DrinkID]; !ok {
				continue
			}
			match := wishlistShelfMatch{
				DrinkID:       entry.DrinkID,
				DrinkName:     entry.DrinkName,
				Brand:         entry.Brand,
				DistanceLabel: nearbyDistanceLabel(shelf.DistanceM),
				AgeLabel:      nearbyAgeLabel(entry.AgeDays),
				Reporters:     entry.Reporters,
			}
			if entry.PriceCents != nil {
				match.PriceLabel = nearbyPriceLabel(*entry.PriceCents)
			}
			matches = append(matches, match)
		}
		if len(matches) == 0 {
			continue
		}
		out = append(out, wishlistShelfVenue{
			VenueID:       shelf.Venue.ID,
			VenueName:     shelf.Venue.Name,
			DistanceLabel: nearbyDistanceLabel(shelf.DistanceM),
			Matches:       matches,
			distanceM:     shelf.DistanceM,
		})
	}
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].distanceM != out[j].distanceM {
			return out[i].distanceM < out[j].distanceM
		}
		return out[i].VenueName < out[j].VenueName
	})
	return out
}

// wishlistDrinkIDs returns the set of drink ids on the viewer's system
// wishlist. A count first keeps the common empty-wishlist case to one cheap
// query, and the ids are loaded once.
func (s *Server) wishlistDrinkIDs(ctx context.Context, viewerID string) (map[int]struct{}, error) {
	var count int
	err := s.DB.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM list_items li
		JOIN lists l ON l.id = li.list_id
		WHERE l.user_id = ? AND l.slug = ?`, viewerID, wishlistListSlug).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}

	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT li.drink_id
		FROM list_items li
		JOIN lists l ON l.id = li.list_id
		WHERE l.user_id = ? AND l.slug = ?`, viewerID, wishlistListSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[int]struct{}, count)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// nearbyDistanceLabel renders a venue distance in metres below a kilometre and
// in kilometres above it.
func nearbyDistanceLabel(distanceM int) string {
	if distanceM < 1000 {
		return fmt.Sprintf("%d m", distanceM)
	}
	km := float64(distanceM) / 1000
	if distanceM%1000 == 0 {
		return fmt.Sprintf("%d km", distanceM/1000)
	}
	return fmt.Sprintf("%.1f km", km)
}

// nearbyAgeLabel phrases how recently a drink was seen on a shelf.
func nearbyAgeLabel(ageDays int) string {
	switch {
	case ageDays <= 0:
		return "today"
	case ageDays == 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", ageDays)
	}
}

// nearbyPriceLabel mirrors the venue page's shelf price formatting: a bare
// decimal amount with no currency symbol. The repo has no currency concept, so
// nothing here can claim the amount is in euros.
func nearbyPriceLabel(cents int) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// logNearbyShelfError records a wishlist-shelf failure without failing the
// page.
func logNearbyShelfError(ctx context.Context, log *slog.Logger, msg string, err error) {
	if log == nil {
		log = slog.Default()
	}
	log.WarnContext(ctx, msg, "err", err)
}

// renderNearby writes the page used by both the first load and the fetch
// fragment response. cursor retains the submitted value on an error so the
// form can be retried without silently restarting at the newest page.
func (s *Server) renderNearby(w http.ResponseWriter, r *http.Request, status int, radius float64, nextCursor, errMsg, submittedCursor string, items []social.NearbyItem, hasMore bool, wishlistShelf []wishlistShelfVenue) {
	csrf := ""
	if sess := SessionFromContext(r); sess != nil {
		csrf = sess.CsrfToken
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	cursor := nextCursor
	if errMsg != "" {
		cursor = submittedCursor
	}
	locationSet := r.Method == http.MethodPost &&
		strings.TrimSpace(r.PostFormValue("lat")) != "" &&
		strings.TrimSpace(r.PostFormValue("lng")) != ""
	s.render(w, r, status, "nearby", map[string]any{
		"Title":       "Nearby",
		"CSRF":        csrf,
		"Cursor":      cursor,
		"Error":       errMsg,
		"LocationSet": locationSet,
		"Radius":      radius,
		"Empty":       errMsg == "" && items != nil && len(items) == 0,
		"Items":       items,
		"HasMore":     hasMore,

		"WishlistShelf": wishlistShelf,
	})
}
