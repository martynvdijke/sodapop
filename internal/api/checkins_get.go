package api

import (
	"errors"
	"net/http"

	"sodapop/internal/auth"
	"sodapop/internal/checkins"
)

func (s *Server) getCheckin(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	detail, err := s.Checkins.Get(r.Context(), tok.User.ID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, checkins.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "checkin not found")
			return nil
		}
		return err
	}

	data := map[string]any{
		"id": detail.ID,
		"user": map[string]any{
			"handle":       detail.UserHandle,
			"display_name": detail.UserName,
		},
		"drink": map[string]any{
			"slug":  detail.DrinkSlug,
			"name":  detail.DrinkName,
			"brand": detail.Brand,
			"style": detail.Style,
		},
		"rating":         detail.Rating,
		"note":           detail.Note,
		"visibility":     detail.Visibility,
		"toasts":         detail.Toasts,
		"toasted":        detail.Toasted,
		"comments_count": detail.CommentsCount,
		"can_delete":     tok.User.ID == detail.UserID && tok.HasScope("write"),
		"can_toast":      tok.User.ID != detail.UserID && !detail.Toasted && tok.HasScope("write") && tok.User.EmailVerifiedAt != nil,
		"can_comment":    tok.HasScope("write") && tok.User.EmailVerifiedAt != nil,
		"created_at":     detail.CreatedAt,
	}
	if detail.VenueID != nil {
		data["venue"] = map[string]any{"id": *detail.VenueID, "name": detail.VenueName}
	}
	if detail.MediaID != "" {
		data["photo"] = map[string]any{"id": detail.MediaID, "mime": detail.MediaMime}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
	return nil
}
