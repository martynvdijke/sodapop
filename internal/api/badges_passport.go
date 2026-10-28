package api

import (
	"net/http"
	"strconv"
	"strings"

	"sodapop/internal/auth"
)

// badges returns every badge definition with the viewer's earned state and rule
// progress. It is personal and never exposes another user's progress.
func (s *Server) badges(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	statuses, err := s.Badges.ListForUser(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	progress, err := s.Badges.Progress(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(statuses))
	for _, st := range statuses {
		key, thresholds := ruleTarget(st.Rule)
		have := progress[key]
		target := nextTarget(have, thresholds)
		percent := 0
		if target > 0 {
			percent = have * 100 / target
			if percent > 100 {
				percent = 100
			}
		}
		out = append(out, map[string]any{
			"slug": st.Slug, "name": st.Name, "description": st.Description,
			"kind": st.Kind, "earned": st.Earned, "level": st.Level,
			"thresholds": thresholds, "awarded_at": st.AwardedAt,
			"progress": have, "target": target, "percent": percent,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}

// passport returns the viewer's own lifetime stats plus earned badges. No
// endpoint exposes another user's passport.
func (s *Server) passport(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	stats, err := s.Checkins.Stats(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	statuses, err := s.Badges.ListForUser(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	stamps, err := s.Checkins.Passport(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	earned := make([]map[string]any, 0, len(statuses))
	for _, st := range statuses {
		if !st.Earned {
			continue
		}
		earned = append(earned, map[string]any{
			"slug": st.Slug, "name": st.Name, "description": st.Description,
			"awarded_at": st.AwardedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"stats":       stats,
		"badges":      earned,
		"totals":      stamps.Totals,
		"styles":      stamps.Styles,
		"venue_kinds": stamps.VenueKinds,
		"countries":   stamps.Countries,
		"recent":      stamps.Recent,
	}})
	return nil
}

// ruleTarget splits a seeded badge rule into its counter key and ascending
// thresholds. Malformed rules yield an empty key and no thresholds, which
// renders as no progress bar.
func ruleTarget(rule string) (string, []int) {
	key, value, ok := strings.Cut(strings.TrimSpace(rule), ">=")
	if !ok {
		return "", nil
	}
	key = strings.TrimSpace(key)
	var thresholds []int
	for _, part := range strings.Split(value, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 {
			return key, nil
		}
		if len(thresholds) > 0 && n <= thresholds[len(thresholds)-1] {
			return key, nil
		}
		thresholds = append(thresholds, n)
	}
	return key, thresholds
}

// nextTarget returns the first threshold the counter has not reached so locked
// badges show progress toward the next level. Once every threshold is met the
// last one is used, capping the bar at 100%.
func nextTarget(have int, thresholds []int) int {
	if len(thresholds) == 0 {
		return 0
	}
	for _, need := range thresholds {
		if have < need {
			return need
		}
	}
	return thresholds[len(thresholds)-1]
}
