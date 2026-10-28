package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent/list"
)

func TestListCRUDAPI(t *testing.T) {
	h := newHarness(t)
	bobToken := h.tokenFor(t, h.bob)
	drink := newDrink(t, h, "Cola", "cola", nil)

	rec := h.do(t, http.MethodPost, "/api/v1/lists", `{"name":"Friday sodas","description":"weekend","visibility":"friends"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	created := dataMap(t, rec)
	id := created["id"].(string)
	if created["name"] != "Friday sodas" || created["visibility"] != "friends" || created["item_count"].(float64) != 0 {
		t.Fatalf("create body = %+v", created)
	}

	rec = h.do(t, http.MethodPatch, "/api/v1/lists/"+id, `{"name":"Saturday sodas","visibility":"private"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d: %s", rec.Code, rec.Body.String())
	}
	updated := dataMap(t, rec)
	if updated["name"] != "Saturday sodas" || updated["visibility"] != "private" {
		t.Fatalf("patch body = %+v", updated)
	}

	rec = h.do(t, http.MethodPost, "/api/v1/lists/"+id+"/items", `{"drink_id":`+strconv.Itoa(drink.ID)+`,"note":"cold"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add item = %d: %s", rec.Code, rec.Body.String())
	}
	item := dataMap(t, rec)
	if item["slug"] != "cola" || item["note"] != "cold" {
		t.Fatalf("item body = %+v", item)
	}

	rec = h.do(t, http.MethodPost, "/api/v1/lists/"+id+"/items", `{"slug":"cola"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate add = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if n, _ := h.server.DB.Client.ListItem.Query().Count(context.Background()); n != 1 {
		t.Fatalf("duplicate add produced %d items", n)
	}

	// A stranger sees the same 404 as a missing list and never a 403.
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodPatch, "/api/v1/lists/" + id, `{"name":"stolen"}`},
		{http.MethodPost, "/api/v1/lists/" + id + "/items", `{"drink_id":` + strconv.Itoa(drink.ID) + `}`},
		{http.MethodDelete, "/api/v1/lists/" + id + "/items/" + strconv.Itoa(drink.ID), ""},
		{http.MethodDelete, "/api/v1/lists/" + id, ""},
	} {
		rec = h.doWith(t, bobToken, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("non-owner %s %s = %d, want 404: %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	rec = h.do(t, http.MethodDelete, "/api/v1/lists/"+id+"/items/"+strconv.Itoa(drink.ID), "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove item = %d: %s", rec.Code, rec.Body.String())
	}
	if n, _ := h.server.DB.Client.ListItem.Query().Count(context.Background()); n != 0 {
		t.Fatalf("items after remove = %d", n)
	}

	rec = h.do(t, http.MethodDelete, "/api/v1/lists/"+id, "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body.String())
	}
	if n, _ := h.server.DB.Client.List.Query().Where(list.ID(id)).Count(context.Background()); n != 0 {
		t.Fatalf("list still present after delete")
	}
}

func TestListSystemRulesAndValidation(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Cola", "cola", nil)
	sys, err := h.server.DB.Client.List.Create().
		SetUserID(h.alice).SetName("Wishlist").SetIsSystem(true).SetVisibility("private").
		Save(context.Background())
	if err != nil {
		t.Fatalf("system list: %v", err)
	}

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPatch, "/api/v1/lists/" + sys.ID, `{"name":"Renamed"}`},
		{http.MethodDelete, "/api/v1/lists/" + sys.ID, ""},
	} {
		rec := h.do(t, tc.method, tc.path, tc.body, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("system %s = %d, want 400: %s", tc.method, rec.Code, rec.Body.String())
		}
	}

	rec := h.do(t, http.MethodPost, "/api/v1/lists/"+sys.ID+"/items", `{"drink_id":`+strconv.Itoa(drink.ID)+`}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("system add = %d: %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodDelete, "/api/v1/lists/"+sys.ID+"/items/"+strconv.Itoa(drink.ID), "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("system remove = %d: %s", rec.Code, rec.Body.String())
	}

	for _, body := range []string{
		`{"name":""}`,
		`{"name":"` + strings.Repeat("x", 81) + `"}`,
		`{"name":"ok","description":"` + strings.Repeat("d", 501) + `"}`,
		`{"name":"ok","visibility":"secret"}`,
	} {
		rec = h.do(t, http.MethodPost, "/api/v1/lists", body, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid %s = %d, want 400: %s", body, rec.Code, rec.Body.String())
		}
	}

	rec = h.do(t, http.MethodPost, "/api/v1/lists", `{"name":"Valid"}`, true)
	listID := dataMap(t, rec)["id"].(string)
	for _, body := range []string{
		`{}`,
		`{"drink_id":999999}`,
		`{"slug":"does-not-exist"}`,
		`{"drink_id":` + strconv.Itoa(drink.ID) + `,"note":"` + strings.Repeat("n", 201) + `"}`,
	} {
		rec = h.do(t, http.MethodPost, "/api/v1/lists/"+listID+"/items", body, true)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Fatalf("invalid item %s = %d, want 400/404: %s", body, rec.Code, rec.Body.String())
		}
	}
}
