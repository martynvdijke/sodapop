package checkins

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RecapTotals are the year-scoped counters shown on a recap card.
type RecapTotals struct {
	Checkins      int      `json:"checkins"`
	UniqueDrinks  int      `json:"unique_drinks"`
	Styles        int      `json:"styles"`
	NewStyles     int      `json:"new_styles"`
	Venues        int      `json:"venues"`
	Cities        int      `json:"cities"`
	Countries     int      `json:"countries"`
	LongestStreak int      `json:"longest_streak"`
	AvgRating     *float64 `json:"avg_rating"`
}

// RecapDrink is a drink leaderboard row.
type RecapDrink struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Brand string `json:"brand"`
	Count int    `json:"count"`
}

// RecapStyle is a style (or brand) leaderboard row. Brands reuse it because
// both carry a name, a slug and a count.
type RecapStyle struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Count int    `json:"count"`
}

// RecapVenue is a venue leaderboard row.
type RecapVenue struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// RecapRatingBucket is one quartile band of the quarter-star rating scale.
type RecapRatingBucket struct {
	Label string `json:"label"`
	Low   int    `json:"low"`
	High  int    `json:"high"`
	Count int    `json:"count"`
}

// RecapPalate compares one average rating signal across two years.
type RecapPalate struct {
	Label    string   `json:"label"`
	ThisYear *float64 `json:"this_year"`
	LastYear *float64 `json:"last_year"`
}

// Recap is the year-in-review aggregate for one member.
type Recap struct {
	Year          int                 `json:"year"`
	Totals        RecapTotals         `json:"totals"`
	TopDrink      *RecapDrink         `json:"top_drink"`
	TopBrand      *RecapStyle         `json:"top_brand"`
	TopStyles     []RecapStyle        `json:"top_styles"`
	TopVenue      *RecapVenue         `json:"top_venue"`
	RatingBuckets []RecapRatingBucket `json:"rating_buckets"`
	Palate        []RecapPalate       `json:"palate"`
	Fingerprint   *Fingerprint        `json:"fingerprint"`
	HasData       bool                `json:"has_data"`
	// Sparse is true for a year with fewer than five check-ins, so the UI can
	// soften its claims instead of drawing conclusions from almost nothing.
	Sparse bool `json:"sparse"`
}

// FingerprintAxis is one flavour category's share of a member's palate.
type FingerprintAxis struct {
	Key       string  `json:"key"`
	Label     string  `json:"label"`
	Score     float64 `json:"score"`
	Samples   int     `json:"samples"`
	Confident bool    `json:"confident"`
}

// Fingerprint is the flavour fingerprint for a member's whole history.
type Fingerprint struct {
	Axes      []FingerprintAxis `json:"axes"`
	HasData   bool              `json:"has_data"`
	Confident bool              `json:"confident"`
}

// recapMinYear and recapMaxYear bound the accepted year window. A recap outside
// this range is a caller bug, not a year that merely has no data.
const (
	recapMinYear = 2000
	recapMaxYear = 2100
)

// ratingBuckets is the fixed quartile layout of the 0–20 rating scale.
var ratingBuckets = []RecapRatingBucket{
	{Label: "0–4", Low: 0, High: 4},
	{Label: "5–8", Low: 5, High: 8},
	{Label: "9–12", Low: 9, High: 12},
	{Label: "13–16", Low: 13, High: 16},
	{Label: "17–20", Low: 17, High: 20},
}

// flavourCategoryLabels maps the seeded flavour categories to display labels.
var flavourCategoryLabels = map[string]string{
	"fruit":     "Fruit",
	"sweet":     "Sweet",
	"spice":     "Spice",
	"botanical": "Botanical",
	"classic":   "Classic",
	"acid":      "Acid",
	"mineral":   "Mineral",
}

// recapLive is the shared FROM/WHERE prefix. Soft-deleted check-ins and
// check-ins of merged drinks never count anywhere in a recap.
const recapLive = `FROM checkins c
	JOIN drinks d ON d.id = c.drink_id
	WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'`

// Recap aggregates one member's year. It is strictly owner-scoped and never
// reads another member's rows.
func (s *Service) Recap(ctx context.Context, userID string, year int) (*Recap, error) {
	if year < recapMinYear || year > recapMaxYear {
		return nil, fmt.Errorf("recap year %d outside %d..%d", year, recapMinYear, recapMaxYear)
	}
	start := fmt.Sprintf("%d-01-01T00:00:00Z", year)
	end := fmt.Sprintf("%d-01-01T00:00:00Z", year+1)

	out := &Recap{
		Year:          year,
		TopStyles:     []RecapStyle{},
		RatingBuckets: make([]RecapRatingBucket, len(ratingBuckets)),
		Palate:        []RecapPalate{},
	}
	copy(out.RatingBuckets, ratingBuckets)

	if err := s.recapTotals(ctx, userID, start, end, &out.Totals); err != nil {
		return nil, err
	}
	out.HasData = out.Totals.Checkins > 0
	out.Sparse = out.Totals.Checkins > 0 && out.Totals.Checkins < 5

	streak, err := s.recapLongestStreak(ctx, userID, start, end)
	if err != nil {
		return nil, err
	}
	out.Totals.LongestStreak = streak

	if out.HasData {
		if out.TopDrink, err = s.recapTopDrink(ctx, userID, start, end); err != nil {
			return nil, err
		}
		if out.TopBrand, err = s.recapTopBrand(ctx, userID, start, end); err != nil {
			return nil, err
		}
		if out.TopStyles, err = s.recapTopStyles(ctx, userID, start, end); err != nil {
			return nil, err
		}
		if out.TopVenue, err = s.recapTopVenue(ctx, userID, start, end); err != nil {
			return nil, err
		}
	}

	if err := s.recapRatingBuckets(ctx, userID, start, end, out.RatingBuckets); err != nil {
		return nil, err
	}
	if out.Palate, err = s.recapPalate(ctx, userID, year); err != nil {
		return nil, err
	}
	if out.Fingerprint, err = s.Fingerprint(ctx, userID); err != nil {
		return nil, err
	}
	return out, nil
}

// recapTotals fills the scalar counters for the window. Cities and countries
// come from the venue a check-in was logged at; empty strings and NULLs are
// not places.
func (s *Service) recapTotals(ctx context.Context, userID, start, end string, out *RecapTotals) error {
	var avg sql.NullFloat64
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COUNT(DISTINCT c.drink_id),
		       COUNT(DISTINCT d.style_id),
		       COUNT(DISTINCT c.venue_id),
		       COUNT(DISTINCT NULLIF(v.city, '')),
		       COUNT(DISTINCT NULLIF(v.country, '')),
		       AVG(c.rating)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN venues v ON v.id = c.venue_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND c.created_at >= ? AND c.created_at < ?`,
		userID, start, end).
		Scan(&out.Checkins, &out.UniqueDrinks, &out.Styles, &out.Venues,
			&out.Cities, &out.Countries, &avg); err != nil {
		return err
	}
	if avg.Valid {
		v := avg.Float64
		out.AvgRating = &v
	}

	// A style is "new" when this member's first ever check-in of it falls
	// inside the window.
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT st.id
			FROM checkins c
			JOIN drinks d ON d.id = c.drink_id
			JOIN styles st ON st.id = d.style_id
			WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
			GROUP BY st.id
			HAVING MIN(c.created_at) >= ? AND MIN(c.created_at) < ?
		)`, userID, start, end).Scan(&out.NewStyles); err != nil {
		return err
	}
	return nil
}

// recapLongestStreak finds the longest run of consecutive calendar days with a
// check-in inside the window. Days are compared in UTC.
func (s *Service) recapLongestStreak(ctx context.Context, userID, start, end string) (int, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT DISTINCT date(c.created_at)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND c.created_at >= ? AND c.created_at < ?
		ORDER BY 1 ASC`, userID, start, end)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	longest, run := 0, 0
	var prev time.Time
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return 0, err
		}
		ts, perr := time.Parse("2006-01-02", day)
		if perr != nil {
			continue
		}
		if !prev.IsZero() && ts.Equal(prev.AddDate(0, 0, 1)) {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
		prev = ts
	}
	return longest, rows.Err()
}

func (s *Service) recapTopDrink(ctx context.Context, userID, start, end string) (*RecapDrink, error) {
	var row RecapDrink
	err := s.SQL.QueryRowContext(ctx, `
		SELECT d.name, d.slug, COALESCE(b.name, ''), COUNT(*)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND c.created_at >= ? AND c.created_at < ?
		GROUP BY d.id
		ORDER BY COUNT(*) DESC, d.name ASC
		LIMIT 1`, userID, start, end).
		Scan(&row.Name, &row.Slug, &row.Brand, &row.Count)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Service) recapTopBrand(ctx context.Context, userID, start, end string) (*RecapStyle, error) {
	var row RecapStyle
	err := s.SQL.QueryRowContext(ctx, `
		SELECT b.name, b.slug, COUNT(*)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN brands b ON b.id = d.brand_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND c.created_at >= ? AND c.created_at < ?
		GROUP BY b.id
		ORDER BY COUNT(*) DESC, b.name ASC
		LIMIT 1`, userID, start, end).
		Scan(&row.Name, &row.Slug, &row.Count)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Service) recapTopStyles(ctx context.Context, userID, start, end string) ([]RecapStyle, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT st.name, st.slug, COUNT(*)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN styles st ON st.id = d.style_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND c.created_at >= ? AND c.created_at < ?
		GROUP BY st.id
		ORDER BY COUNT(*) DESC, st.name ASC
		LIMIT 5`, userID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RecapStyle, 0, 5)
	for rows.Next() {
		var row RecapStyle
		if err := rows.Scan(&row.Name, &row.Slug, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// recapTopVenue picks the most-visited real venue. The synthetic hidden @home
// marker is not a place worth bragging about, so it is skipped.
func (s *Service) recapTopVenue(ctx context.Context, userID, start, end string) (*RecapVenue, error) {
	var row RecapVenue
	err := s.SQL.QueryRowContext(ctx, `
		SELECT v.id, v.name, COUNT(*)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN venues v ON v.id = c.venue_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND v.hidden = 0
		  AND c.created_at >= ? AND c.created_at < ?
		GROUP BY v.id
		ORDER BY COUNT(*) DESC, v.name ASC
		LIMIT 1`, userID, start, end).
		Scan(&row.ID, &row.Name, &row.Count)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// recapRatingBuckets fills the fixed five-band histogram. NULL ratings are
// ignored, and every band is present even when it is empty.
func (s *Service) recapRatingBuckets(ctx context.Context, userID, start, end string, buckets []RecapRatingBucket) error {
	var counts [5]int
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN c.rating BETWEEN 0 AND 4 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN c.rating BETWEEN 5 AND 8 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN c.rating BETWEEN 9 AND 12 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN c.rating BETWEEN 13 AND 16 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN c.rating BETWEEN 17 AND 20 THEN 1 ELSE 0 END), 0)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND c.rating IS NOT NULL
		  AND c.created_at >= ? AND c.created_at < ?`, userID, start, end).
		Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4]); err != nil {
		return err
	}
	for i := range buckets {
		if i < len(counts) {
			buckets[i].Count = counts[i]
		}
	}
	return nil
}

// recapPalate compares the overall average rating with the previous year. It
// only reports a signal that genuinely exists in both years; with no second
// derivable metric it returns the single overall row, or nothing at all.
func (s *Service) recapPalate(ctx context.Context, userID string, year int) ([]RecapPalate, error) {
	thisAvg, thisOK, err := s.yearAvgRating(ctx, userID, year)
	if err != nil {
		return nil, err
	}
	lastAvg, lastOK, err := s.yearAvgRating(ctx, userID, year-1)
	if err != nil {
		return nil, err
	}
	if !thisOK || !lastOK {
		return []RecapPalate{}, nil
	}
	return []RecapPalate{{Label: "Overall", ThisYear: &thisAvg, LastYear: &lastAvg}}, nil
}

func (s *Service) yearAvgRating(ctx context.Context, userID string, year int) (float64, bool, error) {
	start := fmt.Sprintf("%d-01-01T00:00:00Z", year)
	end := fmt.Sprintf("%d-01-01T00:00:00Z", year+1)
	var avg sql.NullFloat64
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT AVG(c.rating)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		  AND c.rating IS NOT NULL
		  AND c.created_at >= ? AND c.created_at < ?`, userID, start, end).Scan(&avg); err != nil {
		return 0, false, err
	}
	if !avg.Valid {
		return 0, false, nil
	}
	return avg.Float64, true, nil
}

// Fingerprint summarises a member's whole-history palate by flavour category.
// Each contributing drink-flavour row is weighted by the check-in's rating
// (unrated counts as the mid-point 10) times the flavour intensity, then every
// category is normalised so the member's strongest category is 100.
func (s *Service) Fingerprint(ctx context.Context, userID string) (*Fingerprint, error) {
	out := &Fingerprint{Axes: []FingerprintAxis{}}

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT f.category,
		       COUNT(DISTINCT c.id),
		       COALESCE(SUM((COALESCE(c.rating, 10) / 10.0) * (df.intensity / 5.0)), 0)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN drink_flavours df ON df.drink_id = d.id
		JOIN flavours f ON f.id = df.flavour_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.status <> 'merged'
		GROUP BY f.category`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type categoryScore struct {
		key   string
		sum   float64
		count int
	}
	scores := make([]categoryScore, 0, 8)
	maxSum := 0.0
	for rows.Next() {
		var cs categoryScore
		if err := rows.Scan(&cs.key, &cs.count, &cs.sum); err != nil {
			return nil, err
		}
		if cs.sum > maxSum {
			maxSum = cs.sum
		}
		scores = append(scores, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(scores) == 0 {
		return out, nil
	}

	axes := make([]FingerprintAxis, 0, len(scores))
	for _, cs := range scores {
		score := 0.0
		if maxSum > 0 {
			score = cs.sum / maxSum * 100
		}
		axes = append(axes, FingerprintAxis{
			Key:       cs.key,
			Label:     flavourLabel(cs.key),
			Score:     score,
			Samples:   cs.count,
			Confident: cs.count >= 5,
		})
	}
	sort.Slice(axes, func(i, j int) bool {
		if axes[i].Score != axes[j].Score {
			return axes[i].Score > axes[j].Score
		}
		return axes[i].Key < axes[j].Key
	})
	if len(axes) > 5 {
		axes = axes[:5]
	}

	confident := 0
	for _, ax := range axes {
		if ax.Confident {
			confident++
		}
	}
	out.Axes = axes
	out.HasData = true
	out.Confident = confident >= 3
	return out, nil
}

// flavourLabel renders a flavour category for display, falling back to a
// capitalised slug for categories added after this map was written.
func flavourLabel(category string) string {
	if label, ok := flavourCategoryLabels[category]; ok {
		return label
	}
	if category == "" {
		return "Other"
	}
	return strings.ToUpper(category[:1]) + category[1:]
}
