package web

import (
	"errors"
	"net/http"
	"strconv"

	"sodapop/ent"
	"sodapop/ent/adminnote"
	"sodapop/ent/user"
	"sodapop/internal/adminnotes"
)

// adminReportNoteCreate appends an internal note to a moderation report. Admin
// notes have no "report" enum member, so report notes use entity="system" and
// the report id as the entity id.
func (s *Server) adminReportNoteCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	if _, err := s.Auth.Client.Report.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	u := UserFromContext(r)
	if _, err := adminnotes.New(s.Auth.Client).Create(ctx, u.ID, "system", id, r.FormValue("body")); err != nil {
		if errors.Is(err, adminnotes.ErrInvalid) {
			http.Error(w, "note must be between 1 and 2000 characters", http.StatusBadRequest)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// adminNoteDelete permanently removes an internal note. The route is wrapped
// in requireAdmin, so the actor may delete any note.
func (s *Server) adminNoteDelete(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	u := UserFromContext(r)
	if err := adminnotes.New(s.Auth.Client).Delete(r.Context(), u.ID, id, true); err != nil {
		switch {
		case errors.Is(err, adminnotes.ErrNotFound):
			http.NotFound(w, r)
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// reportNotes returns report-scoped notes grouped by report id, including the
// author handle needed by the admin template.
func (s *Server) reportNotes(r *http.Request, reportIDs []int) map[int][]adminNoteView {
	out := map[int][]adminNoteView{}
	if len(reportIDs) == 0 {
		return out
	}
	ctx := r.Context()
	rows, err := s.Auth.Client.AdminNote.Query().
		Where(adminnote.EntityEQ(adminnote.EntitySystem), adminnote.EntityIDIn(reportIDs...)).
		Order(adminnote.ByCreatedAt(), adminnote.ByID()).
		All(ctx)
	if err != nil {
		return out
	}
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
		if users, err := s.Auth.Client.User.Query().Where(user.IDIn(authorIDs...)).All(ctx); err == nil {
			for _, u := range users {
				handles[u.ID] = u.Handle
			}
		}
	}
	for _, row := range rows {
		handle := handles[row.AuthorID]
		if handle == "" {
			handle = "deleted user"
		}
		out[row.EntityID] = append(out[row.EntityID], adminNoteView{
			ID: row.ID, AuthorHandle: handle, Body: row.Body, CreatedAt: row.CreatedAt,
		})
	}
	return out
}
