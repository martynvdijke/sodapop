package api

import (
	"errors"
	"net/http"

	"sodapop/internal/auth"
	"sodapop/internal/checkins"
	"sodapop/internal/saves"
)

// bookmarkCheckin saves a visible check-in for the token holder. The name is
// distinct from saveCheckin, which writes a check-in from the create form.
func (s *Server) bookmarkCheckin(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Saves.Save(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		if errors.Is(err, saves.ErrInvalid) {
			return bad("invalid check-in")
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// unbookmarkCheckin removes a save. Removing an absent save succeeds.
func (s *Server) unbookmarkCheckin(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Saves.Unsave(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		if errors.Is(err, saves.ErrInvalid) {
			return bad("invalid check-in")
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// listSaved returns the viewer's bookmarked check-ins, newest save first, using
// the same DTO as GET /api/v1/checkins/{id}. There is no total.
func (s *Server) listSaved(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	ids, hasMore, nextBefore, err := s.Saves.ListIDs(
		r.Context(), tok.User.ID, queryInt(r, "limit", 20, 50), r.URL.Query().Get("before"))
	if err != nil {
		if errors.Is(err, saves.ErrInvalid) {
			return bad("invalid cursor")
		}
		return err
	}

	items := make([]map[string]any, 0, len(ids))
	if len(ids) > 0 {
		// ByIDs confirms the rows still exist and preserves save order; the
		// detail load supplies the full check-in DTO shape.
		feed, err := s.Social.ByIDs(r.Context(), tok.User.ID, ids)
		if err != nil {
			return err
		}
		for _, item := range feed {
			detail, err := s.Checkins.Get(r.Context(), tok.User.ID, item.ID)
			if err != nil {
				if errors.Is(err, checkins.ErrNotFound) {
					continue
				}
				return err
			}
			data := checkinDTO(detail, tok)
			data["saved"] = true
			items = append(items, data)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": items,
		"meta": map[string]any{
			"count":       len(items),
			"has_more":    hasMore,
			"next_before": nextBefore,
		},
	})
	return nil
}
