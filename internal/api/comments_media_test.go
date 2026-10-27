package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/notification"
	"sodapop/ent/user"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/db"
	"sodapop/internal/ratelimit"
	"sodapop/internal/venues"
)

// tokenFor issues a personal token for an existing user with the given scopes.
func tokenFor(t *testing.T, h *harness, userID string, scopes []string) string {
	t.Helper()
	tok, _, err := h.server.Auth.CreateAPIToken(context.Background(), userID, "test", scopes)
	if err != nil {
		t.Fatalf("token for %s: %v", userID, err)
	}
	return tok
}

func newUser(t *testing.T, h *harness, handle string, verified bool, role user.Role) *ent.User {
	t.Helper()
	b := h.server.DB.Client.User.Create().
		SetHandle(handle).
		SetEmail(handle + "@example.com").
		SetDisplayName(strings.ToUpper(handle[:1]) + handle[1:]).
		SetRole(role)
	if verified {
		b.SetEmailVerifiedAt(db.Now())
	}
	u, err := b.Save(context.Background())
	if err != nil {
		t.Fatalf("user %s: %v", handle, err)
	}
	return u
}

func doAs(t *testing.T, h *harness, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

// pngBytes returns a valid tiny PNG.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 20), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png: %v", err)
	}
	return buf.Bytes()
}

func doUpload(t *testing.T, h *harness, path, token, kind, label string, data []byte, filename string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if data != nil {
		fw, err := mw.CreateFormFile("image", filename)
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatalf("write image: %v", err)
		}
	}
	if kind != "" {
		_ = mw.WriteField("kind", kind)
	}
	if label != "" {
		_ = mw.WriteField("label", label)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func mustDrink(t *testing.T, h *harness) *ent.Drink {
	t.Helper()
	d, _, err := h.server.Catalog.CreateProvisional(context.Background(), h.alice, catalog.CreateInput{
		Name: "Comment Cola", Brand: "Test", Container: "can", VolumeML: 355,
	})
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	return d
}

func mustCheckin(t *testing.T, h *harness, owner *ent.User, drinkID int, visibility string) *ent.Checkin {
	t.Helper()
	c, _, err := h.server.Checkins.Create(context.Background(), owner, checkins.CreateInput{
		DrinkID: drinkID, Visibility: visibility,
	})
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func befriend(t *testing.T, h *harness, fromID string, to *ent.User) {
	t.Helper()
	ctx := context.Background()
	req, err := h.server.Social.Request(ctx, fromID, to.Handle)
	if err != nil || req == nil {
		t.Fatalf("friend request: %v", err)
	}
	if err := h.server.Social.Accept(ctx, to.ID, strconv.Itoa(req.ID)); err != nil {
		t.Fatalf("accept friend: %v", err)
	}
}

func TestCommentEndpointsAuthScopeAndVerification(t *testing.T) {
	h := newHarness(t)
	bob := mustUser(t, h, h.bob)
	c := mustCheckin(t, h, bob, mustDrink(t, h).ID, "public")
	path := "/api/v1/checkins/" + c.ID + "/comments"

	readOnly := tokenFor(t, h, h.bob, []string{"read"})
	writeOnly := tokenFor(t, h, h.bob, []string{"write"})
	unverified := newUser(t, h, "unverified", false, user.RoleUser)
	unverifiedTok := tokenFor(t, h, unverified.ID, []string{"read", "write"})

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		token  string
		want   int
	}{
		{"anonymous read", http.MethodGet, path, "", "", http.StatusUnauthorized},
		{"wrong scope read", http.MethodGet, path, "", writeOnly, http.StatusForbidden},
		{"wrong scope write", http.MethodPost, path, `{"body":"hi"}`, readOnly, http.StatusForbidden},
		{"unverified write", http.MethodPost, path, `{"body":"hi"}`, unverifiedTok, http.StatusForbidden},
		{"unverified delete", http.MethodDelete, "/api/v1/comments/x", "", unverifiedTok, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doAs(t, h, tc.method, tc.path, tc.body, tc.token)
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestCommentVisibilityMatrix(t *testing.T) {
	h := newHarness(t)
	bob := mustUser(t, h, h.bob)
	alice := mustUser(t, h, h.alice)
	carol := newUser(t, h, "carol", true, user.RoleUser)
	drinkID := mustDrink(t, h).ID
	token := tokenFor(t, h, carol.ID, []string{"read", "write"})

	befriend(t, h, h.alice, bob)

	cases := []struct {
		visibility string
		viewer     *ent.User
		wantRead   int
		wantWrite  int
	}{
		{"public", carol, http.StatusOK, http.StatusCreated},
		{"public", bob, http.StatusOK, http.StatusCreated},
		{"friends", alice, http.StatusOK, http.StatusCreated},
		{"friends", carol, http.StatusNotFound, http.StatusNotFound},
		{"private", bob, http.StatusOK, http.StatusCreated},
		{"private", alice, http.StatusNotFound, http.StatusNotFound},
		{"private", carol, http.StatusNotFound, http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.visibility+"/"+tc.viewer.Handle, func(t *testing.T) {
			c := mustCheckin(t, h, bob, drinkID, tc.visibility)
			var tok string
			if tc.viewer.ID == carol.ID {
				tok = token
			} else {
				tok = tokenFor(t, h, tc.viewer.ID, []string{"read", "write"})
			}
			read := doAs(t, h, http.MethodGet, "/api/v1/checkins/"+c.ID+"/comments", "", tok)
			if read.Code != tc.wantRead {
				t.Fatalf("read: got %d, want %d (%s)", read.Code, tc.wantRead, read.Body.String())
			}
			write := doAs(t, h, http.MethodPost, "/api/v1/checkins/"+c.ID+"/comments", `{"body":"nice"}`, tok)
			if write.Code != tc.wantWrite {
				t.Fatalf("write: got %d, want %d (%s)", write.Code, tc.wantWrite, write.Body.String())
			}
			if tc.wantRead == http.StatusOK {
				if strings.Contains(read.Body.String(), "total") {
					t.Fatalf("comment list leaks a total: %s", read.Body.String())
				}
			}
		})
	}
}

func TestCommentBodyValidationAndRateLimit(t *testing.T) {
	h := newHarness(t)
	bob := mustUser(t, h, h.bob)
	alice := mustUser(t, h, h.alice)
	c := mustCheckin(t, h, bob, mustDrink(t, h).ID, "public")
	path := "/api/v1/checkins/" + c.ID + "/comments"
	tok := tokenFor(t, h, alice.ID, []string{"read", "write"})

	for _, body := range []string{`{"body":""}`, `{"body":"   "}`, `{"body":"` + strings.Repeat("x", 1001) + `"}`} {
		rec := doAs(t, h, http.MethodPost, path, body, tok)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %.20q: got %d, want 400 (%s)", body, rec.Code, rec.Body.String())
		}
	}

	created := 0
	for i := 0; i < 30; i++ {
		rec := doAs(t, h, http.MethodPost, path, `{"body":"comment `+strconv.Itoa(i)+`"}`, tok)
		if rec.Code != http.StatusCreated {
			t.Fatalf("comment %d: got %d (%s)", i, rec.Code, rec.Body.String())
		}
		created++
	}
	over := doAs(t, h, http.MethodPost, path, `{"body":"one too many"}`, tok)
	if over.Code != http.StatusTooManyRequests {
		t.Fatalf("31st comment: got %d, want 429 (%s)", over.Code, over.Body.String())
	}
	if created != 30 {
		t.Fatalf("created %d comments", created)
	}
}

func TestCommentDeleteAuthorization(t *testing.T) {
	h := newHarness(t)
	bob := mustUser(t, h, h.bob)
	alice := mustUser(t, h, h.alice)
	admin := newUser(t, h, "mod", true, user.RoleAdmin)
	c := mustCheckin(t, h, bob, mustDrink(t, h).ID, "public")
	path := "/api/v1/checkins/" + c.ID + "/comments"

	aliceTok := tokenFor(t, h, alice.ID, []string{"read", "write"})
	bobTok := tokenFor(t, h, bob.ID, []string{"read", "write"})
	adminTok := tokenFor(t, h, admin.ID, []string{"read", "write"})

	rec := doAs(t, h, http.MethodPost, path, `{"body":"mine"}`, aliceTok)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	commentPath := "/api/v1/comments/" + payload.Data.ID

	if rec := doAs(t, h, http.MethodDelete, commentPath, "", bobTok); rec.Code != http.StatusNotFound {
		t.Fatalf("parent owner delete: got %d, want 404", rec.Code)
	}
	if rec := doAs(t, h, http.MethodDelete, "/api/v1/comments/does-not-exist", "", aliceTok); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown comment: got %d, want 404", rec.Code)
	}
	if rec := doAs(t, h, http.MethodDelete, commentPath, "", aliceTok); rec.Code != http.StatusNoContent {
		t.Fatalf("author delete: got %d, want 204 (%s)", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, h, http.MethodDelete, commentPath, "", aliceTok); rec.Code != http.StatusNotFound {
		t.Fatalf("author repeat delete: got %d, want 404", rec.Code)
	}

	// Admin can remove someone else's comment.
	rec = doAs(t, h, http.MethodPost, path, `{"body":"moderate me"}`, aliceTok)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second: got %d", rec.Code)
	}
	payload.Data.ID = ""
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if rec := doAs(t, h, http.MethodDelete, "/api/v1/comments/"+payload.Data.ID, "", adminTok); rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete: got %d, want 204 (%s)", rec.Code, rec.Body.String())
	}
}

func TestVenueMediaFlowAndAuthorization(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	carol := newUser(t, h, "carol", true, user.RoleUser)
	aliceTok := tokenFor(t, h, alice.ID, []string{"read", "write"})
	carolTok := tokenFor(t, h, carol.ID, []string{"read", "write"})
	admin := newUser(t, h, "mod", true, user.RoleAdmin)
	adminTok := tokenFor(t, h, admin.ID, []string{"read", "write"})

	v, _, err := h.server.Venues.Create(ctx, alice.ID, venues.CreateInput{Name: "Fizz Corner", Kind: "shop"})
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	mediaPath := "/api/v1/venues/" + strconv.Itoa(v.ID) + "/media"
	img := pngBytes(t)

	photo := doUpload(t, h, mediaPath, aliceTok, "photo", "Storefront", img, "photo.png")
	if photo.Code != http.StatusCreated {
		t.Fatalf("photo upload: got %d (%s)", photo.Code, photo.Body.String())
	}
	menu := doUpload(t, h, mediaPath, carolTok, "menu", "Specials", img, "menu.png")
	if menu.Code != http.StatusCreated {
		t.Fatalf("menu upload: got %d (%s)", menu.Code, menu.Body.String())
	}

	var uploaded struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(photo.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}

	list := doAs(t, h, http.MethodGet, mediaPath, "", carolTok)
	if list.Code != http.StatusOK {
		t.Fatalf("list: got %d (%s)", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), "total") || strings.Contains(list.Body.String(), "count") {
		t.Fatalf("media list leaks a total/count: %s", list.Body.String())
	}
	var listed struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Data) != 2 {
		t.Fatalf("listed %d rows, want 2", len(listed.Data))
	}

	itemPath := "/api/v1/venues/media/" + uploaded.Data.ID
	if rec := doAs(t, h, http.MethodDelete, itemPath, "", carolTok); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger delete: got %d, want 404", rec.Code)
	}
	if rec := doAs(t, h, http.MethodDelete, itemPath, "", aliceTok); rec.Code != http.StatusNoContent {
		t.Fatalf("uploader delete: got %d, want 204 (%s)", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, h, http.MethodDelete, itemPath, "", aliceTok); rec.Code != http.StatusNotFound {
		t.Fatalf("repeat delete: got %d, want 404", rec.Code)
	}

	// Admin can remove another user's active row.
	carolMenu := doUpload(t, h, mediaPath, carolTok, "photo", "", img, "second.png")
	if carolMenu.Code != http.StatusCreated {
		t.Fatalf("second upload: got %d", carolMenu.Code)
	}
	if err := json.Unmarshal(carolMenu.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if rec := doAs(t, h, http.MethodDelete, "/api/v1/venues/media/"+uploaded.Data.ID, "", adminTok); rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete: got %d, want 204", rec.Code)
	}
}

func TestVenueMediaErrorMapping(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	tok := tokenFor(t, h, alice.ID, []string{"read", "write"})
	v, _, err := h.server.Venues.Create(ctx, alice.ID, venues.CreateInput{Name: "Error Bar", Kind: "bar"})
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	mediaPath := "/api/v1/venues/" + strconv.Itoa(v.ID) + "/media"
	img := pngBytes(t)

	cases := []struct {
		name     string
		path     string
		token    string
		kind     string
		data     []byte
		filename string
		want     int
	}{
		{"unknown venue list", "/api/v1/venues/999999/media", tok, "", nil, "", http.StatusNotFound},
		{"invalid venue list", "/api/v1/venues/not-a-number/media", tok, "", nil, "", http.StatusBadRequest},
		{"unknown venue upload", "/api/v1/venues/999999/media", tok, "photo", img, "photo.png", http.StatusNotFound},
		{"missing image", mediaPath, tok, "photo", nil, "", http.StatusBadRequest},
		{"bad kind", mediaPath, tok, "poster", img, "photo.png", http.StatusBadRequest},
		{"bad type", mediaPath, tok, "photo", []byte("this is not an image"), "note.txt", http.StatusUnsupportedMediaType},
		{"unknown media delete", "/api/v1/venues/media/nope", tok, "", nil, "", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if strings.Contains(tc.name, "list") || strings.Contains(tc.name, "delete") {
				rec = doAs(t, h, http.MethodGet, tc.path, "", tc.token)
				if strings.Contains(tc.name, "delete") {
					rec = doAs(t, h, http.MethodDelete, tc.path, "", tc.token)
				}
			} else {
				rec = doUpload(t, h, tc.path, tc.token, tc.kind, "", tc.data, tc.filename)
			}
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	// Oversized but image-shaped payload: the media service rejects it at 413.
	big := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 12<<20+1024)...)
	if rec := doUpload(t, h, mediaPath, tok, "photo", "", big, "big.png"); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload: got %d, want 413 (%s)", rec.Code, rec.Body.String())
	}

	// Per-uploader cap: six active rows, the seventh is a conflict. Start with a
	// fresh limiter so the earlier validation uploads do not consume the window.
	h.server.Limiter = ratelimit.New()
	small := pngBytes(t)
	for i := 0; i < 6; i++ {
		if rec := doUpload(t, h, mediaPath, tok, "photo", "", small, "cap.png"); rec.Code != http.StatusCreated {
			t.Fatalf("cap upload %d: got %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
	if rec := doUpload(t, h, mediaPath, tok, "photo", "", small, "cap.png"); rec.Code != http.StatusConflict {
		t.Fatalf("over cap upload: got %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

func TestNewEndpointsRejectAnonymous(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/api/v1/checkins/abc/comments",
		"/api/v1/venues/1/media",
	} {
		if rec := doAs(t, h, http.MethodGet, path, "", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: got %d, want 401", path, rec.Code)
		}
	}
}

func TestVenueSaveRoutesSurviveMediaDispatch(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	tok := tokenFor(t, h, alice.ID, []string{"read", "write"})
	v, _, err := h.server.Venues.Create(ctx, alice.ID, venues.CreateInput{Name: "Saved Soda", Kind: "shop"})
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	path := "/api/v1/venues/" + strconv.Itoa(v.ID) + "/save"
	if rec := doAs(t, h, http.MethodPost, path, "", tok); rec.Code != http.StatusOK {
		t.Fatalf("save: got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doAs(t, h, http.MethodDelete, path, "", tok); rec.Code != http.StatusOK {
		t.Fatalf("unsave: got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestCommentListPagination(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	bob := mustUser(t, h, h.bob)
	c := mustCheckin(t, h, bob, mustDrink(t, h).ID, "public")
	path := "/api/v1/checkins/" + c.ID + "/comments"
	tok := tokenFor(t, h, h.alice, []string{"read"})

	const total = 120
	for i := 0; i < total; i++ {
		if _, err := h.server.DB.SQL.ExecContext(ctx,
			`INSERT INTO comments (id, checkin_id, user_id, body, created_at) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("api-comment-%03d", i), c.ID, h.alice, fmt.Sprintf("body-%03d", i),
			fmt.Sprintf("2026-02-01T00:%02d:%02dZ", i/60, i%60)); err != nil {
			t.Fatalf("insert comment %d: %v", i, err)
		}
	}

	type page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Meta struct {
			Count      int    `json:"count"`
			HasMore    bool   `json:"has_more"`
			NextBefore string `json:"next_before"`
		} `json:"meta"`
	}

	seen := map[string]bool{}
	before := ""
	for i := 0; i < 3; i++ {
		url := path + "?limit=50"
		if before != "" {
			url += "&before=" + before
		}
		rec := doAs(t, h, http.MethodGet, url, "", tok)
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d: got %d (%s)", i, rec.Code, rec.Body.String())
		}
		var p page
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("decode page %d: %v", i, err)
		}
		if p.Meta.Count != len(p.Data) {
			t.Fatalf("page %d count = %d, len = %d", i, p.Meta.Count, len(p.Data))
		}
		wantLen := 50
		wantMore := true
		if i == 2 {
			wantLen = 20
			wantMore = false
		}
		if len(p.Data) != wantLen {
			t.Fatalf("page %d len = %d, want %d", i, len(p.Data), wantLen)
		}
		if p.Meta.HasMore != wantMore {
			t.Fatalf("page %d has_more = %v, want %v", i, p.Meta.HasMore, wantMore)
		}
		for _, item := range p.Data {
			if seen[item.ID] {
				t.Fatalf("duplicate %s across pages", item.ID)
			}
			seen[item.ID] = true
		}
		if wantMore {
			if p.Meta.NextBefore != p.Data[0].ID {
				t.Fatalf("page %d next_before = %q, want %q", i, p.Meta.NextBefore, p.Data[0].ID)
			}
			before = p.Meta.NextBefore
		}
	}
	if strings.Contains(doAs(t, h, http.MethodGet, path, "", tok).Body.String(), "total") {
		t.Fatalf("pagination response leaks a total")
	}
}

func TestMentionNotificationAndHref(t *testing.T) {
	h := newHarness(t)
	bob := mustUser(t, h, h.bob)
	carol := newUser(t, h, "carol", true, user.RoleUser)
	c := mustCheckin(t, h, bob, mustDrink(t, h).ID, "public")
	tok := tokenFor(t, h, h.alice, []string{"read", "write"})

	rec := doAs(t, h, http.MethodPost, "/api/v1/checkins/"+c.ID+"/comments", `{"body":"hi @carol"}`, tok)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d (%s)", rec.Code, rec.Body.String())
	}

	rows, err := h.server.DB.Client.Notification.Query().
		Where(notification.KindEQ(notification.KindMention), notification.UserID(carol.ID)).
		All(context.Background())
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("mention notifications = %d, want 1", len(rows))
	}
	if rows[0].SubjectID != c.ID || rows[0].ActorID == nil || *rows[0].ActorID != h.alice {
		t.Fatalf("mention row = %+v", rows[0])
	}

	cases := map[string]string{
		"comment":        "/checkins/" + c.ID + "#comments",
		"mention":        "/checkins/" + c.ID + "#comments",
		"toast":          "/checkins/" + c.ID,
		"badge":          "/you",
		"friend_request": "/friends",
	}
	for kind, want := range cases {
		if got := notificationHref(kind, c.ID); got != want {
			t.Fatalf("notificationHref(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestOpenAPIDocumentsNewRoutes(t *testing.T) {
	h := newHarness(t)
	rec := doAs(t, h, http.MethodGet, "/api/openapi.json", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi: got %d", rec.Code)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Tags []string `json:"tags"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode openapi: %v", err)
	}
	for _, path := range []string{
		"/api/v1/checkins/{id}/comments",
		"/api/v1/comments/{id}",
		"/api/v1/venues/{id}/media",
		"/api/v1/venues/media/{id}",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("openapi missing %s", path)
		}
	}
	for path, methods := range doc.Paths {
		for method, op := range methods {
			if method == "parameters" || method == "summary" || method == "description" {
				continue
			}
			if len(op.Tags) == 0 {
				t.Errorf("%s %s is untagged", method, path)
			}
		}
	}
	for _, name := range []string{"Comment", "CommentInput", "VenueMedia", "VenueMediaInput"} {
		if _, ok := doc.Components.Schemas[name]; !ok {
			t.Errorf("openapi missing schema %s", name)
		}
	}
}
