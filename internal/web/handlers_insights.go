package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"sodapop/internal/checkins"
)

// insightsPage renders the personal sugar/caffeine/calorie summary.
func (s *Server) insightsPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	days := 30
	if raw := r.URL.Query().Get("range"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			days = checkins.NormalizeInsightsDays(n)
		}
	}

	bedtime := ""
	if prefs, err := s.Auth.GetOrCreateSettings(r.Context(), u.ID); err == nil {
		bedtime = prefs.CaffeineBedtime
	}

	ins, err := s.Checkins.Insights(r.Context(), u.ID, checkins.InsightsOptions{
		Days:    days,
		Bedtime: bedtime,
		Now:     time.Now(),
	})
	if err != nil {
		s.Log.Error("insights failed", "err", err)
		http.Error(w, "could not load insights", http.StatusInternalServerError)
		return
	}

	lastCaffeine := ""
	if t, err := time.Parse(time.RFC3339, ins.LastCaffeineAt); err == nil {
		lastCaffeine = t.Local().Format("Mon 2 Jan, 15:04")
	}
	clearBy := ""
	if t, err := time.Parse(time.RFC3339, ins.CaffeineClearBy); err == nil {
		clearBy = t.Local().Format("Mon 2 Jan, 15:04")
	}

	s.render(w, r, http.StatusOK, "insights", map[string]any{
		"Title":  "Insights",
		"Range":  ins.Days,
		"Ranges": []int{7, 30, 90},
		"Sugar": map[string]any{
			"TotalG":    fmt.Sprintf("%.1f", ins.SugarG),
			"AvgPerDay": fmt.Sprintf("%.1f", ins.AvgSugarPerDay),
			"Series":    ins.Series,
		},
		"Caffeine": map[string]any{
			"TotalMg":        fmt.Sprintf("%.1f", ins.CaffeineMg),
			"Series":         ins.Series,
			"Bedtime":        ins.Bedtime,
			"LastCaffeineAt": lastCaffeine,
			"ClearBy":        clearBy,
			"LateCaffeine":   ins.LateCaffeine,
		},
		"Calories": map[string]any{
			"TotalKcal": ins.CaloriesKcal,
		},
		"TopDrinks": ins.TopDrinks,
		"Empty":     ins.Pours == 0,
		"Pours":     ins.Pours,
	})
}
