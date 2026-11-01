package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/report"
	"sodapop/ent/session"
	"sodapop/ent/user"
)

// reportUserAndCheckin seeds the two report kinds the app can now file but the
// web queue could not act on before.
func (h *v12Harness) reportUserAndCheckin(t *testing.T) (*ent.User, *ent.Checkin, *ent.Report, *ent.Report) {
	t.Helper()
	ctx := context.Background()
	owner := h.verifiedUser(t, "owner", user.RoleUser)
	reporter := h.verifiedUser(t, "reporter", user.RoleUser)
	ci := h.createCheckin(t, owner)

	checkinReport, err := h.db.Client.Report.Create().
		SetEntity(report.EntityCheckin).SetEntityID(0).SetEntityRef(ci.ID).SetUserID(reporter.ID).
		SetReason("off").Save(ctx)
	if err != nil {
		t.Fatalf("checkin report: %v", err)
	}
	userReport, err := h.db.Client.Report.Create().
		SetEntity(report.EntityUser).SetEntityID(0).SetEntityRef(owner.Handle).SetUserID(reporter.ID).
		SetReason("harassment").Save(ctx)
	if err != nil {
		t.Fatalf("user report: %v", err)
	}
	return owner, ci, checkinReport, userReport
}

func (h *v12Harness) postReportAction(t *testing.T, admin *ent.User, id int, action string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	if form == nil {
		form = h.form()
	}
	req := h.request(http.MethodPost, "/admin/reports/"+strconv.Itoa(id)+"/"+action, admin, form)
	req.SetPathValue("id", strconv.Itoa(id))
	rec := httptest.NewRecorder()
	switch action {
	case "edit":
		h.srv.requireAdmin(h.srv.adminReportEdit)(rec, req)
	case "suspend":
		h.srv.requireAdmin(h.srv.adminReportSuspend)(rec, req)
	default:
		t.Fatalf("unknown action %q", action)
	}
	return rec
}

func TestReportSubmitAcceptsCheckinsAndUsers(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	owner := h.verifiedUser(t, "owner", user.RoleUser)
	reporter := h.verifiedUser(t, "reporter", user.RoleUser)
	ci := h.createCheckin(t, owner)

	rec := h.submitReport(t, reporter, url.Values{
		"entity": {"checkin"}, "entity_ref": {ci.ID}, "reason": {"off"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("checkin report status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err := h.db.Client.Report.Query().
		Where(report.EntityEQ(report.EntityCheckin)).Only(ctx)
	if err != nil {
		t.Fatalf("checkin report row: %v", err)
	}
	if row.EntityRef != ci.ID || row.EntityID != 0 {
		t.Fatalf("checkin report ref=%q id=%d, want ref=%q id=0", row.EntityRef, row.EntityID, ci.ID)
	}

	rec = h.submitReport(t, reporter, url.Values{
		"entity": {"user"}, "entity_ref": {owner.Handle}, "reason": {"harassment"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("user report status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err = h.db.Client.Report.Query().
		Where(report.EntityEQ(report.EntityUser)).Only(ctx)
	if err != nil {
		t.Fatalf("user report row: %v", err)
	}
	if row.EntityRef != owner.Handle {
		t.Fatalf("user report ref=%q, want %q", row.EntityRef, owner.Handle)
	}
}

func TestReportSubmitRejectsInvisibleCheckinAndSelfReport(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	owner := h.verifiedUser(t, "owner", user.RoleUser)
	reporter := h.verifiedUser(t, "reporter", user.RoleUser)
	drink, err := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	private, err := h.db.Client.Checkin.Create().
		SetUserID(owner.ID).SetDrinkID(drink.ID).
		SetVisibility(checkin.VisibilityPrivate).Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}

	before := h.reportCount(t)
	rec := h.submitReport(t, reporter, url.Values{
		"entity": {"checkin"}, "entity_ref": {private.ID},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("private checkin status = %d, want 404", rec.Code)
	}
	rec = h.submitReport(t, reporter, url.Values{
		"entity": {"user"}, "entity_ref": {reporter.Handle},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("self report status = %d, want 404", rec.Code)
	}
	rec = h.submitReport(t, reporter, url.Values{
		"entity": {"user"}, "entity_ref": {"nobody-here"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown handle status = %d, want 404", rec.Code)
	}
	if after := h.reportCount(t); after != before {
		t.Fatalf("reports created = %d, want 0", after-before)
	}
}

func TestAdminReportRemoveCheckinSoftDeletes(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	_, ci, checkinReport, _ := h.reportUserAndCheckin(t)

	rec := h.adminRemoveRequest(t, admin, checkinReport.ID)
	if rec.Code != http.StatusFound {
		t.Fatalf("remove status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err := h.db.Client.Checkin.Get(ctx, ci.ID)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if row.DeletedAt == nil {
		t.Fatal("checkin was not soft-deleted")
	}
	rep, err := h.db.Client.Report.Get(ctx, checkinReport.ID)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if rep.Status != report.StatusResolved {
		t.Fatalf("report status = %s, want resolved", rep.Status)
	}
}

func TestAdminReportEditRewritesCommentAndNote(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	_, ci, _, _ := h.reportUserAndCheckin(t)

	cm, err := h.srv.commentService().Create(ctx, ci.UserID, ci.ID, "Original comment")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	commentReport, err := h.db.Client.Report.Create().
		SetEntity(report.EntityComment).SetEntityID(0).SetEntityRef(cm.ID).SetUserID(admin.ID).
		SetReason("off").Save(ctx)
	if err != nil {
		t.Fatalf("comment report: %v", err)
	}

	rec := h.postReportAction(t, admin, commentReport.ID, "edit", url.Values{
		"csrf_token": {"test-csrf"}, "body": {"  Edited comment  "},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("comment edit status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	edited, err := h.db.Client.Comment.Get(ctx, cm.ID)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if edited.Body != "Edited comment" {
		t.Fatalf("comment body = %q, want trimmed replacement", edited.Body)
	}

	noteReport, err := h.db.Client.Report.Create().
		SetEntity(report.EntityCheckin).SetEntityID(0).SetEntityRef(ci.ID).SetUserID(admin.ID).
		SetReason("off").Save(ctx)
	if err != nil {
		t.Fatalf("note report: %v", err)
	}
	rec = h.postReportAction(t, admin, noteReport.ID, "edit", url.Values{
		"csrf_token": {"test-csrf"}, "body": {"Rewritten note"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("note edit status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	editedCI, err := h.db.Client.Checkin.Get(ctx, ci.ID)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if editedCI.Note != "Rewritten note" {
		t.Fatalf("checkin note = %q, want rewritten note", editedCI.Note)
	}
}

// A note is optional, so clearing it is a legitimate edit; a comment body is
// not, which EditAsModerator rejects before the queue sees a redirect.
func TestAdminReportEditCanClearNoteButNotComment(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	_, ci, checkinReport, _ := h.reportUserAndCheckin(t)

	if _, err := ci.Update().SetNote("Original note").Save(ctx); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	rec := h.postReportAction(t, admin, checkinReport.ID, "edit", url.Values{
		"csrf_token": {"test-csrf"}, "body": {"   "},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("empty edit status = %d, want redirect", rec.Code)
	}
	row, err := h.db.Client.Checkin.Get(ctx, ci.ID)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if row.Note != "" {
		t.Fatalf("note = %q, want cleared", row.Note)
	}

	cm, err := h.srv.commentService().Create(ctx, ci.UserID, ci.ID, "Keep me")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	commentReport, err := h.db.Client.Report.Create().
		SetEntity(report.EntityComment).SetEntityID(0).SetEntityRef(cm.ID).SetUserID(admin.ID).
		SetReason("off").Save(ctx)
	if err != nil {
		t.Fatalf("comment report: %v", err)
	}
	rec = h.postReportAction(t, admin, commentReport.ID, "edit", url.Values{
		"csrf_token": {"test-csrf"}, "body": {"   "},
	})
	if loc := rec.Header().Get("Location"); loc != "/admin?error=invalid" {
		t.Fatalf("empty comment edit redirected to %q, want /admin?error=invalid", loc)
	}
	still, err := h.db.Client.Comment.Get(ctx, cm.ID)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if still.Body != "Keep me" {
		t.Fatalf("comment body = %q, want unchanged", still.Body)
	}
}

func TestAdminReportSuspendRevokesAccess(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	owner, _, _, userReport := h.reportUserAndCheckin(t)

	if _, err := h.srv.Auth.CreateSession(ctx, owner.ID, "127.0.0.1", "test"); err != nil {
		t.Fatalf("session: %v", err)
	}

	rec := h.postReportAction(t, admin, userReport.ID, "suspend", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("suspend status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	suspended, err := h.db.Client.User.Get(ctx, owner.ID)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if suspended.Status != user.StatusSuspended {
		t.Fatalf("status = %s, want suspended", suspended.Status)
	}
	sessions, err := h.db.Client.Session.Query().Where(session.UserID(owner.ID)).Count(ctx)
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("sessions = %d, want 0", sessions)
	}
	rep, err := h.db.Client.Report.Get(ctx, userReport.ID)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if rep.Status != report.StatusResolved {
		t.Fatalf("report status = %s, want resolved", rep.Status)
	}
}
