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

	"sodapop/ent/checkin"
)

func doAvatarUpload(t *testing.T, h *harness, field, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if data != nil {
		fw, err := mw.CreateFormFile(field, filename)
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatalf("write image: %v", err)
		}
	} else if err := mw.WriteField(field, "not-a-file"); err != nil {
		t.Fatalf("field: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/avatar", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.token)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func makeProfilePublic(t *testing.T, h *harness, userID string) {
	t.Helper()
	prefs, err := h.server.Auth.GetOrCreateSettings(context.Background(), userID)
	if err != nil {
		t.Fatalf("settings %s: %v", userID, err)
	}
	if _, err := prefs.Update().SetPublicProfile(true).Save(context.Background()); err != nil {
		t.Fatalf("public %s: %v", userID, err)
	}
}

func TestAvatarUploadExposesURLThenDeleteClearsIt(t *testing.T) {
	h := newHarness(t)

	rec := doAvatarUpload(t, h, "avatar", "me.jpg", jpegBytes(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var uploaded struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.AvatarURL == "" || uploaded.AvatarURL == "/api/v1/media/" {
		t.Fatalf("avatar_url = %q", uploaded.AvatarURL)
	}

	// The stored row belongs to the token holder.
	mediaID := uploaded.AvatarURL[len("/api/v1/media/"):]
	var owner string
	if err := h.server.DB.SQL.QueryRow(`SELECT user_id FROM media WHERE id = ?`, mediaID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != h.alice {
		t.Fatalf("media owner = %q, want alice", owner)
	}

	// /me and /me/settings show the authenticated media route.
	rec = h.do(t, http.MethodGet, "/api/v1/me", "", true)
	me := dataMap(t, rec)
	if me["avatar_url"] != uploaded.AvatarURL {
		t.Errorf("me avatar_url = %v, want %s", me["avatar_url"], uploaded.AvatarURL)
	}
	rec = h.do(t, http.MethodGet, "/api/v1/me/settings", "", true)
	if got := decodeSettings(t, rec.Body.Bytes()).Profile.AvatarURL; got == nil || *got != uploaded.AvatarURL {
		t.Errorf("settings avatar_url = %v, want %s", got, uploaded.AvatarURL)
	}

	// Another member's profile uses the same authenticated URL.
	rec = h.do(t, http.MethodGet, "/api/v1/users/alice", "", true)
	if got := dataMap(t, rec)["avatar_url"]; got != uploaded.AvatarURL {
		t.Errorf("profile avatar_url = %v, want %s", got, uploaded.AvatarURL)
	}

	// The public header uses the public media route only after opt-in.
	makeProfilePublic(t, h, h.alice)
	rec = h.do(t, http.MethodGet, "/api/v1/public/users/alice", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("public profile = %d: %s", rec.Code, rec.Body.String())
	}
	publicURL := "/api/v1/public/media/" + mediaID
	if got := dataMap(t, rec)["avatar_url"]; got != publicURL {
		t.Errorf("public avatar_url = %v, want %s", got, publicURL)
	}

	// Removing the avatar clears every view.
	rec = h.do(t, http.MethodDelete, "/api/v1/me/avatar", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodGet, "/api/v1/me", "", true)
	if got := dataMap(t, rec)["avatar_url"]; got != nil {
		t.Errorf("me avatar_url after delete = %v, want null", got)
	}
	rec = h.do(t, http.MethodGet, "/api/v1/me/settings", "", true)
	if got := decodeSettings(t, rec.Body.Bytes()).Profile.AvatarURL; got != nil {
		t.Errorf("settings avatar_url after delete = %v, want null", got)
	}
}

func TestAvatarUploadRejectsMissingBadAndOversizedFiles(t *testing.T) {
	h := newHarness(t)

	// Anonymous upload is refused before any multipart parsing.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/avatar", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous upload = %d, want 401", rec.Code)
	}

	rec = doAvatarUpload(t, h, "avatar", "note.txt", []byte("not an image"))
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("bad type = %d, want 415: %s", rec.Code, rec.Body.String())
	}

	rec = doAvatarUpload(t, h, "avatar", "missing.jpg", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-file field = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	big := append(jpegBytes(t), make([]byte, 6<<20)...)
	rec = doAvatarUpload(t, h, "avatar", "big.jpg", big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized = %d, want 413: %s", rec.Code, rec.Body.String())
	}
	if got := h.server.DB.Client.Media.Query().CountX(context.Background()); got != 0 {
		t.Fatalf("rejected uploads left %d media rows", got)
	}
}

func TestFavouriteDrinkSetExposedAndCleared(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Cherry Cola", "cherry-cola", nil)
	body := `{"drink_id":` + strconv.Itoa(drink.ID) + `}`

	rec := h.do(t, http.MethodPut, "/api/v1/me/favourite-drink", body, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("put = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		FavouriteDrink map[string]any `json:"favourite_drink"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.FavouriteDrink == nil || payload.FavouriteDrink["id"] != float64(drink.ID) ||
		payload.FavouriteDrink["slug"] != "cherry-cola" || payload.FavouriteDrink["name"] != "Cherry Cola" {
		t.Fatalf("favourite_drink = %+v", payload.FavouriteDrink)
	}
	if _, has := payload.FavouriteDrink["image_url"]; !has {
		t.Error("favourite_drink is missing image_url")
	}
	if _, has := payload.FavouriteDrink["average_rating"]; !has {
		t.Error("favourite_drink is missing average_rating")
	}

	rec = h.do(t, http.MethodGet, "/api/v1/me", "", true)
	got, _ := dataMap(t, rec)["favourite_drink"].(map[string]any)
	if got == nil || got["id"] != float64(drink.ID) {
		t.Fatalf("me favourite_drink = %+v", got)
	}

	rec = h.do(t, http.MethodGet, "/api/v1/users/alice", "", true)
	got, _ = dataMap(t, rec)["favourite_drink"].(map[string]any)
	if got == nil || got["id"] != float64(drink.ID) {
		t.Fatalf("profile favourite_drink = %+v", got)
	}

	// The public header includes it only once the profile is public.
	makeProfilePublic(t, h, h.alice)
	rec = h.do(t, http.MethodGet, "/api/v1/public/users/alice", "", false)
	got, _ = dataMap(t, rec)["favourite_drink"].(map[string]any)
	if got == nil || got["id"] != float64(drink.ID) {
		t.Fatalf("public favourite_drink = %+v", got)
	}

	rec = h.do(t, http.MethodDelete, "/api/v1/me/favourite-drink", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodGet, "/api/v1/me", "", true)
	if got := dataMap(t, rec)["favourite_drink"]; got != nil {
		t.Fatalf("favourite_drink after delete = %v, want null", got)
	}
}

func TestFavouriteDrinkValidation(t *testing.T) {
	h := newHarness(t)
	merged := newDrink(t, h, "Merged Cola", "merged-cola", nil)
	if err := h.server.DB.Client.Drink.UpdateOneID(merged.ID).
		SetStatus("merged").Exec(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"missing field", `{}`, http.StatusBadRequest},
		{"zero id", `{"drink_id":0}`, http.StatusBadRequest},
		{"unknown id", `{"drink_id":999999}`, http.StatusNotFound},
		{"merged id", `{"drink_id":` + strconv.Itoa(merged.ID) + `}`, http.StatusNotFound},
		{"bad json", `{"drink_id":"x"}`, http.StatusBadRequest},
	} {
		rec := h.do(t, http.MethodPut, "/api/v1/me/favourite-drink", tc.body, true)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d: %s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
		if rec.Code >= 400 && bytes.Contains(rec.Body.Bytes(), []byte("sql")) {
			t.Errorf("%s leaked an internal error: %s", tc.name, rec.Body.String())
		}
	}

	rec := h.do(t, http.MethodPut, "/api/v1/me/favourite-drink", `{"drink_id":1}`, false)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous put = %d, want 401", rec.Code)
	}
}

// createFilterCheckin inserts a row directly so the test can pin created_at and
// exercise the same SQL the profile reader uses.
func createFilterCheckin(t *testing.T, h *harness, userID string, drinkID, rating int, visibility, photoID, createdAt string) *checkinRow {
	t.Helper()
	create := h.server.DB.Client.Checkin.Create().
		SetUserID(userID).
		SetDrinkID(drinkID).
		SetRating(rating).
		SetVisibility(checkin.Visibility(visibility))
	if photoID != "" {
		create = create.SetPhotoMediaID(photoID)
	}
	if createdAt != "" {
		create = create.SetCreatedAt(createdAt)
	}
	row, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return &checkinRow{ID: row.ID}
}

type checkinRow struct{ ID string }

func TestMyCheckinsFiltersAndPaging(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Filter Cola", "filter-cola", nil)
	low := createFilterCheckin(t, h, h.alice, drink.ID, 8, "public", "", "2026-01-01T10:00:00Z")
	high := createFilterCheckin(t, h, h.alice, drink.ID, 16, "public", "", "2026-01-02T10:00:00Z")
	private := createFilterCheckin(t, h, h.alice, drink.ID, 20, "private", "", "2026-01-02T12:00:00Z")
	photoMedia := storeCheckinPhoto(t, h, h.alice)
	photo := createFilterCheckin(t, h, h.alice, drink.ID, 12, "public", photoMedia.ID, "2026-01-03T10:00:00Z")

	// Rating + visibility + sort.
	rec := h.do(t, http.MethodGet, "/api/v1/me/checkins?rating_min=15&visibility=public&sort=rating_high", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered = %d: %s", rec.Code, rec.Body.String())
	}
	rows := dataList(t, rec)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != high.ID {
		t.Fatalf("rating/visibility filter = %+v, want only %s", rows, high.ID)
	}
	if rows[0].(map[string]any)["serving"] != "can" {
		t.Errorf("serving = %v, want the stored default can", rows[0].(map[string]any)["serving"])
	}
	meta, _ := decodeBody(t, rec)["meta"].(map[string]any)
	if meta["page"] != float64(1) || meta["has_next"] != false {
		t.Errorf("meta = %+v", meta)
	}

	// has_photo.
	rec = h.do(t, http.MethodGet, "/api/v1/me/checkins?has_photo=1", "", true)
	rows = dataList(t, rec)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != photo.ID {
		t.Fatalf("has_photo filter = %+v, want only %s", rows, photo.ID)
	}

	// Date-only bounds: the whole of 2 January, plus the private row.
	rec = h.do(t, http.MethodGet, "/api/v1/me/checkins?from=2026-01-02&to=2026-01-02", "", true)
	rows = dataList(t, rec)
	ids := map[string]bool{}
	for _, row := range rows {
		ids[row.(map[string]any)["id"].(string)] = true
	}
	if !ids[high.ID] || !ids[private.ID] || ids[low.ID] || ids[photo.ID] {
		t.Fatalf("date filter ids = %+v", ids)
	}

	// Offset paging reports page and has_next.
	rec = h.do(t, http.MethodGet, "/api/v1/me/checkins?limit=1&page=2&sort=oldest", "", true)
	rows = dataList(t, rec)
	meta, _ = decodeBody(t, rec)["meta"].(map[string]any)
	if len(rows) != 1 || meta["page"] != float64(2) || meta["has_next"] != true {
		t.Fatalf("page 2 = %+v meta=%+v", rows, meta)
	}

	// Cursor mode still exposes next_before when more rows exist.
	rec = h.do(t, http.MethodGet, "/api/v1/me/checkins?limit=1", "", true)
	meta, _ = decodeBody(t, rec)["meta"].(map[string]any)
	if meta["next_before"] == nil || meta["next_before"] == "" {
		t.Fatalf("cursor meta = %+v, want next_before", meta)
	}
}

func TestMyCheckinsPreservesServing(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Bottle Cola", "bottle-cola", nil)
	row, err := h.server.DB.Client.Checkin.Create().
		SetUserID(h.alice).
		SetDrinkID(drink.ID).
		SetServing(checkin.ServingBottle).
		SetVisibility(checkin.VisibilityPublic).
		Save(context.Background())
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/me/checkins", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("me/checkins = %d: %s", rec.Code, rec.Body.String())
	}
	rows := dataList(t, rec)
	for _, raw := range rows {
		item := raw.(map[string]any)
		if item["id"] != row.ID {
			continue
		}
		if item["serving"] != "bottle" {
			t.Fatalf("serving = %v, want bottle", item["serving"])
		}
		return
	}
	t.Fatalf("checkin %s missing from %+v", row.ID, rows)
}

func TestMyCheckinsRejectsInvalidFilters(t *testing.T) {
	h := newHarness(t)
	for _, query := range []string{
		"sort=popular",
		"visibility=everyone",
		"rating_min=21",
		"rating_max=-1",
		"min_likes=-1",
		"has_photo=yes",
		"page=0",
		"from=02-01-2026",
		"to=not-a-date",
		"rating_min=abc",
	} {
		rec := h.do(t, http.MethodGet, "/api/v1/me/checkins?"+query, "", true)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", query, rec.Code, rec.Body.String())
		}
	}
}

func TestFriendCheckinsFiltersNeverWidenVisibility(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Friend Filter Cola", "friend-filter-cola", nil)
	public := createFilterCheckin(t, h, h.alice, drink.ID, 16, "public", "", "")
	private := createFilterCheckin(t, h, h.alice, drink.ID, 20, "private", "", "")

	f, err := h.server.Social.Request(context.Background(), h.alice, "bob")
	if err != nil || f == nil {
		t.Fatalf("friend request: %v", err)
	}
	if err := h.server.Social.Accept(context.Background(), h.bob, strconv.Itoa(f.ID)); err != nil {
		t.Fatalf("accept: %v", err)
	}
	bobToken := h.tokenFor(t, h.bob)

	rec := h.doWith(t, bobToken, http.MethodGet, "/api/v1/users/alice/checkins?visibility=private&sort=rating_high", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("friend filter = %d: %s", rec.Code, rec.Body.String())
	}
	rows := dataList(t, rec)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != public.ID {
		t.Fatalf("friend visibility filter = %+v, want only %s (private %s must stay hidden)", rows, public.ID, private.ID)
	}
}
