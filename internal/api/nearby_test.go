package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"sodapop/ent"
	"sodapop/internal/checkins"
)

func nearbyTestDrink(t *testing.T, h *harness, slug string) *ent.Drink {
	t.Helper()
	d, err := h.server.DB.Client.Drink.Create().SetName(slug).SetSlug(slug).Save(context.Background())
	if err != nil {
		t.Fatalf("drink %s: %v", slug, err)
	}
	return d
}

func nearbyTestVenue(t *testing.T, h *harness, slug string, lat, lng float64) *ent.Venue {
	t.Helper()
	v, err := h.server.DB.Client.Venue.Create().
		SetName(slug).SetSlug(slug).SetLat(lat).SetLng(lng).Save(context.Background())
	if err != nil {
		t.Fatalf("venue %s: %v", slug, err)
	}
	return v
}

func nearbyTestCheckin(t *testing.T, h *harness, owner *ent.User, drink *ent.Drink, venue *ent.Venue, visibility string) *ent.Checkin {
	t.Helper()
	venueID := venue.ID
	c, _, err := h.server.Checkins.Create(context.Background(), owner, checkins.CreateInput{
		DrinkID: drink.ID, VenueID: &venueID, Visibility: visibility,
	})
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func decodeNearby(t *testing.T, body []byte) ([]map[string]any, struct {
	Count      int    `json:"count"`
	HasMore    bool   `json:"has_more"`
	NextBefore string `json:"next_before"`
}) {
	t.Helper()
	var payload struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			Count      int    `json:"count"`
			HasMore    bool   `json:"has_more"`
			NextBefore string `json:"next_before"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode nearby: %v (%s)", err, string(body))
	}
	return payload.Data, payload.Meta
}

func TestNearbyFeedReturnsOwnAndFriendRowsWithDistance(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	drink := nearbyTestDrink(t, h, "nearby-cola")
	near := nearbyTestVenue(t, h, "near", 51.500, -0.120)
	friendVenue := nearbyTestVenue(t, h, "friend-venue", 51.5011, -0.120)

	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)
	charlie, err := h.server.DB.Client.User.Create().
		SetHandle("charlie").SetEmail("charlie@example.com").SetDisplayName("Charlie").Save(ctx)
	if err != nil {
		t.Fatalf("charlie: %v", err)
	}

	ownPrivate := nearbyTestCheckin(t, h, alice, drink, near, "private")
	friendVisible := nearbyTestCheckin(t, h, bob, drink, friendVenue, "friends")
	strangerPrivate := nearbyTestCheckin(t, h, charlie, drink, near, "private")
	strangerPublic := nearbyTestCheckin(t, h, charlie, drink, friendVenue, "public")

	f, err := h.server.Social.Request(ctx, h.alice, "bob")
	if err != nil || f == nil {
		t.Fatalf("friend request: %v", err)
	}
	if err := h.server.Social.Accept(ctx, h.bob, strconv.Itoa(f.ID)); err != nil {
		t.Fatalf("accept: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/feed/nearby?lat=51.5&lng=-0.12&radius=2&limit=20", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("nearby: got %d body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", got)
	}

	data, meta := decodeNearby(t, rec.Body.Bytes())
	if meta.Count != len(data) {
		t.Errorf("meta.count = %d, len(data) = %d", meta.Count, len(data))
	}
	if meta.HasMore || meta.NextBefore != "" {
		t.Errorf("meta.has_more=%v next_before=%q, want false/empty", meta.HasMore, meta.NextBefore)
	}

	byID := map[string]map[string]any{}
	for _, item := range data {
		id, _ := item["id"].(string)
		byID[id] = item
		if _, ok := item["lat"]; ok {
			t.Errorf("response echoes lat: %v", item)
		}
		if _, ok := item["lng"]; ok {
			t.Errorf("response echoes lng: %v", item)
		}
	}
	own, ok := byID[ownPrivate.ID]
	if !ok {
		t.Fatalf("own private check-in missing: %v", data)
	}
	if got, _ := own["distance_m"].(float64); got != 0 {
		t.Errorf("own distance_m = %v, want 0", own["distance_m"])
	}
	friend, ok := byID[friendVisible.ID]
	if !ok {
		t.Fatalf("friend visible check-in missing: %v", data)
	}
	if got, _ := friend["distance_m"].(float64); got != 100 {
		t.Errorf("friend distance_m = %v, want 100", friend["distance_m"])
	}
	if _, ok := byID[strangerPrivate.ID]; ok {
		t.Errorf("stranger private check-in leaked")
	}
	if _, ok := byID[strangerPublic.ID]; ok {
		t.Errorf("stranger public check-in leaked")
	}
	if len(data) != 2 {
		t.Errorf("got %d rows, want 2", len(data))
	}
}

func TestNearbyFeedRejectsInvalidInput(t *testing.T) {
	h := newHarness(t)
	tests := []string{
		"?lng=-0.12",
		"?lat=51.5",
		"?lat=abc&lng=-0.12",
		"?lat=NaN&lng=-0.12",
		"?lat=91&lng=0",
		"?lat=0&lng=181",
		"?lat=51.5&lng=-0.12&radius=0",
		"?lat=51.5&lng=-0.12&radius=26",
		"?lat=51.5&lng=-0.12&radius=abc",
		"?lat=51.5&lng=-0.12&limit=abc",
		"?lat=51.5&lng=-0.12&limit=0",
		"?lat=51.5&lng=-0.12&limit=999",
		"?lat=51.5&lng=-0.12&before=notacursor",
	}
	for _, query := range tests {
		rec := h.do(t, http.MethodGet, "/api/v1/feed/nearby"+query, "", true)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (%s)", query, rec.Code, rec.Body.String())
			continue
		}
		var payload struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Errorf("%s: decode: %v", query, err)
			continue
		}
		if payload.Error.Code != "invalid_request" {
			t.Errorf("%s: code = %q, want invalid_request", query, payload.Error.Code)
		}
	}
}

func TestNearbyFeedRateLimitedPerUser(t *testing.T) {
	h := newHarness(t)
	path := "/api/v1/feed/nearby?lat=51.5&lng=-0.12"
	for i := 0; i < 30; i++ {
		rec := h.do(t, http.MethodGet, path, "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200 (%s)", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := h.do(t, http.MethodGet, path, "", true)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 31: got %d, want 429 (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
	if !containsCode(rec.Body.Bytes(), "rate_limited") {
		t.Errorf("body = %s, want rate_limited", rec.Body.String())
	}
}

func TestNearbyFeedPaginationMetadata(t *testing.T) {
	h := newHarness(t)
	alice := mustUser(t, h, h.alice)
	drink := nearbyTestDrink(t, h, "paged-cola")
	offsets := []float64{0, 0.0018, 0.0036}
	want := make([]string, 0, len(offsets))
	for i, offset := range offsets {
		v := nearbyTestVenue(t, h, "paged-"+strconv.Itoa(i), 51.5+offset, -0.12)
		c := nearbyTestCheckin(t, h, alice, drink, v, "public")
		want = append(want, c.ID)
	}

	first := h.do(t, http.MethodGet, "/api/v1/feed/nearby?lat=51.5&lng=-0.12&radius=2&limit=2", "", true)
	if first.Code != http.StatusOK {
		t.Fatalf("first page: got %d (%s)", first.Code, first.Body.String())
	}
	data, meta := decodeNearby(t, first.Body.Bytes())
	if len(data) != 2 {
		t.Fatalf("first page len = %d, want 2", len(data))
	}
	if !meta.HasMore {
		t.Fatalf("first page has_more = false, want true")
	}
	if meta.NextBefore == "" {
		t.Fatalf("first page next_before is empty")
	}
	seen := map[string]bool{}
	for _, item := range data {
		id, _ := item["id"].(string)
		if seen[id] {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = true
	}

	second := h.do(t, http.MethodGet, "/api/v1/feed/nearby?lat=51.5&lng=-0.12&radius=2&limit=2&before="+meta.NextBefore, "", true)
	if second.Code != http.StatusOK {
		t.Fatalf("second page: got %d (%s)", second.Code, second.Body.String())
	}
	data2, meta2 := decodeNearby(t, second.Body.Bytes())
	if len(data2) != 1 {
		t.Fatalf("second page len = %d, want 1", len(data2))
	}
	if meta2.HasMore || meta2.NextBefore != "" {
		t.Errorf("second page has_more=%v next_before=%q, want false/empty", meta2.HasMore, meta2.NextBefore)
	}
	for _, item := range data2 {
		id, _ := item["id"].(string)
		if seen[id] {
			t.Fatalf("duplicate id %s across pages", id)
		}
		seen[id] = true
	}
	if len(seen) != len(want) {
		t.Errorf("paged ids = %d, want %d", len(seen), len(want))
	}
	for _, id := range want {
		if !seen[id] {
			t.Errorf("missing id %s across pages", id)
		}
	}
}
