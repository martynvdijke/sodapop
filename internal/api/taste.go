package api

import (
	"net/http"

	"sodapop/internal/auth"
)

// tasteTwins ranks accepted friends by rating similarity for the token owner.
// It is personal: there is no endpoint that lists another user's twins.
func (s *Server) tasteTwins(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	twins, err := s.Taste.Twins(r.Context(), tok.User.ID, queryInt(r, "limit", 10, 20))
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(twins))
	for _, twin := range twins {
		out = append(out, map[string]any{
			"handle": twin.Handle, "display_name": twin.DisplayName,
			"score": twin.Score, "common_drinks": twin.Common,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}

// tasteRecommendations returns drinks loved by compatible friends that the
// viewer has not checked in yet.
func (s *Server) tasteRecommendations(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	recs, err := s.Taste.Recommendations(r.Context(), tok.User.ID, queryInt(r, "limit", 10, 10))
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		out = append(out, map[string]any{
			"drink_id": rec.DrinkID, "slug": rec.Slug, "name": rec.Name,
			"brand": rec.Brand, "style": rec.Style,
			"score": rec.Score, "reason": rec.Reason,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}
