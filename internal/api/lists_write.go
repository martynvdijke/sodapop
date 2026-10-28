package api

import (
	"net/http"
	"strconv"
	"time"

	"sodapop/internal/auth"
	"sodapop/internal/checkins"
)

// listWriteLimit bounds list mutations per user. Browsing is untouched.
const listWriteLimit = 60 * time.Hour

// allowListWrite applies the per-user write budget shared by every list
// mutation. It returns false after writing a 429 response.
func (s *Server) allowListWrite(w http.ResponseWriter, tok *auth.APIToken) bool {
	if s.Limiter == nil || s.Limiter.Allow("listwrite:"+tok.User.ID, 60, listWriteLimit) {
		return true
	}
	w.Header().Set("Retry-After", "3600")
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many list changes; try again later")
	return false
}

func listSummaryDTO(l *checkins.ListSummary) map[string]any {
	return map[string]any{
		"id": l.ID, "name": l.Name, "description": l.Description,
		"visibility": l.Visibility, "is_system": l.IsSystem, "item_count": l.ItemCount,
	}
}

func listItemDTO(it *checkins.ListItem) map[string]any {
	return map[string]any{
		"drink_id": it.DrinkID, "slug": it.Slug, "name": it.Name,
		"brand": it.Brand, "note": it.Note, "created_at": it.CreatedAt,
	}
}

// createList makes an owner-scoped list. The system wishlist is created at
// signup, not here.
func (s *Server) createList(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !s.allowListWrite(w, tok) {
		return nil
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	l, err := s.Checkins.CreateList(r.Context(), tok.User.ID, checkins.ListCreateInput{
		Name: in.Name, Description: in.Description, Visibility: in.Visibility,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": listSummaryDTO(l)})
	return nil
}

// updateList patches an owned list. System lists reject edits with 400 while
// foreign or missing lists stay indistinguishable 404s.
func (s *Server) updateList(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !s.allowListWrite(w, tok) {
		return nil
	}
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Visibility  *string `json:"visibility"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	l, err := s.Checkins.UpdateList(r.Context(), tok.User.ID, r.PathValue("id"), checkins.ListUpdateInput{
		Name: in.Name, Description: in.Description, Visibility: in.Visibility,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": listSummaryDTO(l)})
	return nil
}

// deleteList removes an owned, non-system list and its items.
func (s *Server) deleteList(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !s.allowListWrite(w, tok) {
		return nil
	}
	if err := s.Checkins.DeleteList(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// addListItem saves a drink by id or slug. Re-adding an existing drink returns
// 200 with the original item instead of creating a duplicate.
func (s *Server) addListItem(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !s.allowListWrite(w, tok) {
		return nil
	}
	var in struct {
		DrinkID int    `json:"drink_id"`
		Slug    string `json:"slug"`
		Note    string `json:"note"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	item, created, err := s.Checkins.AddListItem(r.Context(), tok.User.ID, r.PathValue("id"), checkins.ListItemInput{
		DrinkID: in.DrinkID, Slug: in.Slug, Note: in.Note,
	})
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"data": listItemDTO(item)})
	return nil
}

// removeListItem deletes one drink from an owned list. Removing an absent item
// still succeeds so retries are safe.
func (s *Server) removeListItem(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !s.allowListWrite(w, tok) {
		return nil
	}
	drinkID, err := strconv.Atoi(r.PathValue("drink_id"))
	if err != nil || drinkID <= 0 {
		return bad("invalid drink id")
	}
	if err := s.Checkins.RemoveListItem(r.Context(), tok.User.ID, r.PathValue("id"), drinkID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
