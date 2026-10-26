package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"sodapop/ent/adminnote"
	"sodapop/ent/report"
	"sodapop/ent/user"
)

func TestAdminReportNotesWeb(t *testing.T) {
	h := newV12Harness(t)
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	ctx := context.Background()
	rep, err := h.db.Client.Report.Create().
		SetEntity(report.EntityDrink).SetEntityID(1).SetUserID(admin.ID).
		SetReason("other").Save(ctx)
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	form := url.Values{"csrf_token": {"test-csrf"}, "body": {"  needs a label check  "}}
	req := h.request(http.MethodPost, "/admin/reports/"+strconv.Itoa(rep.ID)+"/notes", admin, form)
	req.SetPathValue("id", strconv.Itoa(rep.ID))
	rec := httptest.NewRecorder()
	h.srv.requireAdmin(h.srv.adminReportNoteCreate)(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("create status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err := h.db.Client.AdminNote.Query().
		Where(adminnote.EntityEQ(adminnote.EntitySystem), adminnote.EntityIDEQ(rep.ID)).
		Only(ctx)
	if err != nil {
		t.Fatalf("note row: %v", err)
	}
	if row.Body != "needs a label check" || row.AuthorID != admin.ID {
		t.Fatalf("note row = %+v", row)
	}

	notes := h.srv.reportNotes(req, []int{rep.ID})
	if len(notes[rep.ID]) != 1 {
		t.Fatalf("report notes = %+v", notes)
	}
	note := notes[rep.ID][0]
	if note.AuthorHandle != "admin" || note.Body != "needs a label check" || note.CreatedAt == "" {
		t.Fatalf("note view = %+v", note)
	}

	// Invalid bodies are rejected without creating another row.
	req = h.request(http.MethodPost, "/admin/reports/"+strconv.Itoa(rep.ID)+"/notes", admin,
		url.Values{"csrf_token": {"test-csrf"}, "body": {"   "}})
	req.SetPathValue("id", strconv.Itoa(rep.ID))
	rec = httptest.NewRecorder()
	h.srv.requireAdmin(h.srv.adminReportNoteCreate)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty body status = %d, want 400", rec.Code)
	}

	req = h.request(http.MethodPost, "/admin/notes/"+strconv.Itoa(row.ID)+"/delete", admin, h.form())
	req.SetPathValue("id", strconv.Itoa(row.ID))
	rec = httptest.NewRecorder()
	h.srv.requireAdmin(h.srv.adminNoteDelete)(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("delete status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if n, err := h.db.Client.AdminNote.Query().Count(ctx); err != nil || n != 0 {
		t.Fatalf("notes after delete = %d, %v; want 0", n, err)
	}
}
