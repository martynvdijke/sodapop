package api

import (
	"net/http"
	"strconv"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/adminnotes"
	"sodapop/internal/auth"
)

// listReportNotes returns internal notes attached to a report. Reports have no
// AdminNote enum member, so notes use entity="system" and the report id as the
// entity id. Non-admins see 404 rather than 403 to avoid confirming that an
// admin surface exists.
func (s *Server) listReportNotes(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !isAdmin(tok) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	id, ok := reportIDFromPath(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	if _, err := s.Auth.Client.Report.Get(r.Context(), id); err != nil {
		if ent.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return nil
		}
		return err
	}
	rows, err := adminnotes.New(s.Auth.Client).List(r.Context(), "system", id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.adminNoteDTOs(r, rows)})
	return nil
}

// createReportNote appends an internal note to a report.
func (s *Server) createReportNote(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !isAdmin(tok) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	id, ok := reportIDFromPath(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	if _, err := s.Auth.Client.Report.Get(r.Context(), id); err != nil {
		if ent.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return nil
		}
		return err
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	row, err := adminnotes.New(s.Auth.Client).Create(r.Context(), tok.User.ID, "system", id, in.Body)
	if err != nil {
		if err == adminnotes.ErrInvalid {
			return bad("note must be between 1 and 2000 characters")
		}
		return err
	}
	dto := map[string]any{
		"id":            row.ID,
		"author_handle": tok.User.Handle,
		"body":          row.Body,
		"created_at":    row.CreatedAt,
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": dto})
	return nil
}

// deleteAdminNote permanently deletes an internal note. Non-admins get 404.
func (s *Server) deleteAdminNote(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !isAdmin(tok) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	if err := adminnotes.New(s.Auth.Client).Delete(r.Context(), tok.User.ID, id, true); err != nil {
		if err == adminnotes.ErrNotFound || err == adminnotes.ErrForbidden {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return nil
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func reportIDFromPath(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (s *Server) adminNoteDTOs(r *http.Request, rows []*ent.AdminNote) []map[string]any {
	authorIDs := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !seen[row.AuthorID] {
			seen[row.AuthorID] = true
			authorIDs = append(authorIDs, row.AuthorID)
		}
	}
	handles := map[string]string{}
	if len(authorIDs) > 0 {
		if users, err := s.Auth.Client.User.Query().Where(user.IDIn(authorIDs...)).All(r.Context()); err == nil {
			for _, u := range users {
				handles[u.ID] = u.Handle
			}
		}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		handle := handles[row.AuthorID]
		if handle == "" {
			handle = "deleted user"
		}
		out = append(out, map[string]any{
			"id":            row.ID,
			"author_handle": handle,
			"body":          row.Body,
			"created_at":    row.CreatedAt,
		})
	}
	return out
}
