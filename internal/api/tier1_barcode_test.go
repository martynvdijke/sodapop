package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestContributeBarcode(t *testing.T) {
	h := newHarness(t)
	newDrink(t, h, "Cola", "cola", nil)

	t.Run("creates a barcode", func(t *testing.T) {
		rec := h.do(t, http.MethodPost, "/api/v1/drinks/cola/barcodes",
			`{"gtin":"5449000000996","symbology":"ean13"}`, true)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		body := decodeBody(t, rec)
		if body["gtin"] != "05449000000996" {
			t.Fatalf("gtin = %v", body["gtin"])
		}
		if body["created"] != true {
			t.Fatalf("created = %v", body["created"])
		}
	})

	t.Run("repeating the same code is 200 with created false", func(t *testing.T) {
		rec := h.do(t, http.MethodPost, "/api/v1/drinks/cola/barcodes",
			`{"gtin":"5449 0000 00996"}`, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		body := decodeBody(t, rec)
		if body["created"] != false || body["gtin"] != "05449000000996" {
			t.Fatalf("body = %v", body)
		}
	})

	t.Run("invalid check digit is 400", func(t *testing.T) {
		rec := h.do(t, http.MethodPost, "/api/v1/drinks/cola/barcodes",
			`{"gtin":"5449000000995"}`, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		errBody := decodeBody(t, rec)["error"].(map[string]any)
		if errBody["code"] != "invalid_barcode" {
			t.Fatalf("code = %v", errBody["code"])
		}
	})

	t.Run("missing code is 400", func(t *testing.T) {
		rec := h.do(t, http.MethodPost, "/api/v1/drinks/cola/barcodes", `{}`, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown field is 400", func(t *testing.T) {
		rec := h.do(t, http.MethodPost, "/api/v1/drinks/cola/barcodes",
			`{"gtin":"5449000000996","colour":"red"}`, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown drink is 404", func(t *testing.T) {
		rec := h.do(t, http.MethodPost, "/api/v1/drinks/nope/barcodes",
			`{"gtin":"5449000000996"}`, true)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("anonymous is 401", func(t *testing.T) {
		rec := h.do(t, http.MethodPost, "/api/v1/drinks/cola/barcodes",
			`{"gtin":"5449000000996"}`, false)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestContributeBarcodeConflict(t *testing.T) {
	h := newHarness(t)
	newDrink(t, h, "Owner Cola", "owner-cola", nil)
	newDrink(t, h, "Other Cola", "other-cola", nil)

	if rec := h.do(t, http.MethodPost, "/api/v1/drinks/owner-cola/barcodes",
		`{"gtin":"5449000000996"}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}

	rec := h.do(t, http.MethodPost, "/api/v1/drinks/other-cola/barcodes",
		`{"gtin":"5449000000996"}`, true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	errBody := decodeBody(t, rec)["error"].(map[string]any)
	if errBody["code"] != "barcode_taken" {
		t.Fatalf("code = %v", errBody["code"])
	}
	message, _ := errBody["message"].(string)
	if !strings.Contains(message, "Owner Cola") {
		t.Fatalf("message does not name the owning drink: %q", message)
	}
}
