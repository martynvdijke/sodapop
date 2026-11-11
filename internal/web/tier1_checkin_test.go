package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/internal/checkins"
)

const editFormProbe = `R={{.Form.Rating}}|S={{.Form.Serving}}|PM={{.Form.PrepMethod}}|PN={{.Form.PrepNote}}|N={{.Form.Note}}|V={{.Form.Visibility}}|P={{.Form.PriceAmount}}|C={{.Form.Currency}}|ERR={{.Error}}`

// newEditCheckin creates a check-in owned by u and returns it.
func newEditCheckin(t *testing.T, h *v12Harness, u *ent.User) *ent.Checkin {
	t.Helper()
	drink, err := h.db.Client.Drink.Query().Only(context.Background())
	if err != nil {
		drink, err = h.db.Client.Drink.Create().SetName("Ginger Beer").SetSlug("ginger-beer").Save(context.Background())
		if err != nil {
			t.Fatalf("drink: %v", err)
		}
	}
	rating := 16
	price := 275
	c, _, err := h.srv.Checkins.Create(context.Background(), u, checkins.CreateInput{
		DrinkID: drink.ID, Rating: &rating, Serving: "can", Note: "original",
		PriceCents: &price, PriceCurrency: "EUR", Visibility: "public",
	})
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func editGet(t *testing.T, h *v12Harness, u *ent.User, id string) *httptest.ResponseRecorder {
	t.Helper()
	rec := h.renderWith(t, "checkin_edit", editFormProbe)
	req := h.request(http.MethodGet, "/checkins/"+id+"/edit", u, url.Values{})
	req.SetPathValue("id", id)
	h.srv.checkinEditPage(rec, req)
	return rec
}

func TestCheckinEditPage(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")
	other := h.user(t, "other")
	c := newEditCheckin(t, h, owner)

	t.Run("owner sees prefilled form", func(t *testing.T) {
		rec := editGet(t, h, owner, c.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{"R=4.00", "S=can", "N=original", "P=2.75", "C=EUR", "ERR="} {
			if !strings.Contains(body, want) {
				t.Fatalf("body missing %q: %s", want, body)
			}
		}
	})

	t.Run("non-owner is 404", func(t *testing.T) {
		rec := editGet(t, h, other, c.ID)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("outside the window is 404", func(t *testing.T) {
		old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
		if _, err := h.db.Client.Checkin.UpdateOneID(c.ID).SetCreatedAt(old).Save(context.Background()); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		rec := editGet(t, h, owner, c.ID)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("deleted is 404", func(t *testing.T) {
		deleted := newEditCheckin(t, h, owner)
		if err := h.srv.Checkins.Delete(context.Background(), owner.ID, deleted.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		rec := editGet(t, h, owner, deleted.ID)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestCheckinEditSubmit(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")
	c := newEditCheckin(t, h, owner)

	t.Run("saves and redirects", func(t *testing.T) {
		form := url.Values{
			"csrf_token":  {"test-csrf"},
			"rating":      {"3.25"},
			"serving":     {"bottle"},
			"prep_method": {"none"},
			"prep_note":   {""},
			"note":        {"  revised note  "},
			"visibility":  {"friends"},
			"venue_id":    {""},
			"price":       {"3.10"},
			"currency":    {"usd"},
		}
		rec := h.renderWith(t, "checkin_edit", editFormProbe)
		req := h.request(http.MethodPost, "/checkins/"+c.ID+"/edit", owner, form)
		req.SetPathValue("id", c.ID)
		h.srv.checkinEditSubmit(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302: %s", rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "/checkins/"+c.ID {
			t.Fatalf("location = %q", loc)
		}
		detail, err := h.srv.Checkins.Get(context.Background(), owner.ID, c.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if detail.Rating == nil || *detail.Rating != 13 {
			t.Fatalf("rating = %v", detail.Rating)
		}
		if detail.Note != "revised note" || detail.Visibility != "friends" || detail.Serving != "bottle" {
			t.Fatalf("detail = %+v", detail)
		}
		if detail.PriceCents == nil || *detail.PriceCents != 310 || detail.PriceCurrency != "USD" {
			t.Fatalf("price = %v %q", detail.PriceCents, detail.PriceCurrency)
		}
		if detail.EditedAt == "" {
			t.Fatal("edited_at not set")
		}
	})

	t.Run("invalid rating re-renders", func(t *testing.T) {
		form := url.Values{
			"csrf_token": {"test-csrf"},
			"rating":     {"9"},
			"serving":    {"can"},
			"visibility": {"public"},
		}
		rec := h.renderWith(t, "checkin_edit", editFormProbe)
		req := h.request(http.MethodPost, "/checkins/"+c.ID+"/edit", owner, form)
		req.SetPathValue("id", c.ID)
		h.srv.checkinEditSubmit(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "R=9") {
			t.Fatalf("submitted rating was not kept: %s", rec.Body.String())
		}
	})

	t.Run("invalid currency re-renders", func(t *testing.T) {
		form := url.Values{
			"csrf_token": {"test-csrf"},
			"serving":    {"can"},
			"visibility": {"public"},
			"currency":   {"EURO"},
		}
		rec := h.renderWith(t, "checkin_edit", editFormProbe)
		req := h.request(http.MethodPost, "/checkins/"+c.ID+"/edit", owner, form)
		req.SetPathValue("id", c.ID)
		h.srv.checkinEditSubmit(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "three-letter") {
			t.Fatalf("missing currency message: %s", rec.Body.String())
		}
	})
}

func TestCheckinEditSubmitAfterWindow(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")
	c := newEditCheckin(t, h, owner)
	old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	if _, err := h.db.Client.Checkin.UpdateOneID(c.ID).SetCreatedAt(old).Save(context.Background()); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	form := url.Values{
		"csrf_token": {"test-csrf"},
		"serving":    {"can"},
		"visibility": {"public"},
		"note":       {"too late"},
	}
	rec := h.renderWith(t, "checkin_edit", editFormProbe)
	req := h.request(http.MethodPost, "/checkins/"+c.ID+"/edit", owner, form)
	req.SetPathValue("id", c.ID)
	h.srv.checkinEditSubmit(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "24 hours") {
		t.Fatalf("missing window message: %s", rec.Body.String())
	}
	detail, err := h.srv.Checkins.Get(context.Background(), owner.ID, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.Note == "too late" {
		t.Fatal("edit was applied after the window")
	}
}

func TestCommentEditHandler(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")
	stranger := h.user(t, "stranger")
	c := newEditCheckin(t, h, owner)

	created, err := h.srv.commentService().Create(context.Background(), owner.ID, c.ID, "first draft")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}

	t.Run("author edit redirects to the thread", func(t *testing.T) {
		form := url.Values{
			"csrf_token": {"test-csrf"},
			"body":       {"second draft"},
			"checkin_id": {c.ID},
		}
		rec := h.renderWith(t, "checkin_detail", `unused`)
		req := h.request(http.MethodPost, "/comments/"+created.ID+"/edit", owner, form)
		req.SetPathValue("id", created.ID)
		h.srv.commentEdit(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302: %s", rec.Code, rec.Body.String())
		}
		want := "/checkins/" + c.ID + "#comment-" + created.ID
		if loc := rec.Header().Get("Location"); loc != want {
			t.Fatalf("location = %q, want %q", loc, want)
		}
		list, err := h.srv.commentService().List(context.Background(), owner.ID, c.ID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(list) != 1 || list[0].Body != "second draft" || list[0].EditedAt == "" {
			t.Fatalf("comment = %+v", list)
		}
	})

	t.Run("non-author is 404", func(t *testing.T) {
		form := url.Values{
			"csrf_token": {"test-csrf"},
			"body":       {"hijack"},
			"checkin_id": {c.ID},
		}
		rec := h.renderWith(t, "checkin_detail", `unused`)
		req := h.request(http.MethodPost, "/comments/"+created.ID+"/edit", stranger, form)
		req.SetPathValue("id", created.ID)
		h.srv.commentEdit(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})
}

// The detail page must render the real templates with the new affordances:
// the owner edit link, the price, the edited marker and the comment edit form.
func TestCheckinDetailRendersEditAffordances(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	owner := h.user(t, "owner")
	c := newEditCheckin(t, h, owner)
	if _, err := h.srv.Checkins.Update(context.Background(), owner.ID, c.ID, checkins.UpdateInput{
		Note: strPtrWeb("edited body"),
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := h.srv.commentService().Create(context.Background(), owner.ID, c.ID, "my comment"); err != nil {
		t.Fatalf("comment: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/checkins/"+c.ID, nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey{}, owner))
	req.SetPathValue("id", c.ID)
	rec := httptest.NewRecorder()
	h.srv.renderCheckinDetail(rec, req, http.StatusOK, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"/checkins/" + c.ID + "/edit",
		"2.75 EUR",
		"edited just now",
		"/comments/",
		"comment-edit-form",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail body missing %q", want)
		}
	}
}

func strPtrWeb(v string) *string { return &v }

// The insights page must render the spend block only when a priced check-in
// exists in the range.
func TestInsightsRendersSpend(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	owner := h.user(t, "owner")
	newEditCheckin(t, h, owner)

	req := httptest.NewRequest(http.MethodGet, "/insights", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey{}, owner))
	rec := httptest.NewRecorder()
	h.srv.insightsPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Spend") || !strings.Contains(body, "2.75 EUR") {
		t.Fatalf("insights body missing spend block")
	}
}

// The creation form must persist an optional price, and the detail page must
// show it.
func TestCheckinCreateWithPrice(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")
	drink, err := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(context.Background())
	if err != nil {
		t.Fatalf("drink: %v", err)
	}

	form := url.Values{
		"csrf_token": {"test-csrf"},
		"drink_id":   {strconv.Itoa(drink.ID)},
		"serving":    {"can"},
		"visibility": {"public"},
		"price":      {"1.80"},
		"currency":   {"gbp"},
	}
	rec := httptest.NewRecorder()
	req := checkinMultipartValues(t, owner, form)
	h.srv.checkinSubmit(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	c, err := h.db.Client.Checkin.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if c.PriceCents == nil || *c.PriceCents != 180 || c.PriceCurrency != "GBP" {
		t.Fatalf("price = %v %q", c.PriceCents, c.PriceCurrency)
	}
}
