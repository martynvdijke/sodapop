package api

import (
	"net/http"
	"testing"
)

func TestCreateDrinkDedupesBarcodeAndSoftKey(t *testing.T) {
	h := newHarness(t)

	t.Run("barcode replay returns the existing drink", func(t *testing.T) {
		first := h.do(t, http.MethodPost, "/api/v1/drinks",
			`{"name":"Barcode Cola","brand":"Acme","volume_ml":330,"container":"can","barcode":"5449000000996"}`, true)
		if first.Code != http.StatusCreated {
			t.Fatalf("first: %d %s", first.Code, first.Body.String())
		}
		firstData := dataMap(t, first)
		if firstData["message"] != "provisional drink created" {
			t.Fatalf("first message = %v", firstData["message"])
		}

		second := h.do(t, http.MethodPost, "/api/v1/drinks",
			`{"name":"Barcode Cola Renamed","brand":"Acme","volume_ml":330,"container":"can","barcode":"5449000000996"}`, true)
		if second.Code != http.StatusOK {
			t.Fatalf("second: %d %s", second.Code, second.Body.String())
		}
		secondData := dataMap(t, second)
		if secondData["id"] != firstData["id"] {
			t.Fatalf("second id = %v, want %v", secondData["id"], firstData["id"])
		}
		if secondData["message"] != "existing equivalent drink returned" {
			t.Fatalf("second message = %v", secondData["message"])
		}
	})

	t.Run("soft key ignores case across fields", func(t *testing.T) {
		first := h.do(t, http.MethodPost, "/api/v1/drinks",
			`{"name":"Soft Key Cola","brand":"Acme","volume_ml":355,"container":"can"}`, true)
		if first.Code != http.StatusCreated {
			t.Fatalf("first: %d %s", first.Code, first.Body.String())
		}
		firstID := dataMap(t, first)["id"]

		second := h.do(t, http.MethodPost, "/api/v1/drinks",
			`{"name":"soft key cola","brand":"acme","volume_ml":355,"container":"can"}`, true)
		if second.Code != http.StatusOK {
			t.Fatalf("second: %d %s", second.Code, second.Body.String())
		}
		secondData := dataMap(t, second)
		if secondData["id"] != firstID || secondData["message"] != "existing equivalent drink returned" {
			t.Fatalf("second data = %+v, want existing %v", secondData, firstID)
		}
	})

	t.Run("soft key normalizes an unsupported container", func(t *testing.T) {
		first := h.do(t, http.MethodPost, "/api/v1/drinks",
			`{"name":"Odd Container Cola","brand":"Acme","volume_ml":355,"container":"bottle"}`, true)
		if first.Code != http.StatusCreated {
			t.Fatalf("first: %d %s", first.Code, first.Body.String())
		}
		firstID := dataMap(t, first)["id"]

		second := h.do(t, http.MethodPost, "/api/v1/drinks",
			`{"name":"Odd Container Cola","brand":"Acme","volume_ml":355,"container":"can"}`, true)
		if second.Code != http.StatusOK {
			t.Fatalf("second: %d %s", second.Code, second.Body.String())
		}
		secondData := dataMap(t, second)
		if secondData["id"] != firstID || secondData["message"] != "existing equivalent drink returned" {
			t.Fatalf("second data = %+v, want existing %v", secondData, firstID)
		}
	})

	t.Run("malformed barcode is rejected", func(t *testing.T) {
		rec := h.do(t, http.MethodPost, "/api/v1/drinks",
			`{"name":"Bad Barcode Cola","brand":"Acme","volume_ml":330,"container":"can","barcode":"123"}`, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if code := apiErrorCode(rec.Body.Bytes()); code != "invalid_barcode" {
			t.Fatalf("error code = %q, want invalid_barcode", code)
		}
	})
}

func TestBarcodeUnknownWhenOFFDisabled(t *testing.T) {
	h := newHarness(t)
	if h.server.Cfg.OFFEnabled {
		t.Fatal("test expectation: harness must have Open Food Facts disabled")
	}

	rec := h.do(t, http.MethodGet, "/api/v1/barcodes/5449000000996", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown barcode: %d %s", rec.Code, rec.Body.String())
	}
	if status := dataMap(t, rec)["status"]; status != "unknown" {
		t.Fatalf("status = %v, want unknown", status)
	}

	bad := h.do(t, http.MethodGet, "/api/v1/barcodes/not-a-barcode", "", true)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("malformed barcode: %d %s", bad.Code, bad.Body.String())
	}
	if code := apiErrorCode(bad.Body.Bytes()); code != "invalid_barcode" {
		t.Fatalf("error code = %q, want invalid_barcode", code)
	}
}
