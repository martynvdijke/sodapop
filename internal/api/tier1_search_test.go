package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestListSavedSearches(t *testing.T) {
	h := newHarness(t)

	rec := h.do(t, http.MethodGet, "/api/v1/me/searches", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if rows := dataList(t, rec); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
	meta := decodeBody(t, rec)["meta"].(map[string]any)
	if meta["max"].(float64) != 25 {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestCreateSavedSearch(t *testing.T) {
	h := newHarness(t)

	body := `{"name":"Cherry drops","query":"cherry","beverage_type":"soda",` +
		`"style_slug":"cola","brand_slug":"fanta","flavour_slug":"cherry",` +
		`"caffeine_free":false,"sugar_free":true}`
	rec := h.do(t, http.MethodPost, "/api/v1/me/searches", body, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	view := dataMap(t, rec)
	if view["name"] != "Cherry drops" || view["query"] != "cherry" || view["sugar_free"] != true {
		t.Fatalf("view = %+v", view)
	}
	// An omitted notify_drops defaults to on, and last_notified_at is null.
	if view["notify_drops"] != true {
		t.Fatalf("notify_drops = %v", view["notify_drops"])
	}
	if got, ok := view["last_notified_at"]; !ok || got != nil {
		t.Fatalf("last_notified_at = %v (present %v)", got, ok)
	}
	if _, ok := view["id"].(string); !ok {
		t.Fatalf("id missing: %+v", view)
	}
	if loc := rec.Header().Get("Location"); loc == "" {
		t.Fatal("Location header missing")
	}

	// It shows up in the list.
	list := h.do(t, http.MethodGet, "/api/v1/me/searches", "", true)
	rows := dataList(t, list)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestCreateSavedSearchExplicitNotifyOff(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodPost, "/api/v1/me/searches", `{"query":"cola","notify_drops":false}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if dataMap(t, rec)["notify_drops"] != false {
		t.Fatalf("notify_drops was not kept false: %s", rec.Body.String())
	}
}

func TestCreateSavedSearchValidation(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"no filters", `{}`, http.StatusBadRequest, "invalid_request"},
		{"unknown beverage type", `{"beverage_type":"cider"}`, http.StatusBadRequest, "invalid_request"},
		{"bad slug characters", `{"style_slug":"cold brew"}`, http.StatusBadRequest, "invalid_request"},
		{"unknown field", `{"colour":"red"}`, http.StatusBadRequest, "invalid_request"},
		{"malformed json", `{`, http.StatusBadRequest, "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			rec := h.do(t, http.MethodPost, "/api/v1/me/searches", tt.body, true)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body.String())
			}
			errBody := decodeBody(t, rec)["error"].(map[string]any)
			if errBody["code"] != tt.code {
				t.Fatalf("code = %v, want %s", errBody["code"], tt.code)
			}
		})
	}
}

func TestCreateSavedSearchCap(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 25; i++ {
		body := `{"query":"cola ` + strconv.Itoa(i) + `"}`
		if rec := h.do(t, http.MethodPost, "/api/v1/me/searches", body, true); rec.Code != http.StatusCreated {
			t.Fatalf("create %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	rec := h.do(t, http.MethodPost, "/api/v1/me/searches", `{"query":"one too many"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	errBody := decodeBody(t, rec)["error"].(map[string]any)
	if errBody["code"] != "limit_reached" {
		t.Fatalf("code = %v", errBody["code"])
	}
}

func TestDeleteSavedSearch(t *testing.T) {
	h := newHarness(t)

	created := h.do(t, http.MethodPost, "/api/v1/me/searches", `{"query":"cherry"}`, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	id := dataMap(t, created)["id"].(string)

	t.Run("owner deletes", func(t *testing.T) {
		rec := h.do(t, http.MethodDelete, "/api/v1/me/searches/"+id, "", true)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		if rows := dataList(t, h.do(t, http.MethodGet, "/api/v1/me/searches", "", true)); len(rows) != 0 {
			t.Fatalf("rows after delete = %+v", rows)
		}
	})

	t.Run("missing id is 404", func(t *testing.T) {
		rec := h.do(t, http.MethodDelete, "/api/v1/me/searches/does-not-exist", "", true)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestDeleteSavedSearchScoping(t *testing.T) {
	h := newHarness(t)
	created := h.do(t, http.MethodPost, "/api/v1/me/searches", `{"query":"cherry"}`, true)
	id := dataMap(t, created)["id"].(string)

	// Bob may not delete or see alice's search.
	bobToken := h.tokenFor(t, h.bob)
	rec := h.doWith(t, bobToken, http.MethodDelete, "/api/v1/me/searches/"+id, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user delete status = %d: %s", rec.Code, rec.Body.String())
	}
	if rows := dataList(t, h.doWith(t, bobToken, http.MethodGet, "/api/v1/me/searches", "")); len(rows) != 0 {
		t.Fatalf("bob sees %+v", rows)
	}
	// Alice's row is untouched.
	if rows := dataList(t, h.do(t, http.MethodGet, "/api/v1/me/searches", "", true)); len(rows) != 1 {
		t.Fatalf("alice rows = %+v", rows)
	}
}

// The API never asks for another member's searches, so there is no collection
// endpoint to enumerate.
func TestSavedSearchesRequiresAuth(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/me/searches", ""},
		{http.MethodPost, "/api/v1/me/searches", `{"query":"cola"}`},
		{http.MethodDelete, "/api/v1/me/searches/abc", ""},
	} {
		rec := h.do(t, tc.method, tc.path, tc.body, false)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", tc.method, tc.path, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
}
