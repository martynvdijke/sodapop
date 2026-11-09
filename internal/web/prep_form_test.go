package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent/checkin"
	"sodapop/ent/user"
)

func TestCheckinSubmitStoresPreparation(t *testing.T) {
	h := newGalleryWebHarness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	member := h.verifiedUser(t, "prep-member", user.RoleUser)
	drink := h.galleryDrink(t, "Prep Coffee", "prep-coffee")

	fields := url.Values{}
	fields.Set("drink_id", strconv.Itoa(drink.ID))
	fields.Set("visibility", "public")
	fields.Set("idempotency_key", "prep-form-1")
	fields.Set("prep_method", "pour_over")
	fields.Set("prep_note", "18g / 300ml")

	rec := httptest.NewRecorder()
	h.srv.checkinSubmit(rec, checkinMultipartValues(t, member, fields))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	row, err := h.db.Client.Checkin.Query().
		Where(checkin.IdempotencyKey("prep-form-1")).
		Only(context.Background())
	if err != nil {
		t.Fatalf("check-in: %v", err)
	}
	if row.PrepMethod != checkin.PrepMethodPourOver {
		t.Errorf("prep_method = %q, want pour_over", row.PrepMethod)
	}
	if row.PrepNote != "18g / 300ml" {
		t.Errorf("prep_note = %q, want 18g / 300ml", row.PrepNote)
	}

	req := h.request(http.MethodGet, "/checkins/"+row.ID, member, url.Values{})
	req.SetPathValue("id", row.ID)
	detail := httptest.NewRecorder()
	h.srv.checkinDetailPage(detail, req)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", detail.Code, detail.Body.String())
	}
	body := detail.Body.String()
	if !strings.Contains(body, "Pour-over") {
		t.Errorf("detail missing humanised method: %s", body)
	}
	if !strings.Contains(body, "18g / 300ml") {
		t.Errorf("detail missing prep note: %s", body)
	}
}
