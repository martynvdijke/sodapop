package api

import (
	"net/http"
	"testing"
)

func TestWishlistAlertsGetDefaultsFalse(t *testing.T) {
	h := newHarness(t)
	rec := h.doWith(t, h.token, http.MethodGet, "/api/v1/me/wishlist/alerts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	if data["friend_checkins"] != false || data["venue_shelf"] != false {
		t.Fatalf("defaults = %v, want both false", data)
	}
}

func TestWishlistAlertsPutUpdatesOnlySuppliedField(t *testing.T) {
	h := newHarness(t)

	rec := h.doWith(t, h.token, http.MethodPut, "/api/v1/me/wishlist/alerts", `{"friend_checkins":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, body %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	if data["friend_checkins"] != true || data["venue_shelf"] != false {
		t.Fatalf("after first put = %v, want friend true venue false", data)
	}

	rec = h.doWith(t, h.token, http.MethodPut, "/api/v1/me/wishlist/alerts", `{"venue_shelf":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("second put status = %d, body %s", rec.Code, rec.Body.String())
	}
	data = dataMap(t, rec)
	if data["friend_checkins"] != true || data["venue_shelf"] != true {
		t.Fatalf("after second put = %v, want both true", data)
	}

	rec = h.doWith(t, h.token, http.MethodGet, "/api/v1/me/wishlist/alerts", "")
	data = dataMap(t, rec)
	if data["friend_checkins"] != true || data["venue_shelf"] != true {
		t.Fatalf("get = %v, want both true", data)
	}
}
