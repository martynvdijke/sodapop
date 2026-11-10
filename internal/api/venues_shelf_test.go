package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"sodapop/ent"
	"sodapop/internal/venues"
)

// shelfVenue creates a fresh venue owned by alice and returns it.
func shelfVenue(t *testing.T, h *harness, name string) *ent.Venue {
	t.Helper()
	v, _, err := h.server.Venues.Create(context.Background(), h.alice, venues.CreateInput{
		Name: name, Kind: "shop", City: "Leeds",
	})
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	return v
}

func shelfPath(id int) string { return "/api/v1/venues/" + strconv.Itoa(id) + "/shelf" }

func TestVenueShelfEmpty(t *testing.T) {
	h := newHarness(t)
	v := shelfVenue(t, h, "Empty Shelf Shop")

	rec := h.do(t, http.MethodGet, shelfPath(v.ID), "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("shelf: got %d body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !json.Valid([]byte(got)) {
		t.Fatalf("invalid JSON: %s", got)
	}
	body := decodeBody(t, rec)
	rows, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array (must not be null): %s", rec.Body.String())
	}
	if len(rows) != 0 {
		t.Fatalf("empty venue shelf returned %d rows", len(rows))
	}
	meta, _ := body["meta"].(map[string]any)
	if meta["count"].(float64) != 0 {
		t.Fatalf("meta.count = %v, want 0", meta["count"])
	}
}

func TestVenueShelfReportAndRetract(t *testing.T) {
	h := newHarness(t)
	v := shelfVenue(t, h, "Stocked Shop")
	d := newDrink(t, h, "Shelf Cola", "shelf-cola", nil)

	rec := h.do(t, http.MethodPost, shelfPath(v.ID),
		`{"drink_slug":"`+d.Slug+`","price_cents":150,"note":"bottom shelf"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("report: got %d body %s", rec.Code, rec.Body.String())
	}
	entry := dataMap(t, rec)
	reportID, _ := entry["report_id"].(string)
	if reportID == "" {
		t.Fatalf("created report has no report_id: %s", rec.Body.String())
	}
	if entry["mine"] != true {
		t.Fatalf("created entry is not mine: %+v", entry)
	}
	if entry["price_cents"].(float64) != 150 {
		t.Fatalf("price_cents = %v, want 150", entry["price_cents"])
	}
	if entry["source"] != "report" || entry["band"] != "fresh" {
		t.Fatalf("entry = %+v, want source report / band fresh", entry)
	}

	rec = h.do(t, http.MethodGet, shelfPath(v.ID), "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("shelf after report: got %d", rec.Code)
	}
	rows := dataList(t, rec)
	if len(rows) != 1 {
		t.Fatalf("shelf rows = %d, want 1", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["mine"] != true {
		t.Fatalf("shelf row is not mine: %+v", row)
	}
	if row["price_cents"].(float64) != 150 {
		t.Fatalf("shelf price_cents = %v, want 150", row["price_cents"])
	}
	if row["report_id"] != reportID {
		t.Fatalf("shelf report_id = %v, want %s", row["report_id"], reportID)
	}

	rec = h.do(t, http.MethodDelete, shelfPath(v.ID)+"/"+reportID, "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("retract: got %d body %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("retract returned a body: %s", rec.Body.String())
	}

	rec = h.do(t, http.MethodDelete, shelfPath(v.ID)+"/"+reportID, "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second retract: got %d, want 404", rec.Code)
	}

	rec = h.do(t, http.MethodGet, shelfPath(v.ID), "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("shelf after retract: got %d", rec.Code)
	}
	if rows := dataList(t, rec); len(rows) != 0 {
		t.Fatalf("retracted report still on shelf: %+v", rows)
	}
}

func TestVenueShelfUnknownDrink(t *testing.T) {
	h := newHarness(t)
	v := shelfVenue(t, h, "No Such Drink Shop")

	rec := h.do(t, http.MethodPost, shelfPath(v.ID), `{"drink_slug":"not-a-real-drink"}`, true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown drink: got %d body %s, want 404", rec.Code, rec.Body.String())
	}
}

func TestVenueShelfPriceOutOfRange(t *testing.T) {
	h := newHarness(t)
	v := shelfVenue(t, h, "Pricey Shop")
	d := newDrink(t, h, "Price Cola", "price-cola", nil)

	rec := h.do(t, http.MethodPost, shelfPath(v.ID),
		`{"drink_slug":"`+d.Slug+`","price_cents":100001}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("price 100001: got %d body %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestVenueShelfCannotRetractAnotherMembersReport(t *testing.T) {
	h := newHarness(t)
	v := shelfVenue(t, h, "Shared Shop")
	d := newDrink(t, h, "Shared Cola", "shared-cola", nil)
	bobToken := h.tokenFor(t, h.bob)

	rec := h.doWith(t, bobToken, http.MethodPost, shelfPath(v.ID), `{"drink_slug":"`+d.Slug+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bob report: got %d body %s", rec.Code, rec.Body.String())
	}
	reportID, _ := dataMap(t, rec)["report_id"].(string)
	if reportID == "" {
		t.Fatalf("bob report has no id: %s", rec.Body.String())
	}

	rec = h.do(t, http.MethodDelete, shelfPath(v.ID)+"/"+reportID, "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("alice retracting bob's report: got %d, want 404", rec.Code)
	}

	// Bob's report is still live and Alice's view does not claim it.
	rec = h.do(t, http.MethodGet, shelfPath(v.ID), "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("shelf: got %d", rec.Code)
	}
	rows := dataList(t, rec)
	if len(rows) != 1 {
		t.Fatalf("shelf rows = %d, want bob's one report", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["mine"] != false || row["report_id"] != nil {
		t.Fatalf("alice sees bob's report as hers: %+v", row)
	}
}

func TestVenueShelfRequiresAuth(t *testing.T) {
	h := newHarness(t)
	v := shelfVenue(t, h, "Private Shop")

	rec := h.do(t, http.MethodGet, shelfPath(v.ID), "", false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous shelf: got %d, want 401", rec.Code)
	}
}

func TestVenueShelfReportNeedsWriteScope(t *testing.T) {
	h := newHarness(t)
	v := shelfVenue(t, h, "Read Only Shop")
	d := newDrink(t, h, "Scope Cola", "scope-cola-shelf", nil)
	readToken, _, err := h.server.Auth.CreateAPIToken(context.Background(), h.alice, "read only", []string{"read"})
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	rec := h.doWith(t, readToken, http.MethodPost, shelfPath(v.ID), `{"drink_slug":"`+d.Slug+`"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read-only report: got %d body %s, want 403", rec.Code, rec.Body.String())
	}

	// Reads are fine with the read scope.
	rec = h.doWith(t, readToken, http.MethodGet, shelfPath(v.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("read-only shelf: got %d, want 200", rec.Code)
	}
}

func TestVenueDetailEmbedsShelf(t *testing.T) {
	h := newHarness(t)
	v := shelfVenue(t, h, "Detail Shop")
	d := newDrink(t, h, "Detail Cola", "detail-cola", nil)

	rec := h.do(t, http.MethodPost, shelfPath(v.ID), `{"drink_slug":"`+d.Slug+`","price_cents":99}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("report: got %d body %s", rec.Code, rec.Body.String())
	}

	rec = h.do(t, http.MethodGet, "/api/v1/venues/"+strconv.Itoa(v.ID), "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("venue detail: got %d body %s", rec.Code, rec.Body.String())
	}
	shelf, ok := dataMap(t, rec)["shelf"].([]any)
	if !ok {
		t.Fatalf("venue detail has no shelf array: %s", rec.Body.String())
	}
	if len(shelf) != 1 {
		t.Fatalf("venue detail shelf rows = %d, want 1", len(shelf))
	}
	if shelf[0].(map[string]any)["drink_slug"] != d.Slug {
		t.Fatalf("venue detail shelf = %+v", shelf[0])
	}
}
