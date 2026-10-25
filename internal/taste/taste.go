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

const (
	defaultTwinLimit = 5
	maxTwinLimit     = 20
	maxRecommend     = 10
	minCommonDrinks  = 3
	minTwinScore     = 50
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
	if len(friends) == 0 {
		return []Recommendation{}, nil
	}

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

	out := []Recommendation{}
	for rows.Next() {
		var (
			rec      Recommendation
			ratings  int
			sum      float64
			friendsN int
		)
		if err := rows.Scan(&rec.DrinkID, &rec.Slug, &rec.Name, &rec.Brand, &rec.Style,
			&ratings, &sum, &friendsN); err != nil {
			return nil, err
		}
		rec.Score = sum / float64(ratings+1)
		rec.Reason = twinReason(friendsN)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func twinReason(n int) string {
	if n == 1 {
		return "Loved by 1 taste twin"
	}
	return fmt.Sprintf("Loved by %d taste twins", n)
}

// computeTwins returns every eligible friend ordered by score descending.
func (s *Service) computeTwins(ctx context.Context, userID string) ([]Twin, error) {
	mine, err := s.ratingVector(ctx, userID)
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
		theirs, err := s.ratingVector(ctx, f.id)
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
// check-ins for the same drink are averaged so each drink counts once.
func (s *Service) ratingVector(ctx context.Context, userID string) (map[int]float64, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT drink_id, AVG(rating)
		FROM checkins
		WHERE user_id = ?
		  AND deleted_at IS NULL
		  AND rating IS NOT NULL
		GROUP BY drink_id`, userID)
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
