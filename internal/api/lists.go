package api

import (
	"net/http"

	"sodapop/internal/auth"
)

// myLists returns the viewer's own lists, including the system wishlist, as
// summaries with an item count. It never lists another user's lists.
func (s *Server) myLists(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	lists, err := s.Checkins.ListsForOwner(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(lists))
	for _, l := range lists {
		out = append(out, map[string]any{
			"id": l.ID, "name": l.Name, "description": l.Description,
			"visibility": l.Visibility, "is_system": l.IsSystem, "item_count": l.ItemCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}

// getList returns one visibility-scoped list with its newest items. Missing,
// invisible and blocked-owner lists all return the same 404.
func (s *Server) getList(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	l, err := s.Checkins.ListForViewer(r.Context(), tok.User.ID, r.PathValue("id"))
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(l.Items))
	for _, it := range l.Items {
		items = append(items, map[string]any{
			"drink_id": it.DrinkID, "slug": it.Slug, "name": it.Name,
			"brand": it.Brand, "note": it.Note, "created_at": it.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"id": l.ID, "name": l.Name, "description": l.Description,
		"visibility": l.Visibility, "is_system": l.IsSystem,
		"item_count": l.ItemCount, "items": items,
	}})
	return nil
}
