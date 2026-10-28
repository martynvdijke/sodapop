package api

import (
	"net/http"
	"testing"
)

func TestCompareAPI(t *testing.T) {
	h := newHarness(t)
	newDrink(t, h, "Cola", "cola", nil)
	newDrink(t, h, "Lemon", "lemon", nil)
	newDrink(t, h, "Grape", "grape", nil)

	rec := h.do(t, http.MethodGet, "/api/v1/compare?drinks=cola,lemon", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("compare = %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	rows, _ := body["data"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	meta, _ := body["meta"].(map[string]any)
	if meta["count"].(float64) != 2 {
		t.Fatalf("meta = %+v", meta)
	}
	first, _ := rows[0].(map[string]any)
	if first["slug"] != "cola" {
		t.Fatalf("order lost: %+v", first)
	}

	rec = h.do(t, http.MethodGet, "/api/v1/compare?drinks=cola&drinks=lemon&drinks=grape", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("repeated compare = %d: %s", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct {
		query  string
		code   string
		status int
	}{
		{"?drinks=cola", "compare_count", http.StatusBadRequest},
		{"?drinks=cola,lemon,grape,cola", "compare_duplicate", http.StatusBadRequest},
		{"?drinks=cola,missing", "not_found", http.StatusNotFound},
	} {
		rec = h.do(t, http.MethodGet, "/api/v1/compare"+tc.query, "", true)
		if rec.Code != tc.status {
			t.Fatalf("%s = %d, want %d: %s", tc.query, rec.Code, tc.status, rec.Body.String())
		}
		errObj, _ := decodeBody(t, rec)["error"].(map[string]any)
		if errObj["code"] != tc.code {
			t.Fatalf("%s code = %v, want %s", tc.query, errObj["code"], tc.code)
		}
	}
}
