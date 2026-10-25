package api

import (
	"math"
	"net/http"

	"sodapop/internal/auth"
	"sodapop/internal/checkins"
)

// myInsights returns the caller's own nutrition window. There is no way to ask
// for another user's insights, which keeps the endpoint personal.
func (s *Server) myInsights(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	days := checkins.NormalizeInsightsDays(queryInt(r, "range", 30, 90))
	ins, err := s.Checkins.Insights(r.Context(), tok.User.ID, checkins.InsightsOptions{Days: days})
	if err != nil {
		return err
	}

	series := make([]map[string]any, 0, len(ins.Series))
	for _, p := range ins.Series {
		series = append(series, map[string]any{
			"date":          p.Date,
			"sugar_g":       round2(p.SugarG),
			"caffeine_mg":   round2(p.CaffeineMg),
			"calories_kcal": p.CaloriesKcal,
			"pours":         p.Pours,
		})
	}
	top := make([]map[string]any, 0, len(ins.TopDrinks))
	for _, d := range ins.TopDrinks {
		top = append(top, map[string]any{
			"slug":        d.Slug,
			"name":        d.Name,
			"brand":       d.Brand,
			"pours":       d.Pours,
			"sugar_g":     round2(d.SugarG),
			"caffeine_mg": round2(d.CaffeineMg),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"range_days":              ins.Days,
		"from":                    ins.From,
		"to":                      ins.To,
		"pours":                   ins.Pours,
		"sugar_g":                 round2(ins.SugarG),
		"caffeine_mg":             round2(ins.CaffeineMg),
		"calories_kcal":           ins.CaloriesKcal,
		"avg_sugar_per_day_g":     round2(ins.AvgSugarPerDay),
		"avg_caffeine_per_day_mg": round2(ins.AvgCaffeinePerDay),
		"series":                  series,
		"top_drinks":              top,
		"bedtime":                 ins.Bedtime,
		"last_caffeine_at":        ins.LastCaffeineAt,
		"caffeine_clear_by":       ins.CaffeineClearBy,
		"late_caffeine":           ins.LateCaffeine,
	}})
	return nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
