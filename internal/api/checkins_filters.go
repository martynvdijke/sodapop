package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"sodapop/internal/checkins"
)

// parseCheckinFilters reads the shared filter query string for the personal and
// friend check-in routes. The returned filter still needs ViewerID/TargetID.
// Invalid values are rejected with a 400 rather than silently falling back, so
// a typo cannot quietly widen a query.
func parseCheckinFilters(r *http.Request) (checkins.ProfileCheckinsFilter, error) {
	q := r.URL.Query()
	f := checkins.ProfileCheckinsFilter{
		Limit:      queryInt(r, "limit", 20, 50),
		Before:     strings.TrimSpace(q.Get("before")),
		Visibility: strings.TrimSpace(q.Get("visibility")),
		Sort:       strings.TrimSpace(q.Get("sort")),
	}

	var err error
	if f.From, err = parseDateBound(q.Get("from"), false); err != nil {
		return f, err
	}
	if f.To, err = parseDateBound(q.Get("to"), true); err != nil {
		return f, err
	}
	if f.RatingMin, err = parseBoundedInt(q.Get("rating_min"), "rating_min", 0, 20); err != nil {
		return f, err
	}
	if f.RatingMax, err = parseBoundedInt(q.Get("rating_max"), "rating_max", 0, 20); err != nil {
		return f, err
	}
	if f.MinLikes, err = parseBoundedInt(q.Get("min_likes"), "min_likes", 0, 0); err != nil {
		return f, err
	}

	switch strings.ToLower(strings.TrimSpace(q.Get("has_photo"))) {
	case "", "0", "false":
	case "1", "true":
		f.HasPhoto = true
	default:
		return f, bad("has_photo must be 1 or 0")
	}

	switch f.Visibility {
	case "", "public", "friends", "private":
	default:
		return f, bad("visibility must be public, friends or private")
	}
	switch f.Sort {
	case "", "newest", "oldest", "rating_high", "rating_low", "likes":
	default:
		return f, bad("sort must be newest, oldest, rating_high, rating_low or likes")
	}

	if raw := strings.TrimSpace(q.Get("page")); raw != "" {
		page, err := strconv.Atoi(raw)
		if err != nil || page < 1 {
			return f, bad("page must be a positive integer")
		}
		f.Page = page
	}
	return f, nil
}

// parseBoundedInt parses an optional integer. When max is positive the value
// must not exceed it; otherwise only a negative value is rejected.
func parseBoundedInt(raw, name string, min, max int) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || (max > 0 && n > max) {
		if max > 0 {
			return nil, bad(name + " must be an integer between " + strconv.Itoa(min) + " and " + strconv.Itoa(max))
		}
		return nil, bad(name + " must be a non-negative integer")
	}
	return &n, nil
}

// parseDateBound normalises a YYYY-MM-DD date or an RFC3339 timestamp to an
// RFC3339 UTC string. A date-only "to" is advanced one day because the profile
// query treats To as exclusive: to=2026-01-02 includes all of 2 January.
func parseDateBound(raw string, end bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) == len("2006-01-02") {
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return "", bad("dates must be RFC3339 or YYYY-MM-DD")
		}
		if end {
			t = t.Add(24 * time.Hour)
		}
		return t.UTC().Format(time.RFC3339), nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", bad("dates must be RFC3339 or YYYY-MM-DD")
	}
	return t.UTC().Format(time.RFC3339), nil
}

// profileItemsAsItems converts the richer profile row into the existing stable
// personal check-in shape. The profile query now carries serving and venue_id
// so this conversion preserves the full pre-existing API contract.
func profileItemsAsItems(items []checkins.ProfileItem) []checkins.Item {
	out := make([]checkins.Item, 0, len(items))
	for _, it := range items {
		row := checkins.Item{
			ID:         it.ID,
			UserID:     it.UserID,
			DrinkID:    it.DrinkID,
			DrinkName:  it.DrinkName,
			DrinkSlug:  it.DrinkSlug,
			Brand:      it.Brand,
			Rating:     it.Rating,
			Serving:    it.Serving,
			PrepMethod: it.PrepMethod,
			PrepNote:   it.PrepNote,
			Note:       it.Note,
			VenueID:    it.VenueID,
			VenueName:  it.VenueName,
			AtHome:     it.AtHome,
			Visibility: it.Visibility,
			CreatedAt:  it.CreatedAt,
			PhotoID:    it.PhotoID,
		}
		if it.PhotoID != "" {
			row.PhotoURL = "/api/v1/media/" + it.PhotoID + "/thumb"
		}
		out = append(out, row)
	}
	return out
}

// checkinPageMeta builds the additive paging block. Cursor pages expose
// next_before; offset pages report the page number. Both report has_next.
func checkinPageMeta(res *checkins.ProfileCheckinsResult) map[string]any {
	meta := map[string]any{
		"count":    len(res.Items),
		"has_next": res.HasNext,
		"page":     res.Page,
	}
	if res.Mode == checkins.ProfileModeCursor && res.HasNext && len(res.Items) > 0 {
		last := res.Items[len(res.Items)-1]
		meta["next_before"] = checkins.Cursor(last.CreatedAt, last.ID)
	}
	return meta
}
