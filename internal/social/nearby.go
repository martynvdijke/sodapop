package social

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidLocation = errors.New("invalid location")
	ErrInvalidRadius   = errors.New("invalid radius")
	ErrInvalidCursor   = errors.New("invalid cursor")
	ErrInvalidLimit    = errors.New("invalid limit")
)

const (
	nearbyCandidateCap = 200
	// nearbyDistanceBucketM is the resolution of the distance returned to
	// clients. Coarsening the value prevents a response from being used to
	// triangulate the viewer or a venue to metre-level precision.
	nearbyDistanceBucketM = 100
)

// NearbyItem is a friend-scoped check-in at a venue near the viewer.
// DistanceM is bucketed down to 100 m and never raw metres.
type NearbyItem struct {
	FeedItem
	VenueID   int `json:"venue_id,omitempty"`
	DistanceM int `json:"distance_m"`
}

// Nearby returns visible check-ins at venues within radiusKM of the snapped
// point. It never stores the viewer's coordinates: the point is used to build
// a bounding box for the SQL prefilter and to compute haversine distances in
// memory, then discarded.
//
// Candidates are the newest up to nearbyCandidateCap visible venue check-ins
// inside the box. hasMore therefore means more matching rows exist among those
// candidates, not that a total count is known. Ordering is distance ascending
// (bucketed), then created_at descending, then id descending. cursor is the
// opaque value returned by a previous call; rows strictly after it are
// returned.
func (s *Service) Nearby(ctx context.Context, viewerID string, lat, lng float64, radiusKM float64, limit int, cursor string) ([]NearbyItem, bool, string, error) {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 ||
		math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180 || lng > 180 {
		return nil, false, "", ErrInvalidLocation
	}
	if math.IsNaN(radiusKM) || math.IsInf(radiusKM, 0) || radiusKM < 0.1 || radiusKM > 25 {
		return nil, false, "", ErrInvalidRadius
	}
	if limit < 1 || limit > 50 {
		return nil, false, "", ErrInvalidLimit
	}

	// Snap before any use so the exact position never reaches SQL or logs.
	lat = roundTo(lat, 3)
	lng = roundTo(lng, 3)

	cursorDist, cursorCreated, cursorID, err := decodeNearbyCursor(cursor)
	if err != nil {
		return nil, false, "", err
	}

	box := nearbyBoxFor(lat, lng, radiusKM)

	// nearbyVisibilitySQL mirrors the friend/follow ladder in feed().
	const friendOfViewer = `EXISTS (
		SELECT 1 FROM friendships f
		WHERE f.status = 'accepted'
		  AND (
			(f.user_a = ? AND f.user_b = c.user_id)
			OR (f.user_b = ? AND f.user_a = c.user_id)
		  )
	)`
	const nearbyVisibilitySQL = `c.user_id = ?
		OR (
			c.user_id IN (SELECT followee_id FROM follows WHERE follower_id = ?)
			AND (
				c.visibility = 'public'
				OR (c.visibility = 'friends' AND ` + friendOfViewer + `)
			)
		)
		OR (
			c.visibility IN ('public', 'friends')
			AND ` + friendOfViewer + `
		)`

	query := `
		SELECT c.id, c.created_at, v.id, v.lat, v.lng
		FROM checkins c
		JOIN users u ON u.id = c.user_id
		JOIN venues v ON v.id = c.venue_id
		WHERE c.deleted_at IS NULL
		  AND c.venue_id IS NOT NULL
		  AND v.merged_into_id IS NULL
		  AND NOT (v.lat = 0 AND v.lng = 0)
		  AND u.deleted_at IS NULL
		  AND u.status NOT IN ('deleted', 'suspended')
		  AND NOT EXISTS (
			SELECT 1 FROM blocks bl
			WHERE (bl.blocker_id = ? AND bl.blocked_id = c.user_id)
			   OR (bl.blocked_id = ? AND bl.blocker_id = c.user_id)
		  )
		  AND (` + nearbyVisibilitySQL + `)
		  AND v.lat BETWEEN ? AND ?
		  AND ` + box.lngCondition() + `
		ORDER BY c.created_at DESC
		LIMIT ?`

	args := []any{
		viewerID, viewerID, // blocks
		viewerID,           // own
		viewerID,           // followed
		viewerID, viewerID, // friends (followed branch)
		viewerID, viewerID, // friends (friend branch)
	}
	args = append(args, box.args()...)
	args = append(args, nearbyCandidateCap)

	rows, err := s.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, "", err
	}
	defer rows.Close()

	type candidate struct {
		id        string
		createdAt string
		venueID   int
		distanceM int // bucketed
	}
	candidates := make([]candidate, 0, nearbyCandidateCap)
	for rows.Next() {
		var id, createdAt string
		var venueID int
		var vLat, vLng float64
		if err := rows.Scan(&id, &createdAt, &venueID, &vLat, &vLng); err != nil {
			return nil, false, "", err
		}
		metres := distanceMeters(lat, lng, vLat, vLng)
		if metres > radiusKM*1000 {
			continue
		}
		candidates = append(candidates, candidate{
			id:        id,
			createdAt: createdAt,
			venueID:   venueID,
			distanceM: bucketDistance(metres),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, false, "", err
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.distanceM != b.distanceM {
			return a.distanceM < b.distanceM
		}
		if a.createdAt != b.createdAt {
			return a.createdAt > b.createdAt
		}
		return a.id > b.id
	})

	if cursor != "" {
		after := candidates[:0]
		for _, c := range candidates {
			if nearbyAfterCursor(c.distanceM, c.createdAt, c.id, cursorDist, cursorCreated, cursorID) {
				after = append(after, c)
			}
		}
		candidates = after
	}

	hasMore := len(candidates) > limit
	if hasMore {
		candidates = candidates[:limit]
	}
	if len(candidates) == 0 {
		return []NearbyItem{}, false, "", nil
	}

	nextCursor := ""
	if hasMore {
		last := candidates[len(candidates)-1]
		nextCursor = encodeNearbyCursor(last.distanceM, last.createdAt, last.id)
	}

	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.id
	}
	feed, err := s.ByIDs(ctx, viewerID, ids)
	if err != nil {
		return nil, false, "", err
	}
	byID := make(map[string]FeedItem, len(feed))
	for _, item := range feed {
		byID[item.ID] = item
	}

	items := make([]NearbyItem, 0, len(candidates))
	for _, c := range candidates {
		item, ok := byID[c.id]
		if !ok {
			continue
		}
		items = append(items, NearbyItem{
			FeedItem:  item,
			VenueID:   c.venueID,
			DistanceM: c.distanceM,
		})
	}
	return items, hasMore, nextCursor, nil
}

func roundTo(v float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(v*scale) / scale
}

func floorTo(v float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Floor(v*scale) / scale
}

func ceilTo(v float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Ceil(v*scale) / scale
}

// nearbyBox is a latitude/longitude bounding box computed in Go. Only these
// four rounded floats reach SQL; the snapped viewer point never does.
type nearbyBox struct {
	latMin, latMax float64
	lngMin, lngMax float64
	wrapWest       bool // box crosses the antimeridian westward
	wrapEast       bool // box crosses the antimeridian eastward
}

func (b nearbyBox) lngCondition() string {
	switch {
	case b.wrapEast, b.wrapWest:
		return `(v.lng >= ? OR v.lng <= ?)`
	default:
		return `v.lng BETWEEN ? AND ?`
	}
}

func (b nearbyBox) args() []any {
	return []any{b.latMin, b.latMax, b.lngMin, b.lngMax}
}

func nearbyBoxFor(lat, lng, radiusKM float64) nearbyBox {
	const kmPerDegree = 111.32
	latDelta := radiusKM / kmPerDegree
	latMin := math.Max(-90, lat-latDelta)
	latMax := math.Min(90, lat+latDelta)

	lngDelta := 180.0
	if cos := math.Cos(lat * math.Pi / 180); cos > 1e-9 {
		lngDelta = radiusKM / (kmPerDegree * cos)
	}

	box := nearbyBox{
		latMin: floorTo(latMin, 3),
		latMax: ceilTo(latMax, 3),
	}
	if lngDelta >= 180 {
		box.lngMin = -180
		box.lngMax = 180
		return box
	}

	lngMin := lng - lngDelta
	lngMax := lng + lngDelta
	switch {
	case lngMax > 180:
		box.wrapEast = true
		box.lngMin = floorTo(lngMin, 3)
		box.lngMax = ceilTo(lngMax-360, 3)
	case lngMin < -180:
		box.wrapWest = true
		box.lngMin = floorTo(lngMin+360, 3)
		box.lngMax = ceilTo(lngMax, 3)
	default:
		box.lngMin = floorTo(lngMin, 3)
		box.lngMax = ceilTo(lngMax, 3)
	}
	return box
}

func distanceMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earth = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earth * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func bucketDistance(metres float64) int {
	if metres < 0 {
		return 0
	}
	return int(math.Floor(metres/nearbyDistanceBucketM) * nearbyDistanceBucketM)
}

func encodeNearbyCursor(distanceM int, createdAt, id string) string {
	raw := strconv.Itoa(distanceM) + "|" + createdAt + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeNearbyCursor(cursor string) (int, string, string, error) {
	if cursor == "" {
		return 0, "", "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "", "", ErrInvalidCursor
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 {
		return 0, "", "", ErrInvalidCursor
	}
	distanceM, err := strconv.Atoi(parts[0])
	if err != nil || distanceM < 0 {
		return 0, "", "", ErrInvalidCursor
	}
	if _, err := time.Parse(time.RFC3339, parts[1]); err != nil {
		return 0, "", "", ErrInvalidCursor
	}
	if parts[2] == "" {
		return 0, "", "", ErrInvalidCursor
	}
	return distanceM, parts[1], parts[2], nil
}

// nearbyAfterCursor reports whether a row sorts strictly after the cursor in
// the distance-asc, created_at-desc, id-desc order.
func nearbyAfterCursor(distanceM int, createdAt, id string, curDistanceM int, curCreatedAt, curID string) bool {
	if distanceM != curDistanceM {
		return distanceM > curDistanceM
	}
	if createdAt != curCreatedAt {
		return createdAt < curCreatedAt
	}
	return id < curID
}
