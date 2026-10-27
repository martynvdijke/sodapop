package api

import (
	"net/http"

	"sodapop/internal/auth"
	"sodapop/internal/comments"
)

// listComments returns the live comments on a check-in the viewer can see.
// An invisible parent is reported as 404 so the endpoint cannot be used to
// probe for check-in identifiers.
func (s *Server) listComments(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	limit := queryInt(r, "limit", 50, 100)
	before := r.URL.Query().Get("before")
	items, _, hasMore, err := s.Comments.ListPage(r.Context(), tok.User.ID, r.PathValue("id"), before, limit)
	if err != nil {
		return err
	}
	admin := isAdmin(tok)
	out := make([]map[string]any, 0, len(items))
	for _, c := range items {
		out = append(out, commentDTO(c, admin || c.UserID == tok.User.ID))
	}
	meta := map[string]any{"count": len(out), "has_more": hasMore}
	if hasMore && len(items) > 0 {
		meta["next_before"] = items[0].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out, "meta": meta})
	return nil
}

// createComment adds a comment to a visible check-in. The service applies the
// rune cap, the per-user hourly limit and the owner notification.
func (s *Server) createComment(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	c, err := s.Comments.Create(r.Context(), tok.User.ID, r.PathValue("id"), in.Body)
	if err != nil {
		return err
	}
	view := comments.Comment{
		ID:          c.ID,
		UserID:      c.UserID,
		Handle:      tok.User.Handle,
		DisplayName: tok.User.DisplayName,
		Body:        c.Body,
		CreatedAt:   c.CreatedAt,
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"data": commentDTO(view, true),
	})
	return nil
}

// deleteComment soft-deletes a comment authored by the token holder, or any
// comment when the token holder is an admin. Non-owners get 404 rather than 403
// so they cannot tell whether the comment exists.
func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Comments.Delete(r.Context(), tok.User.ID, isAdmin(tok), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func commentDTO(c comments.Comment, canDelete bool) map[string]any {
	return map[string]any{
		"id":         c.ID,
		"user":       map[string]any{"handle": c.Handle, "display_name": c.DisplayName},
		"body":       c.Body,
		"created_at": c.CreatedAt,
		"can_delete": canDelete,
		"user_id":    c.UserID,
	}
}
