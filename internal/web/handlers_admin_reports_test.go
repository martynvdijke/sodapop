package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/report"
	"sodapop/ent/user"
)

func (h *v12Harness) verifiedUser(t *testing.T, handle string, role user.Role) *ent.User {
	t.Helper()
	u, err := h.db.Client.User.Create().
		SetHandle(handle).SetEmail(handle + "@example.com").
		SetDisplayName("Test " + handle).SetRole(role).
		SetEmailVerifiedAt(time.Now().UTC().Format(time.RFC3339)).
		Save(context.Background())
	if err != nil {
		t.Fatalf("verified user %s: %v", handle, err)
	}
	return u
}

func (h *v12Harness) createCheckin(t *testing.T, owner *ent.User) *ent.Checkin {
	t.Helper()
	drink, err := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(context.Background())
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	c, err := h.db.Client.Checkin.Create().
		SetUserID(owner.ID).SetDrinkID(drink.ID).
		SetVisibility(checkin.VisibilityPublic).
		Save(context.Background())
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func (h *v12Harness) submitReport(t *testing.T, u *ent.User, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	if form.Get("csrf_token") == "" {
		form.Set("csrf_token", "test-csrf")
	}
	req := h.request(http.MethodPost, "/reports", u, form)
	rec := httptest.NewRecorder()
	h.srv.reportSubmit(rec, req)
	return rec
}

func (h *v12Harness) reportCount(t *testing.T) int {
	t.Helper()
	n, err := h.db.Client.Report.Query().Count(context.Background())
	if err != nil {
		t.Fatalf("report count: %v", err)
	}
	return n
}

func TestReportSubmitStringRefs(t *testing.T) {
	h := newV12Harness(t)
	owner := h.verifiedUser(t, "owner", user.RoleUser)
	reporter := h.verifiedUser(t, "reporter", user.RoleUser)
	ci := h.createCheckin(t, owner)
	cm, err := h.srv.commentService().Create(context.Background(), owner.ID, ci.ID, "Needs moderation")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	venue, err := h.db.Client.Venue.Create().SetName("Corner Shop").SetSlug("corner-shop").Save(context.Background())
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	vm, err := h.db.Client.VenueMedia.Create().
		SetVenueID(venue.ID).SetMediaID("media-1").SetKind("photo").
		SetLabel("Patio").SetUploadedBy(owner.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("venue media: %v", err)
	}

	rec := h.submitReport(t, reporter, url.Values{
		"entity": {"comment"}, "entity_ref": {cm.ID}, "reason": {"spam"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("comment report status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err := h.db.Client.Report.Query().
		Where(report.EntityEQ(report.EntityComment)).Only(context.Background())
	if err != nil {
		t.Fatalf("comment report row: %v", err)
	}
	if row.EntityRef != cm.ID || row.EntityID != 0 {
		t.Fatalf("comment report ref=%q id=%d, want ref=%q id=0", row.EntityRef, row.EntityID, cm.ID)
	}

	rec = h.submitReport(t, reporter, url.Values{
		"entity": {"venue_media"}, "entity_ref": {vm.ID}, "reason": {"inappropriate"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("venue media report status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err = h.db.Client.Report.Query().
		Where(report.EntityEQ(report.EntityVenueMedia)).Only(context.Background())
	if err != nil {
		t.Fatalf("venue media report row: %v", err)
	}
	if row.EntityRef != vm.ID || row.EntityID != 0 {
		t.Fatalf("venue media report ref=%q id=%d, want ref=%q id=0", row.EntityRef, row.EntityID, vm.ID)
	}
}

func TestReportSubmitRejectsBadStringRefs(t *testing.T) {
	h := newV12Harness(t)
	reporter := h.verifiedUser(t, "reporter", user.RoleUser)

	before := h.reportCount(t)
	rec := h.submitReport(t, reporter, url.Values{"entity": {"comment"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing comment ref status = %d, want 400", rec.Code)
	}
	rec = h.submitReport(t, reporter, url.Values{
		"entity": {"comment"}, "entity_ref": {"0123456789abcdef0123456789abcdef"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown comment status = %d, want 404", rec.Code)
	}
	rec = h.submitReport(t, reporter, url.Values{
		"entity": {"venue_media"}, "entity_ref": {"0123456789abcdef0123456789abcdef"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown venue media status = %d, want 404", rec.Code)
	}
	if after := h.reportCount(t); after != before {
		t.Fatalf("reports created = %d, want %d", after-before, 0)
	}
}

func TestReportSubmitKeepsIntEntityReports(t *testing.T) {
	h := newV12Harness(t)
	reporter := h.verifiedUser(t, "reporter", user.RoleUser)
	drink, err := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(context.Background())
	if err != nil {
		t.Fatalf("drink: %v", err)
	}

	rec := h.submitReport(t, reporter, url.Values{
		"entity": {"drink"}, "entity_id": {strconv.Itoa(drink.ID)}, "reason": {"other"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("drink report status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err := h.db.Client.Report.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("report row: %v", err)
	}
	if row.Entity != report.EntityDrink || row.EntityID != drink.ID || row.EntityRef != "" {
		t.Fatalf("drink report = %+v", row)
	}
}

func (h *v12Harness) adminRemoveRequest(t *testing.T, admin *ent.User, reportID int) *httptest.ResponseRecorder {
	t.Helper()
	req := h.request(http.MethodPost, "/admin/reports/"+strconv.Itoa(reportID)+"/remove", admin, h.form())
	req.SetPathValue("id", strconv.Itoa(reportID))
	rec := httptest.NewRecorder()
	h.srv.requireAdmin(h.srv.adminReportRemove)(rec, req)
	return rec
}

func (h *v12Harness) newReport(t *testing.T, reporter *ent.User, entity report.Entity, ref string) *ent.Report {
	t.Helper()
	row, err := h.db.Client.Report.Create().
		SetEntity(entity).SetEntityID(0).SetEntityRef(ref).
		SetUserID(reporter.ID).SetReason("other").Save(context.Background())
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	return row
}

func TestAdminReportRemoveComment(t *testing.T) {
	h := newV12Harness(t)
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	owner := h.verifiedUser(t, "owner", user.RoleUser)
	ci := h.createCheckin(t, owner)
	cm, err := h.srv.commentService().Create(context.Background(), owner.ID, ci.ID, "bad comment")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	rep := h.newReport(t, owner, report.EntityComment, cm.ID)

	rec := h.adminRemoveRequest(t, admin, rep.ID)
	if rec.Code != http.StatusFound {
		t.Fatalf("remove status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	got, err := h.db.Client.Comment.Get(context.Background(), cm.ID)
	if err != nil {
		t.Fatalf("comment after remove: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatal("comment was not soft-deleted")
	}
	list, err := h.srv.commentService().List(context.Background(), owner.ID, ci.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	for _, c := range list {
		if c.ID == cm.ID {
			t.Fatal("removed comment still appears in the list")
		}
	}
	saved, err := h.db.Client.Report.Get(context.Background(), rep.ID)
	if err != nil {
		t.Fatalf("report after remove: %v", err)
	}
	if saved.Status != report.StatusResolved {
		t.Fatalf("report status = %q, want resolved", saved.Status)
	}

	// Removing again is idempotent and does not resurrect the report.
	rec = h.adminRemoveRequest(t, admin, rep.ID)
	if rec.Code != http.StatusFound {
		t.Fatalf("second remove status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminReportRemoveVenueMedia(t *testing.T) {
	h := newV12Harness(t)
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	owner := h.verifiedUser(t, "owner", user.RoleUser)
	venue, err := h.db.Client.Venue.Create().SetName("Corner Shop").SetSlug("corner-shop").Save(context.Background())
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	vm, err := h.db.Client.VenueMedia.Create().
		SetVenueID(venue.ID).SetMediaID("media-1").SetKind("menu").
		SetLabel("Old menu").SetUploadedBy(owner.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("venue media: %v", err)
	}
	rep := h.newReport(t, owner, report.EntityVenueMedia, vm.ID)

	rec := h.adminRemoveRequest(t, admin, rep.ID)
	if rec.Code != http.StatusFound {
		t.Fatalf("remove status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	got, err := h.db.Client.VenueMedia.Get(context.Background(), vm.ID)
	if err != nil {
		t.Fatalf("venue media after remove: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatal("venue media was not soft-deleted")
	}
	views, err := h.srv.Venues.VenueMedia(context.Background(), venue.ID)
	if err != nil {
		t.Fatalf("list venue media: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("venue media still visible: %+v", views)
	}
	saved, err := h.db.Client.Report.Get(context.Background(), rep.ID)
	if err != nil {
		t.Fatalf("report after remove: %v", err)
	}
	if saved.Status != report.StatusResolved {
		t.Fatalf("report status = %q, want resolved", saved.Status)
	}
}

func TestAdminReportRemoveUnknownTargets(t *testing.T) {
	h := newV12Harness(t)
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	owner := h.verifiedUser(t, "owner", user.RoleUser)

	rep := h.newReport(t, owner, report.EntityComment, "0123456789abcdef0123456789abcdef")
	if rec := h.adminRemoveRequest(t, admin, rep.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown comment target status = %d, want 404", rec.Code)
	}
	if rec := h.adminRemoveRequest(t, admin, rep.ID+999); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown report status = %d, want 404", rec.Code)
	}
}

func TestAdminReportRemoveRequiresAdmin(t *testing.T) {
	h := newV12Harness(t)
	owner := h.verifiedUser(t, "owner", user.RoleUser)
	member := h.verifiedUser(t, "member", user.RoleUser)
	ci := h.createCheckin(t, owner)
	cm, err := h.srv.commentService().Create(context.Background(), owner.ID, ci.ID, "still here")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	rep := h.newReport(t, owner, report.EntityComment, cm.ID)

	if rec := h.adminRemoveRequest(t, member, rep.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin remove status = %d, want 403", rec.Code)
	}
	if got, err := h.db.Client.Comment.Get(context.Background(), cm.ID); err != nil || got.DeletedAt != nil {
		t.Fatalf("comment changed after forbidden remove: %+v, %v", got, err)
	}
}

func TestAdminReportResolveAndDismissStillWork(t *testing.T) {
	h := newV12Harness(t)
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	owner := h.verifiedUser(t, "owner", user.RoleUser)
	drink, err := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(context.Background())
	if err != nil {
		t.Fatalf("drink: %v", err)
	}

	resolve := func(t *testing.T, handler http.HandlerFunc, want report.Status) {
		t.Helper()
		row, err := h.db.Client.Report.Create().
			SetEntity(report.EntityDrink).SetEntityID(drink.ID).
			SetUserID(owner.ID).SetReason("other").Save(context.Background())
		if err != nil {
			t.Fatalf("report: %v", err)
		}
		req := h.request(http.MethodPost, "/admin/reports/"+strconv.Itoa(row.ID), admin, h.form())
		req.SetPathValue("id", strconv.Itoa(row.ID))
		rec := httptest.NewRecorder()
		h.srv.requireAdmin(handler)(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302: %s", rec.Code, rec.Body.String())
		}
		got, err := h.db.Client.Report.Get(context.Background(), row.ID)
		if err != nil {
			t.Fatalf("report get: %v", err)
		}
		if got.Status != want {
			t.Fatalf("status = %q, want %q", got.Status, want)
		}
	}

	resolve(t, h.srv.adminReportResolve, report.StatusResolved)
	resolve(t, h.srv.adminReportDismiss, report.StatusDismissed)
}
