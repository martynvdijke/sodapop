package web

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"sodapop/internal/checkins"
)

// recapYearFloor is the oldest year a recap may describe. Anything earlier is
// a typo or a probe rather than a year with no data, so it is a 404.
const recapYearFloor = 2000

// recapYearCap bounds the year switcher. Eight chips fit on a phone without
// turning the nav into a wall of numbers.
const recapYearCap = 8

// recapCacheTTL is how long a built recap is reused. Eight aggregate queries
// per visit is wasteful for a page nobody edits; a fresh check-in appearing up
// to five minutes late is a fair trade. The key always carries the user id, so
// one member's totals can never be served to another.
const recapCacheTTL = 5 * time.Minute

// recapCacheSize caps the map so a long-lived process cannot grow it without
// bound. Expired entries are dropped on write.
const recapCacheSize = 512

type recapCacheEntry struct {
	view    *recapView
	years   []int
	expires time.Time
}

var (
	recapCacheMu sync.Mutex
	recapCache   = make(map[string]recapCacheEntry)
)

// recapView is the template contract for pages/recap.html.
type recapView struct {
	Year       int
	InProgress bool
	HasData    bool
	Sparse     bool
	Summary    string
	SparseNote string

	Stats     []recapStat
	TopDrink  *checkins.RecapDrink
	TopBrand  *checkins.RecapStyle
	TopStyles []checkins.RecapStyle
	TopVenue  *checkins.RecapVenue

	Rated      int
	RatedLine  string
	RatingBars []recapBar

	Palate     []recapPalateRow
	PalateNote string

	Axes []recapBar
	// AxesNote explains a fingerprint that is still thin on data.
	AxesNote string
}

// recapStat is one cell of the totals row.
type recapStat struct {
	Value string
	Label string
}

// recapBar is one labelled horizontal bar. Width is a CSS percentage keyed off
// the largest value in its chart; Meta carries the number as text so the chart
// still reads without seeing the bar. Flag marks a bar whose sample is too thin
// to trust yet, in words rather than colour.
type recapBar struct {
	Label string
	Meta  string
	Width string
	Flag  bool
}

// recapPalateRow is one year of the palate comparison.
type recapPalateRow struct {
	Label string
	Value string
}

// recapPage renders one member's year in soda. The viewer is always the
// subject: there is no handle in the path and no way to read anyone else's.
func (s *Server) recapPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	now := time.Now().UTC()
	year, ok := parseRecapYear(r.PathValue("year"), now)
	if !ok {
		http.NotFound(w, r)
		return
	}

	view, years, err := s.loadRecap(r.Context(), u.ID, year, now)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	shareURL, shareError := s.shareNotice(r, u.ID)
	s.render(w, r, http.StatusOK, "recap", map[string]any{
		"Title":           fmt.Sprintf("%d in soda", year),
		"Recap":           view,
		"RecapYear":       year,
		"Years":           years,
		"CreatedShareURL": shareURL,
		"ShareError":      shareError,
	})
}

// parseRecapYear resolves the path year. An empty path means the current year;
// a non-numeric or out-of-window year is rejected so the handler can answer
// 404 instead of rendering an empty page that looks like a bug.
func parseRecapYear(raw string, now time.Time) (int, bool) {
	current := now.Year()
	if current < recapYearFloor {
		current = recapYearFloor
	}
	if raw == "" {
		return current, true
	}
	year, err := strconv.Atoi(raw)
	if err != nil || year < recapYearFloor || year > current {
		return 0, false
	}
	return year, true
}

// recapNudgeYear is the year the /you page points at, or 0 outside December.
// December is the only month with a finished year worth looking back on, so the
// pointer stays out of the way for the other eleven.
func recapNudgeYear(now time.Time) int {
	if now.Month() != time.December {
		return 0
	}
	return now.Year()
}

// loadRecap returns the view and the year switcher, from cache when fresh. The
// caller renders the normal error page when the service fails.
func (s *Server) loadRecap(ctx context.Context, userID string, year int, now time.Time) (*recapView, []int, error) {
	key := userID + ":" + strconv.Itoa(year)
	if cached, ok := recapCacheGet(key, now); ok {
		return cached.view, cached.years, nil
	}

	rc, err := s.Checkins.Recap(ctx, userID, year)
	if err != nil {
		return nil, nil, err
	}
	view := newRecapView(rc, now)
	years := s.recapYears(ctx, userID, now)
	recapCachePut(key, recapCacheEntry{view: view, years: years, expires: now.Add(recapCacheTTL)})
	return view, years, nil
}

func recapCacheGet(key string, now time.Time) (recapCacheEntry, bool) {
	recapCacheMu.Lock()
	defer recapCacheMu.Unlock()
	entry, ok := recapCache[key]
	if !ok || now.After(entry.expires) {
		if ok {
			delete(recapCache, key)
		}
		return recapCacheEntry{}, false
	}
	return entry, true
}

func recapCachePut(key string, entry recapCacheEntry) {
	recapCacheMu.Lock()
	defer recapCacheMu.Unlock()
	if len(recapCache) >= recapCacheSize {
		now := time.Now()
		for k, v := range recapCache {
			if now.After(v.expires) {
				delete(recapCache, k)
			}
		}
		if len(recapCache) >= recapCacheSize {
			// Everything left is still fresh; drop the lot rather than grow
			// without bound. A recap is cheap to rebuild.
			recapCache = make(map[string]recapCacheEntry)
		}
	}
	recapCache[key] = entry
}

// recapYears builds the year switcher: the current year back to the member's
// first check-in, capped at recapYearCap. It is navigation only, so a failed
// read falls back to the current year instead of failing the page.
func (s *Server) recapYears(ctx context.Context, userID string, now time.Time) []int {
	top := now.Year()
	first := top
	if s.DB != nil && s.DB.SQL != nil {
		var min sql.NullString
		err := s.DB.SQL.QueryRowContext(ctx,
			`SELECT MIN(created_at) FROM checkins WHERE user_id = ? AND deleted_at IS NULL`,
			userID).Scan(&min)
		if err == nil && min.Valid {
			if t, perr := time.Parse(time.RFC3339, min.String); perr == nil && t.Year() < first {
				first = t.Year()
			}
		}
	}
	if floor := top - recapYearCap + 1; first < floor {
		first = floor
	}
	years := make([]int, 0, recapYearCap)
	for y := top; y >= first; y-- {
		years = append(years, y)
	}
	return years
}

// newRecapView turns the service aggregate into template data. It is pure: no
// clock beyond "is this year still running", and no new aggregates.
func newRecapView(rc *checkins.Recap, now time.Time) *recapView {
	v := &recapView{
		Year:       rc.Year,
		InProgress: rc.Year == now.Year(),
		HasData:    rc.HasData,
		Sparse:     rc.Sparse,
		TopDrink:   rc.TopDrink,
		TopBrand:   rc.TopBrand,
		TopStyles:  rc.TopStyles,
		TopVenue:   rc.TopVenue,
		Stats:      recapStats(rc.Totals),
	}
	if rc.HasData {
		v.Summary = recapSummary(rc.Totals, rc.Year, v.InProgress)
	}
	if rc.Sparse {
		v.SparseNote = "A thin year. Everything logged is still counted here."
		if v.InProgress {
			v.SparseNote = "A thin year so far. This page fills in as you log more."
		}
	}

	rated, highest := recapBucketCounts(rc.RatingBuckets)
	v.Rated = rated
	for _, b := range rc.RatingBuckets {
		v.RatingBars = append(v.RatingBars, recapBar{
			Label: b.Label,
			Meta:  fmt.Sprintf("%d %s", b.Count, plural(b.Count, "check-in", "check-ins")),
			Width: recapWidth(b.Count, highest),
		})
	}
	if rated > 0 {
		v.RatedLine = recapRatedLine(rated, rc.Totals.AvgRating)
	}

	v.Palate, v.PalateNote = recapPalate(rc.Palate, rc.Year)
	v.Axes, v.AxesNote = recapAxes(rc.Fingerprint)
	return v
}

// recapStats is the totals row. Zero counters are dropped rather than shown as
// a wall of noughts; a longest streak of one day is not a streak worth a tile.
func recapStats(t checkins.RecapTotals) []recapStat {
	out := make([]recapStat, 0, 8)
	add := func(n int, one, many string) {
		if n > 0 {
			out = append(out, recapStat{Value: strconv.Itoa(n), Label: plural(n, one, many)})
		}
	}
	add(t.Checkins, "check-in", "check-ins")
	add(t.UniqueDrinks, "different drink", "different drinks")
	add(t.Styles, "style", "styles")
	add(t.NewStyles, "new style", "new styles")
	add(t.Venues, "venue", "venues")
	add(t.Cities, "city", "cities")
	add(t.Countries, "country", "countries")
	if t.LongestStreak > 1 {
		out = append(out, recapStat{Value: fmt.Sprintf("%d days", t.LongestStreak), Label: "longest streak"})
	}
	return out
}

// recapSummary is the one honest sentence under the heading. It only names
// numbers that exist: no venues are claimed for a year without any.
func recapSummary(t checkins.RecapTotals, year int, inProgress bool) string {
	if t.Checkins == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You logged %d %s %s", t.Checkins, plural(t.Checkins, "check-in", "check-ins"), recapWindow(year, inProgress))
	switch {
	case t.UniqueDrinks > 1 && t.Styles > 0:
		fmt.Fprintf(&b, " across %d drinks and %d %s", t.UniqueDrinks, t.Styles, plural(t.Styles, "style", "styles"))
	case t.UniqueDrinks > 1:
		fmt.Fprintf(&b, " across %d drinks", t.UniqueDrinks)
	case t.Styles > 0:
		fmt.Fprintf(&b, " across %d %s", t.Styles, plural(t.Styles, "style", "styles"))
	case t.Checkins > 1:
		b.WriteString(", all of them the same drink")
	}
	if t.NewStyles > 0 {
		if t.Styles > 0 {
			fmt.Fprintf(&b, ", %d of them new to you", t.NewStyles)
		} else {
			fmt.Fprintf(&b, ", %d %s new to you", t.NewStyles, plural(t.NewStyles, "style", "styles"))
		}
	}
	b.WriteString(".")
	return b.String()
}

// recapWindow is the temporal half of a sentence: a running year is "so far".
func recapWindow(year int, inProgress bool) string {
	if inProgress {
		return fmt.Sprintf("so far in %d", year)
	}
	return fmt.Sprintf("in %d", year)
}

// recapBucketCounts totals the rated check-ins and finds the tallest bar.
func recapBucketCounts(buckets []checkins.RecapRatingBucket) (total, highest int) {
	for _, b := range buckets {
		total += b.Count
		if b.Count > highest {
			highest = b.Count
		}
	}
	return total, highest
}

// recapWidth scales a count against the tallest bar in its chart.
func recapWidth(count, highest int) string {
	if highest <= 0 || count <= 0 {
		return "0%"
	}
	return strconv.Itoa(count*100/highest) + "%"
}

// recapRatedLine states the average in stars, the same scale quarter-star
// ratings use everywhere else. The service average is on the 0–20 scale.
func recapRatedLine(rated int, avg *float64) string {
	line := fmt.Sprintf("%d rated %s", rated, plural(rated, "check-in", "check-ins"))
	if avg == nil {
		return line + "."
	}
	stars := *avg / 4
	return line + ", averaging " + starsValue(&stars) + "."
}

// recapPalate renders the year-on-year average comparison. It says which way
// the number moved and nothing more; there is no second signal to interpret.
func recapPalate(rows []checkins.RecapPalate, year int) ([]recapPalateRow, string) {
	if len(rows) == 0 {
		return nil, fmt.Sprintf("Rated check-ins in both %d and %d are needed to compare.", year, year-1)
	}
	out := make([]recapPalateRow, 0, len(rows))
	note := ""
	for _, row := range rows {
		out = append(out, recapPalateRow{
			Label: strconv.Itoa(year),
			Value: recapStars(row.ThisYear),
		})
		out = append(out, recapPalateRow{
			Label: strconv.Itoa(year - 1),
			Value: recapStars(row.LastYear),
		})
		if row.ThisYear != nil && row.LastYear != nil {
			delta := *row.ThisYear - *row.LastYear
			switch {
			case math.Abs(delta) < 0.5:
				note = fmt.Sprintf("About the same as %d.", year-1)
			case delta > 0:
				note = fmt.Sprintf("Higher than %d.", year-1)
			default:
				note = fmt.Sprintf("Lower than %d.", year-1)
			}
		}
	}
	return out, note
}

// recapStars formats a 0–20 average on the star scale the rest of the app
// uses, or an honest dash when that year has no rated check-ins.
func recapStars(avg *float64) string {
	if avg == nil {
		return "No ratings"
	}
	stars := *avg / 4
	return starsValue(&stars)
}

// recapAxes turns the flavour fingerprint into labelled bars. Scores are
// normalised by the service (strongest axis 100), so the bar and the number
// agree with each other.
func recapAxes(fp *checkins.Fingerprint) ([]recapBar, string) {
	if fp == nil || !fp.HasData || len(fp.Axes) == 0 {
		return nil, ""
	}
	bars := make([]recapBar, 0, len(fp.Axes))
	thin := false
	for _, axis := range fp.Axes {
		if !axis.Confident {
			thin = true
		}
		bars = append(bars, recapBar{
			Label: axis.Label,
			Meta: fmt.Sprintf("%.0f · from %d %s",
				axis.Score, axis.Samples, plural(axis.Samples, "check-in", "check-ins")),
			Width: fmt.Sprintf("%.0f%%", axis.Score),
			Flag:  !axis.Confident,
		})
	}
	note := ""
	if thin {
		note = "A bar marked early rests on fewer than five check-ins, so it may move as you log more."
	}
	return bars, note
}

// plural picks the singular form for exactly one, which keeps counts reading
// like sentences rather than labels.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
