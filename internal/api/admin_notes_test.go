package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/report"
	"sodapop/ent/user"
	"sodapop/internal/auth"
)

func adminTokenFor(t *testing.T, h *harness, handle string, role user.Role) (*ent.User, *auth.APIToken) {
	t.Helper()
	u := newUser(t, h, handle, true, role)
	return u, &auth.APIToken{User: u, Scopes: map[string]bool{"read": true, "write": true, "admin": true}}
}

func TestAdminReportNotesAPI(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin, tok := adminTokenFor(t, h, "modnote", user.RoleAdmin)
	reporter, err := h.server.DB.Client.User.Get(ctx, h.bob)
	if err != nil {
		t.Fatalf("reporter: %v", err)
	}
	rep, err := h.server.DB.Client.Report.Create().
		SetEntity(report.EntityDrink).SetEntityID(1).SetUserID(reporter.ID).
		SetReason("other").Save(ctx)
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	// Non-admins see 404 on every note route.
	nonAdmin := newUser(t, h, "plain", true, user.RoleUser)
	plain := &auth.APIToken{User: nonAdmin, Scopes: map[string]bool{"read": true, "write": true}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/1/notes", nil)
	req.SetPathValue("id", strconv.Itoa(rep.ID))
	rec := httptest.NewRecorder()
	if err := h.server.listReportNotes(rec, req, plain); err != nil {
		t.Fatalf("non-admin list: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-admin status = %d, want 404", rec.Code)
	}

	// Create a note.
	body := `{"body":"checking the label"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/reports/1/notes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", strconv.Itoa(rep.ID))
	rec = httptest.NewRecorder()
	if err := h.server.createReportNote(rec, req, tok); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			ID           int    `json:"id"`
			AuthorHandle string `json:"author_handle"`
			Body         string `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Data.Body != "checking the label" || created.Data.AuthorHandle != admin.Handle {
		t.Fatalf("created = %+v", created.Data)
	}

	// List it back.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/1/notes", nil)
	req.SetPathValue("id", strconv.Itoa(rep.ID))
	rec = httptest.NewRecorder()
	if err := h.server.listReportNotes(rec, req, tok); err != nil {
		t.Fatalf("list: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var listed struct {
		Data []struct {
			ID   int    `json:"id"`
			Body string `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Body != "checking the label" {
		t.Fatalf("listed = %+v", listed.Data)
	}

	// Delete it.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/notes/1", nil)
	req.SetPathValue("id", strconv.Itoa(created.Data.ID))
	rec = httptest.NewRecorder()
	if err := h.server.deleteAdminNote(rec, req, tok); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rec.Code)
	}

	// Unknown reports are 404, not empty lists.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/9999/notes", nil)
	req.SetPathValue("id", "9999")
	rec = httptest.NewRecorder()
	if err := h.server.listReportNotes(rec, req, tok); err != nil {
		t.Fatalf("missing report: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing report status = %d, want 404", rec.Code)
	}
}
