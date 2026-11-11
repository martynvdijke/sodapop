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

	saved, err := s.Saves.IsSaved(r.Context(), tok.User.ID, detail.ID)
	if err != nil {
		return err
	}

	data := checkinDTO(detail, tok)
	data["saved"] = saved
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
	return nil
}

// checkinDTO is the single check-in shape shared by the detail endpoint and
// the personal saved list. Keeping it in one place stops the two responses
// drifting apart.
func checkinDTO(detail *checkins.Detail, tok *auth.APIToken) map[string]any {
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
		"prep_method":    detail.PrepMethod,
		"prep_note":      detail.PrepNote,
		"note":           detail.Note,
		"visibility":     detail.Visibility,
		"at_home":        detail.AtHome,
		"tagged_friends": taggedFriendDTOs(detail.TaggedFriends),
		"toasts":         detail.Toasts,
		"toasted":        detail.Toasted,
		"comments_count": detail.CommentsCount,
		"can_delete":     tok.User.ID == detail.UserID && tok.HasScope("write"),
		"can_toast":      tok.User.ID != detail.UserID && !detail.Toasted && tok.HasScope("write") && tok.User.EmailVerifiedAt != nil,
		"can_comment":    tok.HasScope("write") && tok.User.EmailVerifiedAt != nil,
		"created_at":     detail.CreatedAt,
		"price_cents":    detail.PriceCents,
		"price_currency": detail.PriceCurrency,
	}
	if detail.EditedAt != "" {
		data["edited_at"] = detail.EditedAt
	}
	if detail.VenueID != nil {
		data["venue"] = map[string]any{"id": *detail.VenueID, "name": detail.VenueName}
	}
	if detail.MediaID != "" {
		data["photo"] = map[string]any{"id": detail.MediaID, "mime": detail.MediaMime}
	}
	return data
}

// taggedFriendDTOs always returns a non-nil slice so the JSON contract is an
// empty array rather than null when nobody was tagged.
func taggedFriendDTOs(friends []checkins.TaggedFriend) []map[string]any {
	out := make([]map[string]any, 0, len(friends))
	for _, f := range friends {
		out = append(out, map[string]any{
			"id": f.ID, "handle": f.Handle, "display_name": f.DisplayName,
		})
	}
	return out
}
