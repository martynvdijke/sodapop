// Package taste compares users by their drink ratings to surface compatible
// friends ("taste twins") and drinks those twins love but the viewer has not
// tried yet.
package taste

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"

	"sodapop/ent"
	"sodapop/internal/catalog"
	"sodapop/internal/db"
)

// Service owns taste similarity and recommendation queries.
type Service struct {
	Client *ent.Client
	SQL    *sql.DB
}

// New builds a taste service from the shared database bundle.
func New(database *db.DB) *Service {
	return &Service{Client: database.Client, SQL: database.SQL}
}

// Twin is a friend whose ratings correlate with the viewer's.
type Twin struct {
	UserID      string
	Handle      string
	DisplayName string
	Score       int
	Common      int
}

// Recommendation is a drink loved by taste twins that the viewer has not
// checked in yet.
type Recommendation struct {
	DrinkID int
	Slug    string
	Name    string
	Brand   string
	Style   string
	Score   float64
	Reason  string
}

// QueueItem is a friend-derived drink suggestion with concrete reasoning,
// shown as the guided "Drink this next" queue on the taste page.
type QueueItem struct {
	DrinkID   int
	Slug      string
	Name      string
	Brand     string
	Style     string
	Score     float64
	TwinCount int
	AvgRating int
	Reasons   []string
}

const (
	defaultTwinLimit  = 5
	maxTwinLimit      = 20
	maxRecommend      = 10
	defaultQueueLimit = 5
	minCommonDrinks   = 3
	minTwinScore      = 50
)

// Twins returns accepted friends ranked by rating similarity, highest first.
// Only friends with at least three commonly rated drinks are considered.
func (s *Service) Twins(ctx context.Context, userID string, limit int) ([]Twin, error) {
	if limit <= 0 {
		limit = defaultTwinLimit
	}
	if limit > maxTwinLimit {
		limit = maxTwinLimit
	}

	all, err := s.computeTwins(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// Recommendations returns drinks highly rated by compatible twins (score >= 50)
// that the viewer has not checked in yet. At most ten are returned.
func (s *Service) Recommendations(ctx context.Context, userID string, limit int) ([]Recommendation, error) {
	if limit <= 0 || limit > maxRecommend {
		limit = maxRecommend
	}

	friends, err := s.recommendationFriends(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(friends) == 0 {
		return []Recommendation{}, nil
	}

	candidates, err := s.candidateDrinks(ctx, userID, friends, limit)
	if err != nil {
		return nil, err
	}

	out := make([]Recommendation, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, Recommendation{
			DrinkID: c.DrinkID,
			Slug:    c.Slug,
			Name:    c.Name,
			Brand:   c.Brand,
			Style:   c.Style,
			Score:   c.Score,
			Reason:  twinReason(c.TwinCount),
		})
	}
	return out, nil
}

// Queue returns up to limit friend-derived drink suggestions with concrete
// reasoning. It applies the same candidate rules as Recommendations and adds
// per-drink context, so the result is deterministic for a given database.
func (s *Service) Queue(ctx context.Context, userID string, limit int) ([]QueueItem, error) {
	if limit <= 0 {
		limit = defaultQueueLimit
	}
	if limit > maxRecommend {
		limit = maxRecommend
	}

	friends, err := s.recommendationFriends(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(friends) == 0 {
		return []QueueItem{}, nil
	}

	candidates, err := s.candidateDrinks(ctx, userID, friends, limit)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []QueueItem{}, nil
	}

	history, err := s.viewerHistory(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.DrinkID)
	}
	community, err := s.communityAverages(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make([]QueueItem, 0, len(candidates))
	for _, c := range candidates {
		avgRating := 0
		if avg, ok := community[c.DrinkID]; ok {
			avgRating = int(math.Round(avg))
		}
		out = append(out, QueueItem{
			DrinkID:   c.DrinkID,
			Slug:      c.Slug,
			Name:      c.Name,
			Brand:     c.Brand,
			Style:     c.Style,
			Score:     c.Score,
			TwinCount: c.TwinCount,
			AvgRating: avgRating,
			Reasons:   queueReasons(c, history, community[c.DrinkID]),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].TwinCount != out[j].TwinCount {
			return out[i].TwinCount > out[j].TwinCount
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// candidateDrink is one aggregated friend recommendation row.
type candidateDrink struct {
	DrinkID   int
	Slug      string
	Name      string
	Brand     string
	Style     string
	Score     float64
	TwinCount int
	ratingSum float64
	ratings   int
}

// recommendationFriends returns the IDs of twins compatible enough to drive
// recommendations: accepted friends with a taste score of at least 50.
func (s *Service) recommendationFriends(ctx context.Context, userID string) ([]string, error) {
	twins, err := s.computeTwins(ctx, userID)
	if err != nil {
		return nil, err
	}

	friends := make([]string, 0, len(twins))
	for _, twin := range twins {
		if twin.Score >= minTwinScore {
			friends = append(friends, twin.UserID)
		}
	}
	return friends, nil
}

// candidateDrinks aggregates friends' visible ratings for drinks the viewer has
// not checked in, ordered by score descending.
func (s *Service) candidateDrinks(ctx context.Context, userID string, friends []string, limit int) ([]candidateDrink, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(friends)), ",")
	args := make([]any, 0, len(friends)+2)
	for _, id := range friends {
		args = append(args, id)
	}
	args = append(args, userID, limit)

	query := fmt.Sprintf(`
		SELECT d.id, d.slug, d.name,
		       COALESCE(b.name, ''), COALESCE(st.name, ''),
		       COUNT(*) AS rating_count,
		       SUM(c.rating) AS rating_sum,
		       COUNT(DISTINCT c.user_id) AS friend_count
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN styles st ON st.id = d.style_id
		WHERE c.user_id IN (%s)
		  AND c.deleted_at IS NULL
		  AND c.rating IS NOT NULL
		  AND c.visibility IN ('public', 'friends')
		  AND d.status <> 'merged'
		  AND d.merged_into_id IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM checkins v
		      WHERE v.user_id = ? AND v.drink_id = d.id AND v.deleted_at IS NULL
		  )
		GROUP BY d.id, d.slug, d.name, b.name, st.name
		ORDER BY (SUM(c.rating) * 1.0 / (COUNT(*) + 1)) DESC,
		         friend_count DESC, d.name
		LIMIT ?`, placeholders)

	rows, err := s.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []candidateDrink{}
	for rows.Next() {
		var c candidateDrink
		if err := rows.Scan(&c.DrinkID, &c.Slug, &c.Name, &c.Brand, &c.Style,
			&c.ratings, &c.ratingSum, &c.TwinCount); err != nil {
			return nil, err
		}
		c.Score = c.ratingSum / float64(c.ratings+1)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// viewerHistory holds the viewer's own taste context for reason wording.
type viewerHistory struct {
	styles map[string]bool
	brands map[string]bool
	avg    float64
	hasAvg bool
}

// viewerHistory loads the viewer's styles, brands, and mean rating in one pass.
func (s *Service) viewerHistory(ctx context.Context, userID string) (viewerHistory, error) {
	h := viewerHistory{styles: map[string]bool{}, brands: map[string]bool{}}

	styleRows, err := s.SQL.QueryContext(ctx, `
		SELECT DISTINCT st.name
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN styles st ON st.id = d.style_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND st.name IS NOT NULL`, userID)
	if err != nil {
		return h, err
	}
	for styleRows.Next() {
		var name string
		if err := styleRows.Scan(&name); err != nil {
			styleRows.Close()
			return h, err
		}
		h.styles[name] = true
	}
	if err := styleRows.Err(); err != nil {
		styleRows.Close()
		return h, err
	}
	styleRows.Close()

	brandRows, err := s.SQL.QueryContext(ctx, `
		SELECT DISTINCT b.name
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN brands b ON b.id = d.brand_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND b.name IS NOT NULL`, userID)
	if err != nil {
		return h, err
	}
	for brandRows.Next() {
		var name string
		if err := brandRows.Scan(&name); err != nil {
			brandRows.Close()
			return h, err
		}
		h.brands[name] = true
	}
	if err := brandRows.Err(); err != nil {
		brandRows.Close()
		return h, err
	}
	brandRows.Close()

	var avg sql.NullFloat64
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT AVG(rating)
		FROM checkins
		WHERE user_id = ? AND deleted_at IS NULL AND rating IS NOT NULL`, userID).Scan(&avg); err != nil {
		return h, err
	}
	if avg.Valid {
		h.avg = avg.Float64
		h.hasAvg = true
	}
	return h, nil
}

// communityAverages returns the visible community average per drink, but only
// for drinks with at least catalog.MinRaters distinct raters.
func (s *Service) communityAverages(ctx context.Context, drinkIDs []int) (map[int]float64, error) {
	out := map[int]float64{}
	if len(drinkIDs) == 0 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(drinkIDs)), ",")
	args := make([]any, 0, len(drinkIDs))
	for _, id := range drinkIDs {
		args = append(args, id)
	}

	rows, err := s.SQL.QueryContext(ctx, fmt.Sprintf(`
		SELECT drink_id, avg_rating, rater_count
		FROM drink_ratings
		WHERE drink_id IN (%s)`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id    int
			avg   sql.NullFloat64
			rated int
		)
		if err := rows.Scan(&id, &avg, &rated); err != nil {
			return nil, err
		}
		if avg.Valid && rated >= catalog.MinRaters {
			out[id] = avg.Float64
		}
	}
	return out, rows.Err()
}

// queueReasons builds at most three reasons in a fixed order: twin ratings,
// new style, new brand, above the viewer's average.
func queueReasons(c candidateDrink, history viewerHistory, communityAvg float64) []string {
	reasons := make([]string, 0, 3)

	if c.TwinCount > 0 && c.ratings > 0 {
		friendAvg := int(math.Round(c.ratingSum / float64(c.ratings)))
		if friendAvg >= 14 {
			reasons = append(reasons, fmt.Sprintf("%s rated it %d+", twinLabel(c.TwinCount), friendAvg))
		} else {
			reasons = append(reasons, twinLabel(c.TwinCount)+" liked it")
		}
	}
	if c.Style != "" && !history.styles[c.Style] {
		reasons = append(reasons, "New style for you")
	}
	if c.Brand != "" && !history.brands[c.Brand] {
		reasons = append(reasons, "New brand for you")
	}
	if communityAvg > 0 && history.hasAvg && communityAvg > history.avg {
		reasons = append(reasons, "Above your average rating")
	}

	if len(reasons) > 3 {
		reasons = reasons[:3]
	}
	return reasons
}

func twinReason(n int) string {
	if n == 1 {
		return "Loved by 1 taste twin"
	}
	return fmt.Sprintf("Loved by %d taste twins", n)
}

// twinLabel returns a plural-aware "N taste twin(s)" phrase.
func twinLabel(n int) string {
	if n == 1 {
		return "1 taste twin"
	}
	return fmt.Sprintf("%d taste twins", n)
}

// computeTwins returns every eligible friend ordered by score descending.
func (s *Service) computeTwins(ctx context.Context, userID string) ([]Twin, error) {
	mine, err := s.ratingVector(ctx, userID, true)
	if err != nil {
		return nil, err
	}
	if len(mine) == 0 {
		return []Twin{}, nil
	}

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT u.id, u.handle, u.display_name
		FROM friendships f
		JOIN users u ON u.id = CASE WHEN f.user_a = ? THEN f.user_b ELSE f.user_a END
		WHERE f.status = 'accepted'
		  AND (f.user_a = ? OR f.user_b = ?)
		  AND u.status = 'active'
		  AND u.deleted_at IS NULL`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	type friend struct {
		id          string
		handle      string
		displayName string
	}
	friends := []friend{}
	for rows.Next() {
		var f friend
		if err := rows.Scan(&f.id, &f.handle, &f.displayName); err != nil {
			rows.Close()
			return nil, err
		}
		friends = append(friends, f)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	out := make([]Twin, 0, len(friends))
	for _, f := range friends {
		theirs, err := s.ratingVector(ctx, f.id, false)
		if err != nil {
			return nil, err
		}
		common := commonKeys(mine, theirs)
		if len(common) < minCommonDrinks {
			continue
		}
		out = append(out, Twin{
			UserID:      f.id,
			Handle:      f.handle,
			DisplayName: f.displayName,
			Score:       similarity(mine, theirs, common),
			Common:      len(common),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Common != out[j].Common {
			return out[i].Common > out[j].Common
		}
		return out[i].Handle < out[j].Handle
	})
	return out, nil
}

// ratingVector returns the mean rating per drink for one user. Multiple
// check-ins for the same drink are averaged so each drink counts once. When
// includePrivate is false (reading another user), only visible check-ins count.
func (s *Service) ratingVector(ctx context.Context, userID string, includePrivate bool) (map[int]float64, error) {
	query := `
		SELECT drink_id, AVG(rating)
		FROM checkins
		WHERE user_id = ?
		  AND deleted_at IS NULL
		  AND rating IS NOT NULL`
	if !includePrivate {
		query += `
		  AND visibility IN ('public', 'friends')`
	}
	query += `
		GROUP BY drink_id`

	rows, err := s.SQL.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]float64{}
	for rows.Next() {
		var (
			drink  int
			rating float64
		)
		if err := rows.Scan(&drink, &rating); err != nil {
			return nil, err
		}
		out[drink] = rating
	}
	return out, rows.Err()
}

func commonKeys(a, b map[int]float64) []int {
	keys := make([]int, 0)
	for drink := range a {
		if _, ok := b[drink]; ok {
			keys = append(keys, drink)
		}
	}
	return keys
}

// similarity returns a 0..100 score. It uses centered cosine on the common
// drinks; when either centered vector is constant (zero norm) it falls back to
// 1 - normalized mean absolute difference over the 0..20 rating range.
func similarity(a, b map[int]float64, common []int) int {
	var sumA, sumB float64
	for _, drink := range common {
		sumA += a[drink]
		sumB += b[drink]
	}
	n := float64(len(common))
	meanA, meanB := sumA/n, sumB/n

	var dot, normA, normB float64
	for _, drink := range common {
		x := a[drink] - meanA
		y := b[drink] - meanB
		dot += x * y
		normA += x * x
		normB += y * y
	}

	var sim float64
	if normA > 1e-9 && normB > 1e-9 {
		// Cosine is -1..1; remap to a friendly 0..1.
		sim = (dot/(math.Sqrt(normA)*math.Sqrt(normB)) + 1) / 2
	} else {
		var diff float64
		for _, drink := range common {
			diff += math.Abs(a[drink] - b[drink])
		}
		sim = 1 - (diff/n)/20.0
	}

	if sim < 0 {
		sim = 0
	}
	if sim > 1 {
		sim = 1
	}
	return int(math.Round(sim * 100))
}
