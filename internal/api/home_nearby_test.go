package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/venue"
	"sodapop/internal/checkins"
	"sodapop/internal/geocode"
)

// homeVenue fetches the seeded shared @home row.
func homeVenue(t *testing.T, h *harness) *ent.Venue {
	t.Helper()
	v, err := h.server.DB.Client.Venue.Query().Where(venue.Slug("at-home")).Only(context.Background())
	if err != nil {
		t.Fatalf("home venue: %v", err)
	}
	return v
}

// checkinCoords reads the stored coordinate columns directly, which is the
// privacy claim the API can only imply.
func checkinCoords(t *testing.T, h *harness, id string) (sql.NullFloat64, sql.NullFloat64) {
	t.Helper()
	var lat, lng sql.NullFloat64
	if err := h.server.DB.SQL.QueryRowContext(context.Background(),
		`SELECT lat, lng FROM checkins WHERE id = ?`, id).Scan(&lat, &lng); err != nil {
		t.Fatalf("read coords: %v", err)
	}
	return lat, lng
}

func assertNoCoords(t *testing.T, label string, body map[string]any) {
	t.Helper()
	if _, ok := body["lat"]; ok {
		t.Fatalf("%s leaked lat: %v", label, body)
	}
	if _, ok := body["lng"]; ok {
		t.Fatalf("%s leaked lng: %v", label, body)
	}
}

func TestAtHomeCheckinStoresNoCoordinatesAndNeverLeaksThem(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	drink := newDrink(t, h, "Home Cola", "home-cola", nil)

	rec := h.do(t, http.MethodPost, "/api/v1/checkins",
		`{"drink_id":`+strconv.Itoa(drink.ID)+`,"at_home":true,"visibility":"public"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	created := dataMap(t, rec)
	if created["at_home"] != true {
		t.Fatalf("create at_home = %v", created["at_home"])
	}
	if created["venue_id"] == nil {
		t.Fatalf("create missing venue_id for @home: %v", created)
	}
	assertNoCoords(t, "create", created)
	id, _ := created["id"].(string)

	lat, lng := checkinCoords(t, h, id)
	if lat.Valid || lng.Valid {
		t.Fatalf("at-home row stored coordinates: lat=%v lng=%v", lat, lng)
	}

	rec = h.do(t, http.MethodGet, "/api/v1/checkins/"+id, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body.String())
	}
	detail := dataMap(t, rec)
	if detail["at_home"] != true {
		t.Fatalf("detail at_home = %v", detail["at_home"])
	}
	assertNoCoords(t, "detail", detail)

	rec = h.do(t, http.MethodGet, "/api/v1/feed", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("feed: %d %s", rec.Code, rec.Body.String())
	}
	var feed struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
		t.Fatalf("feed decode: %v", err)
	}
	found := false
	for _, item := range feed.Data {
		if item["id"] != id {
			continue
		}
		found = true
		if item["at_home"] != true {
			t.Fatalf("feed at_home = %v", item["at_home"])
		}
		assertNoCoords(t, "feed", item)
	}
	if !found {
		t.Fatalf("at-home check-in missing from feed")
	}

	rec = h.do(t, http.MethodGet, "/api/v1/public/feed", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("public feed: %d %s", rec.Code, rec.Body.String())
	}
	var public struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &public); err != nil {
		t.Fatalf("public decode: %v", err)
	}
	found = false
	for _, item := range public.Data {
		if item["id"] != id {
			continue
		}
		found = true
		if item["at_home"] != true {
			t.Fatalf("public at_home = %v", item["at_home"])
		}
		assertNoCoords(t, "public feed", item)
	}
	if !found {
		t.Fatalf("@home public check-in missing from public feed")
	}

	rec = h.do(t, http.MethodGet, "/api/v1/public/checkins/"+id, "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("public checkin: %d %s", rec.Code, rec.Body.String())
	}
	assertNoCoords(t, "public checkin", dataMap(t, rec))

	_ = ctx
}

func TestAtHomeAndVenueIsRejected(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Both Cola", "both-cola", nil)
	v, err := h.server.DB.Client.Venue.Create().SetName("Somewhere").SetSlug("somewhere").Save(context.Background())
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	rec := h.do(t, http.MethodPost, "/api/v1/checkins",
		`{"drink_id":`+strconv.Itoa(drink.ID)+`,"venue_id":`+strconv.Itoa(v.ID)+`,"at_home":true}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Error.Code != "invalid_input" {
		t.Fatalf("code = %q, want invalid_input: %s", payload.Error.Code, rec.Body.String())
	}
}

func TestHomeVenueIsNeverDiscoveredSavedOrCounted(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	home := homeVenue(t, h)
	drink := newDrink(t, h, "Hidden Cola", "hidden-cola", nil)

	// Search by the marker's name never returns it.
	rec := h.do(t, http.MethodGet, "/api/v1/venues/search?q=home", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	for _, v := range dataMaps(t, rec) {
		if v["slug"] == "at-home" || v["at_home"] == true {
			t.Fatalf("venue search returned @home: %v", v)
		}
	}

	// Saving it is refused and it never appears in the saved list.
	rec = h.do(t, http.MethodPost, "/api/v1/venues/"+strconv.Itoa(home.ID)+"/save", "", true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("save @home status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodGet, "/api/v1/venues/saved", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("saved: %d %s", rec.Code, rec.Body.String())
	}
	for _, v := range dataMaps(t, rec) {
		if v["slug"] == "at-home" {
			t.Fatalf("saved list returned @home: %v", v)
		}
	}

	// An @home check-in adds no venue visit, venue kind or badge progress.
	if _, _, err := h.server.Checkins.Create(ctx, mustUser(t, h, h.alice), checkins.CreateInput{DrinkID: drink.ID, AtHome: true}); err != nil {
		t.Fatalf("create home checkin: %v", err)
	}
	stats, err := h.server.Checkins.ProfileStats(ctx, h.alice, h.alice)
	if err != nil {
		t.Fatalf("profile stats: %v", err)
	}
	if stats.Venues != 0 {
		t.Fatalf("profile venue count = %d, want 0", stats.Venues)
	}
	passport, err := h.server.Checkins.Passport(ctx, h.alice)
	if err != nil {
		t.Fatalf("passport: %v", err)
	}
	if passport.Totals.VenueKinds != 0 || len(passport.VenueKinds) != 0 {
		t.Fatalf("passport venue kinds = %d/%v, want zero", passport.Totals.VenueKinds, passport.VenueKinds)
	}
	progress, err := h.server.Badges.Progress(ctx, h.alice)
	if err != nil {
		t.Fatalf("badge progress: %v", err)
	}
	if progress["distinct_venue_kinds"] != 0 {
		t.Fatalf("badge venue progress = %v, want 0", progress["distinct_venue_kinds"])
	}
}

func TestVenueSearchExternalConsentAndFailures(t *testing.T) {
	var calls atomic.Int32
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"features":[{"geometry":{"coordinates":[-0.1201,51.5001]},"properties":{"osm_type":"N","osm_id":42,"name":"Photon Cafe","city":"London","country":"United Kingdom","osm_value":"cafe"}}]}`))
	}))
	defer stub.Close()

	h := newHarness(t)
	h.server.Nearby = geocode.New(true, stub.URL, "Sodapop/test", time.Second)

	// No consent: rejected before any provider call.
	rec := h.do(t, http.MethodGet, "/api/v1/venues/search?lat=51.5&lng=-0.12&source=external", "", true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no consent status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("provider calls without consent = %d, want 0", got)
	}

	// Consent: external rows carry no local id.
	rec = h.do(t, http.MethodGet, "/api/v1/venues/search?lat=51.5&lng=-0.12&source=external&consent=true", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("external search status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
	rows := dataMaps(t, rec)
	if len(rows) != 1 || rows[0]["source"] != "external" || rows[0]["provider"] != "photon" {
		t.Fatalf("external rows = %v", rows)
	}
	if _, ok := rows[0]["id"]; ok {
		t.Fatalf("external row exposed a selectable id: %v", rows[0])
	}

	// Disabled: local rows only, no provider call, status unavailable.
	h.server.Nearby = geocode.New(false, stub.URL, "Sodapop/test", time.Second)
	before := calls.Load()
	rec = h.do(t, http.MethodGet, "/api/v1/venues/search?lat=51.5&lng=-0.12&source=all&consent=true", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("disabled search status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := calls.Load(); got != before {
		t.Fatalf("disabled provider calls = %d, want %d", got, before)
	}
	assertExternalStatus(t, rec, "unavailable")
}

func TestVenueSearchProviderFailureKeepsLocalRows(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream down", http.StatusInternalServerError)
	}))
	defer stub.Close()

	h := newHarness(t)
	v, err := h.server.DB.Client.Venue.Create().
		SetName("Local Corner").SetSlug("local-corner").SetLat(51.5001).SetLng(-0.1201).
		Save(context.Background())
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	h.server.Nearby = geocode.New(true, stub.URL, "Sodapop/test", time.Second)

	rec := h.do(t, http.MethodGet, "/api/v1/venues/search?lat=51.5&lng=-0.12&source=all&consent=true", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	rows := dataMaps(t, rec)
	if len(rows) != 1 || rows[0]["id"] == nil {
		t.Fatalf("local row missing after provider failure: %v", rows)
	}
	if strconv.Itoa(int(rows[0]["id"].(float64))) != strconv.Itoa(v.ID) {
		t.Fatalf("local row id = %v, want %d", rows[0]["id"], v.ID)
	}
	assertExternalStatus(t, rec, "unavailable")
}

func assertExternalStatus(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var payload struct {
		ExternalStatus string `json:"external_status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode external_status: %v", err)
	}
	if payload.ExternalStatus != want {
		t.Fatalf("external_status = %q, want %q: %s", payload.ExternalStatus, want, rec.Body.String())
	}
}

func dataMaps(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode data list: %v (%s)", err, rec.Body.String())
	}
	return payload.Data
}
