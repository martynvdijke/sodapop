package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkintag"
	"sodapop/ent/friendship"
	"sodapop/ent/notification"
)

func apiFriend(t *testing.T, h *harness, handle string) *ent.User {
	t.Helper()
	ctx := context.Background()
	u, err := h.server.DB.Client.User.Create().
		SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(handle).
		SetEmailVerifiedAt("2026-01-01T00:00:00Z").Save(ctx)
	if err != nil {
		t.Fatalf("user %s: %v", handle, err)
	}
	x, y := h.alice, u.ID
	if x > y {
		x, y = y, x
	}
	if _, err := h.server.DB.Client.Friendship.Create().
		SetUserA(x).SetUserB(y).SetStatus(friendship.StatusAccepted).
		SetRequestedBy(h.alice).Save(ctx); err != nil {
		t.Fatalf("friendship %s: %v", handle, err)
	}
	return u
}

func TestCreateCheckinGPSValidation(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "GPS Cola", "gps-cola", nil)
	base := `{"drink_id":` + strconv.Itoa(drink.ID)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"lat only", base + `,"lat":51.5}`, http.StatusBadRequest},
		{"lng only", base + `,"lng":-0.12}`, http.StatusBadRequest},
		{"lat out of range", base + `,"lat":90.1,"lng":0}`, http.StatusBadRequest},
		{"lng out of range", base + `,"lng":180.1,"lat":0}`, http.StatusBadRequest},
		{"valid pair", base + `,"lat":51.5,"lng":-0.12}`, http.StatusCreated},
		{"no coords", base + `}`, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, http.MethodPost, "/api/v1/checkins", tc.body, true)
			if rec.Code != tc.want {
				t.Fatalf("got %d want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestCreateCheckinFriendTagValidation(t *testing.T) {
	t.Run("accepted friend is tagged", func(t *testing.T) {
		h := newHarness(t)
		drink := newDrink(t, h, "Tag Cola", "tag-cola", nil)
		bob := apiFriend(t, h, "bobfriend")
		rec := h.do(t, http.MethodPost, "/api/v1/checkins",
			`{"drink_id":`+strconv.Itoa(drink.ID)+`,"friend_ids":["`+bob.ID+`"]}`, true)
		if rec.Code != http.StatusCreated {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		tags, _ := dataMap(t, rec)["tagged_friends"].([]any)
		if len(tags) != 1 {
			t.Fatalf("tagged_friends = %v, want one", dataMap(t, rec)["tagged_friends"])
		}
	})

	t.Run("self is ignored", func(t *testing.T) {
		h := newHarness(t)
		drink := newDrink(t, h, "Self Cola", "self-cola", nil)
		rec := h.do(t, http.MethodPost, "/api/v1/checkins",
			`{"drink_id":`+strconv.Itoa(drink.ID)+`,"friend_ids":["`+h.alice+`"]}`, true)
		if rec.Code != http.StatusCreated {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		if got := dataMap(t, rec)["tagged_friends"].([]any); len(got) != 0 {
			t.Fatalf("self tagged: %v", got)
		}
	})

	t.Run("non-friend is rejected", func(t *testing.T) {
		h := newHarness(t)
		drink := newDrink(t, h, "Stranger Cola", "stranger-cola", nil)
		rec := h.do(t, http.MethodPost, "/api/v1/checkins",
			`{"drink_id":`+strconv.Itoa(drink.ID)+`,"friend_ids":["`+h.bob+`"]}`, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("got %d want 400: %s", rec.Code, rec.Body.String())
		}
		if code := apiErrorCode(rec.Body.Bytes()); code != "invalid_friend" {
			t.Fatalf("error code = %q, want invalid_friend", code)
		}
	})

	t.Run("more than ten friends is rejected", func(t *testing.T) {
		h := newHarness(t)
		drink := newDrink(t, h, "Crowd Cola", "crowd-cola", nil)
		ids := make([]string, 0, 11)
		for i := 0; i < 11; i++ {
			ids = append(ids, apiFriend(t, h, "crowd"+strconv.Itoa(i)).ID)
		}
		raw, _ := json.Marshal(ids)
		rec := h.do(t, http.MethodPost, "/api/v1/checkins",
			`{"drink_id":`+strconv.Itoa(drink.ID)+`,"friend_ids":`+string(raw)+`}`, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("got %d want 400: %s", rec.Code, rec.Body.String())
		}
		if code := apiErrorCode(rec.Body.Bytes()); code != "invalid_friend" {
			t.Fatalf("error code = %q, want invalid_friend", code)
		}
	})
}

func TestCheckinGPSAndTagsRoundTrip(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Round Cola", "round-cola", nil)
	bob := apiFriend(t, h, "roundbob")

	rec := h.do(t, http.MethodPost, "/api/v1/checkins",
		`{"drink_id":`+strconv.Itoa(drink.ID)+`,"lat":51.5,"lng":-0.12,"friend_ids":["`+bob.ID+`"]}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	created := dataMap(t, rec)
	id, _ := created["id"].(string)
	if _, ok := created["lat"]; ok {
		t.Fatalf("create response must not include lat: %v", created)
	}
	if _, ok := created["lng"]; ok {
		t.Fatalf("create response must not include lng: %v", created)
	}
	if tags, _ := created["tagged_friends"].([]any); len(tags) != 1 {
		t.Fatalf("create tagged_friends = %v", created["tagged_friends"])
	}

	rec = h.do(t, http.MethodGet, "/api/v1/checkins/"+id, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body.String())
	}
	detail := dataMap(t, rec)
	if _, ok := detail["lat"]; ok {
		t.Fatalf("detail must not include lat: %v", detail)
	}
	if _, ok := detail["lng"]; ok {
		t.Fatalf("detail must not include lng: %v", detail)
	}
	tags := detail["tagged_friends"].([]any)
	if len(tags) != 1 {
		t.Fatalf("detail tagged_friends = %v", tags)
	}
	tag := tags[0].(map[string]any)
	if tag["handle"] != "roundbob" || tag["id"] != bob.ID {
		t.Fatalf("detail tag = %v", tag)
	}

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
		if _, ok := item["lat"]; ok {
			t.Fatalf("feed must not include lat: %v", item)
		}
		if _, ok := item["lng"]; ok {
			t.Fatalf("feed must not include lng: %v", item)
		}
		if got := item["tagged_friends"].([]any); len(got) != 1 {
			t.Fatalf("feed tagged_friends = %v", item["tagged_friends"])
		}
	}
	if !found {
		t.Fatalf("check-in missing from feed: %v", feed.Data)
	}
}

func TestCheckinMultipartRepeatedFriendIDs(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Multi Cola", "multi-cola", nil)
	bob := apiFriend(t, h, "multibob")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for key, value := range map[string]string{
		"drink_id": strconv.Itoa(drink.ID),
		"lat":      "51.5",
		"lng":      "-0.12",
	} {
		if err := mw.WriteField(key, value); err != nil {
			t.Fatalf("field %s: %v", key, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := mw.WriteField("friend_ids", bob.ID); err != nil {
			t.Fatalf("friend_ids: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/checkins", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.token)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("multipart create: %d %s", rec.Code, rec.Body.String())
	}
	created := dataMap(t, rec)
	tags, _ := created["tagged_friends"].([]any)
	if len(tags) != 1 {
		t.Fatalf("duplicate friend_ids were not deduped: %v", created["tagged_friends"])
	}
	if _, ok := created["lat"]; ok {
		t.Fatalf("multipart create must not include lat: %v", created)
	}
	if _, ok := created["lng"]; ok {
		t.Fatalf("multipart create must not include lng: %v", created)
	}
}

func TestCheckinIdempotentReplayDoesNotDuplicateTagsOrNotifications(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	drink := newDrink(t, h, "Idem Cola", "idem-cola", nil)
	bob := apiFriend(t, h, "idembob")

	mentions := 0
	h.server.Social.OnNotify = func(_ context.Context, _, kind, _, _, _ string) {
		if kind == "mention" {
			mentions++
		}
	}

	body := `{"drink_id":` + strconv.Itoa(drink.ID) + `,"idempotency_key":"loc-1","friend_ids":["` + bob.ID + `"],"lat":51.5,"lng":-0.12}`
	first := h.do(t, http.MethodPost, "/api/v1/checkins", body, true)
	if first.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	id := dataMap(t, first)["id"].(string)
	second := h.do(t, http.MethodPost, "/api/v1/checkins", body, true)
	if second.Code != http.StatusOK {
		t.Fatalf("replay: %d %s", second.Code, second.Body.String())
	}
	if dataMap(t, second)["id"] != id {
		t.Fatalf("replay returned a different row")
	}

	tags, err := h.server.DB.Client.CheckinTag.Query().Where(checkintag.CheckinID(id)).Count(ctx)
	if err != nil {
		t.Fatalf("tag count: %v", err)
	}
	if tags != 1 {
		t.Fatalf("tags = %d, want 1", tags)
	}
	notifs, err := h.server.DB.Client.Notification.Query().
		Where(notification.KindEQ(notification.KindMention), notification.SubjectID(id)).Count(ctx)
	if err != nil {
		t.Fatalf("notification count: %v", err)
	}
	if notifs != 1 {
		t.Fatalf("mention notifications = %d, want 1", notifs)
	}
	if mentions != 1 {
		t.Fatalf("OnNotify mention calls = %d, want 1", mentions)
	}
}

type venueSearchRow struct {
	ID        int  `json:"id"`
	DistanceM *int `json:"distance_m"`
}

func decodeVenueSearch(t *testing.T, body []byte) []venueSearchRow {
	t.Helper()
	var payload struct {
		Data []venueSearchRow `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode venue search: %v (%s)", err, string(body))
	}
	return payload.Data
}

func TestVenueSearchEmptyQueryNeedsGPS(t *testing.T) {
	h := newHarness(t)
	near := nearbyTestVenue(t, h, "Quiet Bar Near", 51.5005, -0.1200)
	mid := nearbyTestVenue(t, h, "Quiet Bar Mid", 51.5060, -0.1200)
	far := nearbyTestVenue(t, h, "Quiet Bar Far", 51.5150, -0.1200)

	cases := []struct {
		name      string
		path      string
		want      int
		wantIDs   []int
		wantDist  bool
		wantError string
	}{
		{
			name: "empty query with GPS returns nearest first",
			path: "/api/v1/venues/search?lat=51.5&lng=-0.12&limit=10",
			want: http.StatusOK, wantIDs: []int{near.ID, mid.ID, far.ID}, wantDist: true,
		},
		{
			name: "empty query without GPS is rejected",
			path: "/api/v1/venues/search",
			want: http.StatusBadRequest, wantError: "query_too_short",
		},
		{
			name: "one character without GPS is rejected",
			path: "/api/v1/venues/search?q=Q",
			want: http.StatusBadRequest, wantError: "query_too_short",
		},
		{
			name: "invalid latitude is rejected",
			path: "/api/v1/venues/search?lat=91&lng=-0.12",
			want: http.StatusBadRequest, wantError: "invalid_request",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, http.MethodGet, tc.path, "", true)
			if rec.Code != tc.want {
				t.Fatalf("got %d want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want != http.StatusOK {
				if code := apiErrorCode(rec.Body.Bytes()); code != tc.wantError {
					t.Fatalf("error code = %q, want %q", code, tc.wantError)
				}
				return
			}
			rows := decodeVenueSearch(t, rec.Body.Bytes())
			if len(rows) != len(tc.wantIDs) {
				t.Fatalf("rows = %+v, want %d", rows, len(tc.wantIDs))
			}
			for i, row := range rows {
				if row.ID != tc.wantIDs[i] {
					t.Fatalf("row %d = %d, want %d (rows %+v)", i, row.ID, tc.wantIDs[i], rows)
				}
				if tc.wantDist && row.DistanceM == nil {
					t.Fatalf("row %d missing distance_m", i)
				}
			}
		})
	}
}

func TestVenueSearchRadiusCapAppliesToEmptyQuery(t *testing.T) {
	h := newHarness(t)
	near := nearbyTestVenue(t, h, "Cap Bar Near", 51.5000, -0.1200)
	twentyKM := nearbyTestVenue(t, h, "Cap Bar 20km", 51.6800, -0.1200) // ~20 km north
	nearbyTestVenue(t, h, "Cap Bar 30km", 51.7700, -0.1200)             // ~30 km north

	// A huge radius is clamped to the 25 km cap, so the 30 km venue is out.
	rec := h.do(t, http.MethodGet, "/api/v1/venues/search?lat=51.5&lng=-0.12&radius=999999", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	rows := decodeVenueSearch(t, rec.Body.Bytes())
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want near + 20km only", rows)
	}
	if rows[0].ID != near.ID || rows[1].ID != twentyKM.ID {
		t.Fatalf("rows = %+v, want %d then %d", rows, near.ID, twentyKM.ID)
	}
}

func TestVenueSearchOrdersByDistanceWhenGivenGPS(t *testing.T) {
	h := newHarness(t)
	// Alphabetical order differs from distance order.
	near := nearbyTestVenue(t, h, "Geo Bar A", 51.5000, -0.1200)
	mid := nearbyTestVenue(t, h, "Geo Bar B", 51.5050, -0.1200)
	far := nearbyTestVenue(t, h, "Geo Bar C", 51.5150, -0.1200)

	rec := h.do(t, http.MethodGet, "/api/v1/venues/search?q=Geo%20Bar&lat=51.5&lng=-0.12&limit=10", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("geo search: %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []struct {
			ID        int  `json:"id"`
			DistanceM *int `json:"distance_m"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Data) != 3 {
		t.Fatalf("results = %v", payload.Data)
	}
	want := []int{near.ID, mid.ID, far.ID}
	for i, row := range payload.Data {
		if row.ID != want[i] {
			t.Fatalf("result %d = %d, want %d (order %+v)", i, row.ID, want[i], payload.Data)
		}
		if row.DistanceM == nil {
			t.Fatalf("result %d missing distance_m", i)
		}
	}
	if d := *payload.Data[0].DistanceM; d > 10 {
		t.Errorf("nearest distance_m = %d, want ~0", d)
	}

	// Without a reference point the response keeps its old shape.
	rec = h.do(t, http.MethodGet, "/api/v1/venues/search?q=Geo%20Bar&limit=10", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("plain search: %d %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"distance_m"`)) {
		t.Fatalf("plain search leaked distance_m: %s", rec.Body.String())
	}

	// A tiny radius filters out everything farther away.
	rec = h.do(t, http.MethodGet, "/api/v1/venues/search?q=Geo%20Bar&lat=51.5&lng=-0.12&radius=100", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("radius search: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode radius: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].ID != near.ID {
		t.Fatalf("radius result = %+v", payload.Data)
	}

	// Half a coordinate pair is rejected.
	rec = h.do(t, http.MethodGet, "/api/v1/venues/search?q=Geo%20Bar&lat=51.5", "", true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("lat only: got %d want 400: %s", rec.Code, rec.Body.String())
	}
}
