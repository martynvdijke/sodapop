package checkins

import (
	"context"
	"math"
	"sort"
	"time"
)

// Caffeine pharmacokinetics used by the insight model. Caffeine has a plasma
// half-life of roughly five hours, and a dose below 20 mg is treated as
// effectively cleared for sleep-planning purposes.
const (
	caffeineHalfLife = 5 * time.Hour
	caffeineClearMg  = 20.0
	caffeineLateSpan = 8 * time.Hour
)

// InsightsOptions scopes an insights query. Bedtime is the user's preferred
// caffeine cutoff clock ("HH:MM", "" when unset). Now is injectable so tests
// can pin the window.
type InsightsOptions struct {
	Days    int
	Bedtime string
	Now     time.Time
}

// DayPoint is one calendar day (UTC) in the insights series. Days without
// pours are present with zero values so a sparkline has no gaps.
type DayPoint struct {
	Date         string  `json:"date"`
	SugarG       float64 `json:"sugar_g"`
	CaffeineMg   float64 `json:"caffeine_mg"`
	CaloriesKcal int     `json:"calories_kcal"`
	Pours        int     `json:"pours"`
}

// DrinkInsight aggregates one drink's contribution to the window.
type DrinkInsight struct {
	Slug       string  `json:"slug"`
	Name       string  `json:"name"`
	Brand      string  `json:"brand"`
	Pours      int     `json:"pours"`
	SugarG     float64 `json:"sugar_g"`
	CaffeineMg float64 `json:"caffeine_mg"`
}

// Insights summarises a user's sugar, caffeine and calorie intake. Values are
// unrounded; formatting happens at the web/API response layer.
type Insights struct {
	Days int    `json:"days"`
	From string `json:"from"`
	To   string `json:"to"`

	SugarG       float64 `json:"sugar_g"`
	CaffeineMg   float64 `json:"caffeine_mg"`
	CaloriesKcal int     `json:"calories_kcal"`
	Pours        int     `json:"pours"`

	AvgSugarPerDay    float64 `json:"avg_sugar_per_day_g"`
	AvgCaffeinePerDay float64 `json:"avg_caffeine_per_day_mg"`

	Series []DayPoint `json:"series"`

	LastCaffeineAt  string `json:"last_caffeine_at"`
	CaffeineClearBy string `json:"caffeine_clear_by"`
	Bedtime         string `json:"bedtime"`
	LateCaffeine    bool   `json:"late_caffeine"`

	TopDrinks []DrinkInsight `json:"top_drinks"`
}

// NormalizeInsightsDays accepts only the supported sparkline windows and falls
// back to 30 days for anything else.
func NormalizeInsightsDays(days int) int {
	switch days {
	case 7, 30, 90:
		return days
	default:
		return 30
	}
}

type insightRow struct {
	createdAt  time.Time
	slug       string
	name       string
	brand      string
	status     string
	sugarG     float64
	caffeineMg float64
	kcal       float64
}

// Insights aggregates nutrition for the user's check-ins in the window. It uses
// a single check-in query and buckets in Go, so the series never runs N+1
// queries.
func (s *Service) Insights(ctx context.Context, userID string, opts InsightsOptions) (*Insights, error) {
	days := NormalizeInsightsDays(opts.Days)
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	// The series is the last `days` calendar days ending today, so today's
	// pours are visible. Tracking starts at midnight of the oldest day, which
	// is at most one day earlier than the rolling now-Days*24h cutoff.
	fromDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, -(days - 1))
	out := &Insights{
		Days:      days,
		From:      fromDay.Format(time.RFC3339),
		To:        now.Format(time.RFC3339),
		Bedtime:   opts.Bedtime,
		Series:    make([]DayPoint, 0, days),
		TopDrinks: []DrinkInsight{},
	}

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT c.created_at, d.slug, d.name, COALESCE(b.name,''), d.status,
		       d.volume_ml, d.sugar_g_per_100ml, d.caffeine_mg_per_100ml,
		       d.calories_kcal_per_100ml
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		  AND c.created_at >= ? AND c.created_at <= ?
		ORDER BY c.created_at ASC`, userID, out.From, out.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	parsed := make([]insightRow, 0, 16)
	for rows.Next() {
		var (
			createdAt string
			row       insightRow
			volumeML  int
			sugarPer  float64
			caffPer   int
			kcalPer   int
		)
		if err := rows.Scan(&createdAt, &row.slug, &row.name, &row.brand, &row.status,
			&volumeML, &sugarPer, &caffPer, &kcalPer); err != nil {
			return nil, err
		}
		ts, perr := time.Parse(time.RFC3339, createdAt)
		if perr != nil {
			continue
		}
		row.createdAt = ts.UTC()
		frac := float64(volumeML) / 100
		row.sugarG = sugarPer * frac
		row.caffeineMg = float64(caffPer) * frac
		row.kcal = float64(kcalPer) * frac
		parsed = append(parsed, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Zero-filled series, one point per calendar day in the window.
	dayIndex := make(map[string]int, days)
	sugarByDay := make([]float64, days)
	caffeineByDay := make([]float64, days)
	kcalByDay := make([]float64, days)
	poursByDay := make([]int, days)
	for i := 0; i < days; i++ {
		d := fromDay.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		dayIndex[key] = i
		out.Series = append(out.Series, DayPoint{Date: key})
	}

	byDrink := make(map[string]*DrinkInsight)
	var totalKcal float64

	var lastCaffeine time.Time
	doses := make([]caffeineDose, 0, len(parsed))

	for _, row := range parsed {
		key := row.createdAt.Format("2006-01-02")
		if i, ok := dayIndex[key]; ok {
			sugarByDay[i] += row.sugarG
			caffeineByDay[i] += row.caffeineMg
			kcalByDay[i] += row.kcal
			poursByDay[i]++
		}

		out.SugarG += row.sugarG
		out.CaffeineMg += row.caffeineMg
		totalKcal += row.kcal
		out.Pours++

		if row.caffeineMg > 0 {
			doses = append(doses, caffeineDose{at: row.createdAt, mg: row.caffeineMg})
			if !row.createdAt.Before(lastCaffeine) {
				lastCaffeine = row.createdAt
			}
		}

		// Merged and provisional drinks are skipped from the leaderboard; the
		// health totals above still count what the user actually logged.
		if row.status == "merged" || row.status == "provisional" {
			continue
		}
		agg, ok := byDrink[row.slug]
		if !ok {
			agg = &DrinkInsight{Slug: row.slug, Name: row.name, Brand: row.brand}
			byDrink[row.slug] = agg
		}
		agg.Pours++
		agg.SugarG += row.sugarG
		agg.CaffeineMg += row.caffeineMg
	}

	out.CaloriesKcal = int(math.Round(totalKcal))

	for i := range out.Series {
		out.Series[i].SugarG = sugarByDay[i]
		out.Series[i].CaffeineMg = caffeineByDay[i]
		out.Series[i].CaloriesKcal = int(math.Round(kcalByDay[i]))
		out.Series[i].Pours = poursByDay[i]
	}

	out.AvgSugarPerDay = out.SugarG / float64(days)
	out.AvgCaffeinePerDay = out.CaffeineMg / float64(days)

	if !lastCaffeine.IsZero() {
		out.LastCaffeineAt = lastCaffeine.Format(time.RFC3339)
		out.CaffeineClearBy = caffeineClearAt(doses, lastCaffeine).Format(time.RFC3339)
		if opts.Bedtime != "" {
			if bed, ok := bedtimeOn(opts.Bedtime, lastCaffeine, now.Location()); ok {
				out.LateCaffeine = lastCaffeine.Add(caffeineLateSpan).After(bed)
			}
		}
	}

	top := make([]DrinkInsight, 0, len(byDrink))
	for _, agg := range byDrink {
		top = append(top, *agg)
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Pours != top[j].Pours {
			return top[i].Pours > top[j].Pours
		}
		return top[i].Name < top[j].Name
	})
	if len(top) > 5 {
		top = top[:5]
	}
	out.TopDrinks = top

	return out, nil
}

// caffeineDose is one caffeine intake at a point in time.
type caffeineDose struct {
	at time.Time
	mg float64
}

// caffeineResidual sums the exponential decay of every dose at instant t.
// Doses logged after t contribute nothing.
func caffeineResidual(doses []caffeineDose, t time.Time) float64 {
	total := 0.0
	for _, d := range doses {
		dt := t.Sub(d.at)
		if dt < 0 {
			continue
		}
		total += d.mg * math.Pow(2, -dt.Hours()/caffeineHalfLife.Hours())
	}
	return total
}

// caffeineClearAt finds when the combined residual of all doses falls to
// caffeineClearMg. It scans forward from the last dose in five-minute steps,
// then bisects the final interval so the answer is not quantised.
func caffeineClearAt(doses []caffeineDose, from time.Time) time.Time {
	const step = 5 * time.Minute
	const maxWait = 48 * time.Hour

	if caffeineResidual(doses, from) <= caffeineClearMg {
		return from
	}
	prev := from
	for elapsed := step; elapsed <= maxWait; elapsed += step {
		t := from.Add(elapsed)
		if caffeineResidual(doses, t) <= caffeineClearMg {
			lo, hi := prev, t
			for i := 0; i < 18; i++ {
				mid := lo.Add(hi.Sub(lo) / 2)
				if caffeineResidual(doses, mid) > caffeineClearMg {
					lo = mid
				} else {
					hi = mid
				}
			}
			return hi
		}
		prev = t
	}
	return from.Add(maxWait)
}

// bedtimeOn resolves an "HH:MM" bedtime to an instant on the local date of
// lastCaffeine.
func bedtimeOn(bedtime string, lastCaffeine time.Time, loc *time.Location) (time.Time, bool) {
	t, err := time.Parse("15:04", bedtime)
	if err != nil {
		return time.Time{}, false
	}
	local := lastCaffeine.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(),
		t.Hour(), t.Minute(), 0, 0, loc), true
}
