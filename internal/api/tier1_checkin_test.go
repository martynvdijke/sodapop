package api

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestPatchCheckin(t *testing.T) {
	h := newHarness(t)
	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)
	drink := newDrink(t, h, "Cola", "cola", nil)
	c := checkinFor(t, h, alice, drink.ID, "public")

	t.Run("owner patches a subset", func(t *testing.T) {
		body := `{"rating":14,"note":"  updated  ","serving":"bottle","price_cents":240,"price_currency":"eur"}`
		rec := h.doWith(t, h.token, http.MethodPatch, "/api/v1/checkins/"+c.ID, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		view := dataMap(t, rec)
		if view["rating"].(float64) != 14 {
			t.Fatalf("rating = %v", view["rating"])
		}
		if view["note"] != "updated" {
			t.Fatalf("view = %+v", view)
		}
		stored, err := h.server.Auth.Client.Checkin.Get(context.Background(), c.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if string(stored.Serving) != "bottle" {
			t.Fatalf("serving = %q", stored.Serving)
		}
		if view["price_cents"].(float64) != 240 || view["price_currency"] != "EUR" {
			t.Fatalf("price = %v %v", view["price_cents"], view["price_currency"])
		}
		if _, ok := view["edited_at"]; !ok {
			t.Fatalf("edited_at missing: %+v", view)
		}
		// Untouched fields keep their stored value.
		if view["visibility"] != "public" {
			t.Fatalf("visibility = %v", view["visibility"])
		}
	})

	t.Run("explicit null clears the rating", func(t *testing.T) {
		rec := h.doWith(t, h.token, http.MethodPatch, "/api/v1/checkins/"+c.ID, `{"rating":null}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		if dataMap(t, rec)["rating"] != nil {
			t.Fatalf("rating was not cleared: %s", rec.Body.String())
		}
	})

	t.Run("non-owner is 404", func(t *testing.T) {
		rec := h.doWith(t, h.tokenFor(t, bob.ID), http.MethodPatch, "/api/v1/checkins/"+c.ID, `{"note":"hijack"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid rating is 400", func(t *testing.T) {
		rec := h.doWith(t, h.token, http.MethodPatch, "/api/v1/checkins/"+c.ID, `{"rating":21}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown field is rejected", func(t *testing.T) {
		rec := h.doWith(t, h.token, http.MethodPatch, "/api/v1/checkins/"+c.ID, `{"sugar_g":9}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("outside the window is 400", func(t *testing.T) {
		old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
		if _, err := h.server.Auth.Client.Checkin.UpdateOneID(c.ID).SetCreatedAt(old).Save(context.Background()); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		rec := h.doWith(t, h.token, http.MethodPatch, "/api/v1/checkins/"+c.ID, `{"note":"late"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if body := decodeBody(t, rec); body["error"].(map[string]any)["code"] != "edit_window_closed" {
			t.Fatalf("error = %s", rec.Body.String())
		}
	})
}

func TestPatchComment(t *testing.T) {
	h := newHarness(t)
	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)
	drink := newDrink(t, h, "Cola", "cola", nil)
	c := checkinFor(t, h, alice, drink.ID, "public")

	created := h.do(t, http.MethodPost, "/api/v1/checkins/"+c.ID+"/comments", `{"body":"first"}`, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("comment: %d %s", created.Code, created.Body.String())
	}
	commentID := dataMap(t, created)["id"].(string)

	t.Run("author edits", func(t *testing.T) {
		rec := h.doWith(t, h.token, http.MethodPatch, "/api/v1/comments/"+commentID, `{"body":"second"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		view := dataMap(t, rec)
		if view["body"] != "second" {
			t.Fatalf("body = %v", view["body"])
		}
		if _, ok := view["edited_at"]; !ok {
			t.Fatalf("edited_at missing: %+v", view)
		}
	})

	t.Run("non-author is 404", func(t *testing.T) {
		rec := h.doWith(t, h.tokenFor(t, bob.ID), http.MethodPatch, "/api/v1/comments/"+commentID, `{"body":"hijack"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("empty body is 400", func(t *testing.T) {
		rec := h.doWith(t, h.token, http.MethodPatch, "/api/v1/comments/"+commentID, `{"body":"  "}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
	})
}

// The creation endpoint must persist an optional price so the app does not need
// a second PATCH just to record it.
func TestCreateCheckinWithPrice(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Cola", "cola", nil)

	rec := h.do(t, http.MethodPost, "/api/v1/checkins",
		`{"drink_id":`+strconv.Itoa(drink.ID)+`,"visibility":"public","price_cents":199,"price_currency":"gbp"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	view := dataMap(t, rec)
	if view["price_cents"].(float64) != 199 || view["price_currency"] != "GBP" {
		t.Fatalf("price = %v %v", view["price_cents"], view["price_currency"])
	}

	c, err := h.server.Auth.Client.Checkin.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if c.PriceCents == nil || *c.PriceCents != 199 || c.PriceCurrency != "GBP" {
		t.Fatalf("stored price = %v %q", c.PriceCents, c.PriceCurrency)
	}
}
