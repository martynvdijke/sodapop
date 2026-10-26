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
		key, target := ruleTarget(st.Rule)
		have := progress[key]
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
			"awarded_at": st.AwardedAt, "progress": have, "target": target,
			"percent": percent,
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
		"stats":  stats,
		"badges": earned,
	}})
	return nil
}

// ruleTarget splits a seeded "key>=N" badge rule. Malformed rules yield an
// empty key and a zero target, which renders as no progress bar.
func ruleTarget(rule string) (string, int) {
	key, value, ok := strings.Cut(strings.TrimSpace(rule), ">=")
	if !ok {
		return "", 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return strings.TrimSpace(key), n
}
