package api

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"sodapop/ent/drink"
)

func TestCreateDrinkExposesBeverageTypeAndABV(t *testing.T) {
	h := newHarness(t)

	rec := h.do(t, http.MethodPost, "/api/v1/drinks",
		`{"name":"Test Coffee","brand":"Test Roaster","volume_ml":250,"container":"glass","beverage_type":"coffee","abv_percent":0}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	if data["beverage_type"] != "coffee" {
		t.Errorf("create beverage_type = %v, want coffee", data["beverage_type"])
	}
	if data["abv_percent"] != float64(0) {
		t.Errorf("create abv_percent = %v, want 0", data["abv_percent"])
	}

	id, ok := data["id"].(float64)
	if !ok {
		t.Fatalf("id = %v, want a number", data["id"])
	}
	// A newly created drink is provisional and unpublished; publish it so the
	// public fact sheet will serve it.
	if err := h.server.DB.Client.Drink.UpdateOneID(int(id)).
		SetStatus(drink.StatusActive).
		SetPublished(true).
		Exec(context.Background()); err != nil {
		t.Fatalf("publish: %v", err)
	}

	slug, _ := data["slug"].(string)
	public := h.do(t, http.MethodGet, "/api/v1/public/drinks/"+slug, "", false)
	if public.Code != http.StatusOK {
		t.Fatalf("public: %d %s", public.Code, public.Body.String())
	}
	publicData := dataMap(t, public)
	if publicData["beverage_type"] != "coffee" {
		t.Errorf("public beverage_type = %v, want coffee", publicData["beverage_type"])
	}
	if publicData["abv_percent"] != float64(0) {
		t.Errorf("public abv_percent = %v, want 0", publicData["abv_percent"])
	}
}

func TestCreateCheckinStoresPreparation(t *testing.T) {
	h := newHarness(t)
	d := newDrink(t, h, "Prep Coffee", "prep-coffee", nil)

	rec := h.do(t, http.MethodPost, "/api/v1/checkins",
		`{"drink_id":`+strconv.Itoa(d.ID)+`,"prep_method":"pour_over","prep_note":"18g / 300ml"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	created := dataMap(t, rec)
	if created["prep_method"] != "pour_over" {
		t.Errorf("create prep_method = %v, want pour_over", created["prep_method"])
	}
	if created["prep_note"] != "18g / 300ml" {
		t.Errorf("create prep_note = %v", created["prep_note"])
	}

	id, _ := created["id"].(string)
	got := h.do(t, http.MethodGet, "/api/v1/checkins/"+id, "", true)
	if got.Code != http.StatusOK {
		t.Fatalf("get: %d %s", got.Code, got.Body.String())
	}
	detail := dataMap(t, got)
	if detail["prep_method"] != "pour_over" || detail["prep_note"] != "18g / 300ml" {
		t.Errorf("detail prep = %v / %v, want pour_over / 18g / 300ml", detail["prep_method"], detail["prep_note"])
	}
}

func TestCreateCheckinUnknownPrepFallsBackAndTruncatesNote(t *testing.T) {
	h := newHarness(t)
	d := newDrink(t, h, "Fallback Tea", "fallback-tea", nil)

	long := ""
	for i := 0; i < 300; i++ {
		long += "é"
	}
	rec := h.do(t, http.MethodPost, "/api/v1/checkins",
		`{"drink_id":`+strconv.Itoa(d.ID)+`,"prep_method":"telepathy","prep_note":"`+long+`"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	if data["prep_method"] != "none" {
		t.Errorf("prep_method = %v, want none", data["prep_method"])
	}
	note, _ := data["prep_note"].(string)
	if got := len([]rune(note)); got != 280 {
		t.Errorf("prep_note length = %d runes, want 280", got)
	}
}
