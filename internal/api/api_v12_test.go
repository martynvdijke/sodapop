package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/list"
	"sodapop/internal/checkins"
	"sodapop/internal/venues"
)

func (h *harness) doWith(t *testing.T, token, method, path, body string) *httptest.ResponseRecorder {
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

func (h *harness) tokenFor(t *testing.T, userID string) string {
	t.Helper()
	token, _, err := h.server.Auth.CreateAPIToken(context.Background(), userID, "test", []string{"read", "write", "feed"})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	return token
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

func dataMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	body := decodeBody(t, rec)
	m, _ := body["data"].(map[string]any)
	if m == nil {
		t.Fatalf("data is not an object: %s", rec.Body.String())
	}
	return m
}

func dataList(t *testing.T, rec *httptest.ResponseRecorder) []any {
	t.Helper()
	body := decodeBody(t, rec)
	rows, _ := body["data"].([]any)
	if rows == nil {
		t.Fatalf("data is not an array: %s", rec.Body.String())
	}
	return rows
}

func newDrink(t *testing.T, h *harness, name, slug string, styleID *int) *ent.Drink {
	t.Helper()
	create := h.server.Auth.Client.Drink.Create().SetName(name).SetSlug(slug).SetStatus("active")
	if styleID != nil {
		create = create.SetStyleID(*styleID)
	}
	d, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("drink %s: %v", slug, err)
	}
	return d
}

func checkinFor(t *testing.T, h *harness, user *ent.User, drinkID int, visibility string) *ent.Checkin {
	t.Helper()
	c, _, err := h.server.Checkins.Create(context.Background(), user, checkins.CreateInput{
		DrinkID: drinkID, Visibility: visibility,
	})
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func TestCheckinDetailFlagsAndCommentCount(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)
	bobToken := h.tokenFor(t, h.bob)
	drink := newDrink(t, h, "Cola", "cola", nil)
	c := checkinFor(t, h, bob, drink.ID, "public")

	rec := h.do(t, http.MethodGet, "/api/v1/checkins/"+c.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	view := dataMap(t, rec)
	if view["can_delete"] != false || view["can_toast"] != true || view["can_comment"] != true {
		t.Fatalf("alice flags = %+v", view)
	}
	if view["comments_count"].(float64) != 0 {
		t.Fatalf("comments_count = %v, want 0", view["comments_count"])
	}

	created := h.do(t, http.MethodPost, "/api/v1/checkins/"+c.ID+"/comments", `{"body":"nice"}`, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("comment: %d %s", created.Code, created.Body.String())
	}
	commentID := dataMap(t, created)["id"].(string)

	rec = h.doWith(t, bobToken, http.MethodGet, "/api/v1/checkins/"+c.ID, "")
	own := dataMap(t, rec)
	if own["can_delete"] != true || own["can_toast"] != false || own["can_comment"] != true {
		t.Fatalf("owner flags = %+v", own)
	}
	if own["comments_count"].(float64) != 1 {
		t.Fatalf("comments_count = %v, want 1", own["comments_count"])
	}

	if rec := h.do(t, http.MethodDelete, "/api/v1/comments/"+commentID, "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("delete comment: %d", rec.Code)
	}
	rec = h.doWith(t, bobToken, http.MethodGet, "/api/v1/checkins/"+c.ID, "")
	if got := dataMap(t, rec)["comments_count"].(float64); got != 0 {
		t.Fatalf("comments_count after delete = %v, want 0", got)
	}

	// Toasting clears the viewer's own capability flag.
	if rec := h.do(t, http.MethodPost, "/api/v1/checkins/"+c.ID+"/toast", "", true); rec.Code != http.StatusOK {
		t.Fatalf("toast: %d %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodGet, "/api/v1/checkins/"+c.ID, "", true)
	toasted := dataMap(t, rec)
	if toasted["toasted"] != true || toasted["can_toast"] != false {
		t.Fatalf("toasted flags = %+v", toasted)
	}

	// A private check-in stays a 404 even for a signed-in token.
	private := checkinFor(t, h, bob, drink.ID, "private")
	if rec := h.do(t, http.MethodGet, "/api/v1/checkins/"+private.ID, "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("private checkin: got %d, want 404", rec.Code)
	}
	_ = alice
	_ = ctx
}

func TestUserProfileV12(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)

	style, err := h.server.Auth.Client.Style.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("style: %v", err)
	}
	d1 := newDrink(t, h, "Cola One", "cola-one", &style.ID)
	d2 := newDrink(t, h, "Cola Two", "cola-two", &style.ID)
	d3 := newDrink(t, h, "Cola Three", "cola-three", &style.ID)
	checkinFor(t, h, alice, d1.ID, "public")
	checkinFor(t, h, alice, d2.ID, "friends")
	checkinFor(t, h, alice, d3.ID, "private")

	if err := h.server.Social.Toast(ctx, bob.ID, mustCurrentPublic(t, h, alice.ID)); err != nil {
		t.Fatalf("toast: %v", err)
	}

	// Stranger sees public activity only.
	bobToken := h.tokenFor(t, h.bob)
	rec := h.doWith(t, bobToken, http.MethodGet, "/api/v1/users/alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("profile: %d %s", rec.Code, rec.Body.String())
	}
	profile := dataMap(t, rec)
	stats := profile["stats"].(map[string]any)
	if stats["checkins"].(float64) != 1 || stats["distinct_drinks"].(float64) != 1 || stats["toasts_received"].(float64) != 1 {
		t.Fatalf("stranger stats = %+v", stats)
	}
	if profile["is_friend"] != false || profile["is_following"] != false || profile["can_block"] != true {
		t.Fatalf("relationship = %+v", profile)
	}
	if profile["follower_count"].(float64) != 0 {
		t.Fatalf("follower_count = %v", profile["follower_count"])
	}

	// Friend sees friends-only rows too.
	befriend(t, h, alice.ID, bob)
	rec = h.doWith(t, bobToken, http.MethodGet, "/api/v1/users/alice", "")
	profile = dataMap(t, rec)
	stats = profile["stats"].(map[string]any)
	if stats["checkins"].(float64) != 2 || stats["distinct_drinks"].(float64) != 2 {
		t.Fatalf("friend stats = %+v", stats)
	}
	if profile["is_friend"] != true {
		t.Fatalf("is_friend = %v", profile["is_friend"])
	}
	styles, _ := profile["top_styles"].([]any)
	if len(styles) != 1 || styles[0].(map[string]any)["slug"] != "cola" || styles[0].(map[string]any)["count"].(float64) != 2 {
		t.Fatalf("top_styles = %+v", styles)
	}

	// Follow state and counts.
	if err := h.server.Social.Follow(ctx, alice.ID, "bob"); err != nil {
		t.Fatalf("follow: %v", err)
	}
	rec = h.do(t, http.MethodGet, "/api/v1/users/bob", "", true)
	bobProfile := dataMap(t, rec)
	if bobProfile["is_following"] != true || bobProfile["follower_count"].(float64) != 1 {
		t.Fatalf("bob profile = %+v", bobProfile)
	}
	rec = h.do(t, http.MethodGet, "/api/v1/users/alice", "", true)
	aliceProfile := dataMap(t, rec)
	if aliceProfile["following_count"].(float64) != 1 {
		t.Fatalf("alice following_count = %v", aliceProfile["following_count"])
	}

	// Earned badges only, and visible lists only.
	badge, err := h.server.Auth.Client.Badge.Create().
		SetSlug("test-first-pour").SetName("Test First Pour").SetDescription("Log a soda").SetRule("checkins>=1").Save(ctx)
	if err != nil {
		t.Fatalf("badge: %v", err)
	}
	if _, err := h.server.Auth.Client.Badge.Create().
		SetSlug("test-century").SetName("Test Century").SetRule("checkins>=100").Save(ctx); err != nil {
		t.Fatalf("badge 2: %v", err)
	}
	if _, err := h.server.Auth.Client.UserBadge.Create().
		SetUserID(alice.ID).SetBadgeID(badge.ID).SetAwardedAt("2026-01-01T00:00:00Z").Save(ctx); err != nil {
		t.Fatalf("user badge: %v", err)
	}
	for _, vis := range []string{"public", "friends", "private"} {
		if _, err := h.server.Auth.Client.List.Create().
			SetUserID(alice.ID).SetName("List " + vis).SetVisibility(list.Visibility(vis)).Save(ctx); err != nil {
			t.Fatalf("list: %v", err)
		}
	}
	rec = h.do(t, http.MethodGet, "/api/v1/users/alice", "", true)
	profile = dataMap(t, rec)
	badges, _ := profile["badges"].([]any)
	foundEarned := false
	for _, raw := range badges {
		b := raw.(map[string]any)
		if b["slug"] == "test-first-pour" {
			foundEarned = true
		}
		if b["slug"] == "test-century" {
			t.Fatalf("unearned badge leaked onto profile: %+v", b)
		}
	}
	if !foundEarned {
		t.Fatalf("badges = %+v, want test-first-pour", badges)
	}
	lists, _ := profile["lists"].([]any)
	if len(lists) != 3 {
		t.Fatalf("owner lists = %d, want 3", len(lists))
	}

	// Blocked and deleted profiles are indistinguishable from missing ones.
	if err := h.server.Social.Block(ctx, bob.ID, "alice"); err != nil {
		t.Fatalf("block: %v", err)
	}
	if rec := h.doWith(t, bobToken, http.MethodGet, "/api/v1/users/alice", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("blocked profile: got %d, want 404", rec.Code)
	}
}

func mustCurrentPublic(t *testing.T, h *harness, userID string) string {
	t.Helper()
	var id string
	err := h.server.DB.SQL.QueryRow(`SELECT id FROM checkins WHERE user_id = ? AND visibility = 'public' AND deleted_at IS NULL LIMIT 1`, userID).Scan(&id)
	if err != nil {
		t.Fatalf("public checkin: %v", err)
	}
	return id
}

func TestFollowEndpoints(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)

	rec := h.do(t, http.MethodPost, "/api/v1/users/bob/follow", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("follow: %d %s", rec.Code, rec.Body.String())
	}
	// Idempotent.
	if rec := h.do(t, http.MethodPost, "/api/v1/users/bob/follow", "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat follow: %d", rec.Code)
	}
	if ok, _ := h.server.Auth.Client.Follow.Query().Where().Exist(ctx); !ok {
		t.Fatal("follow row missing")
	}

	if rec := h.do(t, http.MethodPost, "/api/v1/users/alice/follow", "", true); rec.Code != http.StatusBadRequest {
		t.Fatalf("self follow: got %d, want 400", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/api/v1/users/nobody/follow", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown follow: got %d, want 404", rec.Code)
	}

	if rec := h.do(t, http.MethodDelete, "/api/v1/users/bob/follow", "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("unfollow: %d", rec.Code)
	}
	if rec := h.do(t, http.MethodDelete, "/api/v1/users/bob/follow", "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat unfollow: %d", rec.Code)
	}

	// Blocked targets produce the same 404 as unknown handles, never 403.
	if err := h.server.Social.Block(ctx, alice.ID, "bob"); err != nil {
		t.Fatalf("block: %v", err)
	}
	blocked := h.do(t, http.MethodPost, "/api/v1/users/bob/follow", "", true)
	unknown := h.do(t, http.MethodPost, "/api/v1/users/nobody/follow", "", true)
	if blocked.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound {
		t.Fatalf("blocked=%d unknown=%d, want 404/404", blocked.Code, unknown.Code)
	}
	if blocked.Body.String() != unknown.Body.String() {
		t.Fatalf("blocked and unknown bodies differ: %q vs %q", blocked.Body.String(), unknown.Body.String())
	}

	// Unverified tokens cannot follow.
	carol, err := h.server.Auth.Client.User.Create().
		SetHandle("carol").SetEmail("carol@example.com").SetDisplayName("Carol").Save(ctx)
	if err != nil {
		t.Fatalf("carol: %v", err)
	}
	carolToken := h.tokenFor(t, carol.ID)
	if rec := h.doWith(t, carolToken, http.MethodPost, "/api/v1/users/alice/follow", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("unverified follow: got %d, want 403", rec.Code)
	}
}

func TestNotificationsV12(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)
	drink := newDrink(t, h, "Lemon", "lemon", nil)
	c := checkinFor(t, h, alice, drink.ID, "public")
	if err := h.server.Social.Toast(ctx, bob.ID, c.ID); err != nil {
		t.Fatalf("toast: %v", err)
	}
	f, err := h.server.Social.Request(ctx, bob.ID, "alice")
	if err != nil || f == nil {
		t.Fatalf("request: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/notifications", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("notifications: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "total") {
		t.Fatalf("notifications leak totals: %s", rec.Body.String())
	}
	rows := dataList(t, rec)
	var friendRow, toastRow map[string]any
	for _, raw := range rows {
		row := raw.(map[string]any)
		switch row["kind"] {
		case "friend_request":
			friendRow = row
		case "toast":
			toastRow = row
		}
	}
	if friendRow == nil || friendRow["friend_request_id"] != strconv.Itoa(f.ID) {
		t.Fatalf("friend row = %+v", friendRow)
	}
	if friendRow["href"] != "/friends" || friendRow["actor_initial"] != "B" {
		t.Fatalf("friend row href/initial = %+v", friendRow)
	}
	if toastRow == nil || toastRow["href"] != "/checkins/"+c.ID {
		t.Fatalf("toast row = %+v", toastRow)
	}
	if _, ok := friendRow["friend_request_id"]; !ok {
		t.Fatal("incoming pending request missing friend_request_id")
	}

	if err := h.server.Social.Accept(ctx, alice.ID, strconv.Itoa(f.ID)); err != nil {
		t.Fatalf("accept: %v", err)
	}
	rec = h.do(t, http.MethodGet, "/api/v1/notifications", "", true)
	rows = dataList(t, rec)
	foundPending := false
	for _, raw := range rows {
		row := raw.(map[string]any)
		if row["kind"] == "friend_request" {
			if _, ok := row["friend_request_id"]; ok {
				foundPending = true
			}
		}
	}
	if foundPending {
		t.Fatal("friend_request_id must be omitted once the request is handled")
	}
}

func TestSavedVenues(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)

	v, _, err := h.server.Venues.Create(ctx, alice.ID, venues.CreateInput{Name: "Corner Shop", Kind: "shop", City: "Leeds"})
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	other, _, err := h.server.Venues.Create(ctx, bob.ID, venues.CreateInput{Name: "Bob Bar", Kind: "bar", City: "York"})
	if err != nil {
		t.Fatalf("venue 2: %v", err)
	}
	if err := h.server.Venues.Save(ctx, alice.ID, v.ID); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := h.server.Venues.Save(ctx, bob.ID, other.ID); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/venues/saved", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("saved: %d %s", rec.Code, rec.Body.String())
	}
	rows := dataList(t, rec)
	if len(rows) != 1 {
		t.Fatalf("saved = %+v, want only alice's venue", rows)
	}
	row := rows[0].(map[string]any)
	if row["id"].(float64) != float64(v.ID) || row["name"] != "Corner Shop" || row["kind"] != "shop" || row["city"] != "Leeds" {
		t.Fatalf("saved row = %+v", row)
	}
	if strings.Contains(rec.Body.String(), "total") {
		t.Fatalf("saved venues leak totals: %s", rec.Body.String())
	}
}

func TestListsAPI(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)
	d1 := newDrink(t, h, "Alpha", "alpha", nil)
	d2 := newDrink(t, h, "Beta", "beta", nil)

	mk := func(name, vis string, system bool) *ent.List {
		l, err := h.server.Auth.Client.List.Create().
			SetUserID(alice.ID).SetName(name).SetVisibility(list.Visibility(vis)).SetIsSystem(system).Save(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return l
	}
	pub := mk("Public list", "public", false)
	friends := mk("Friends list", "friends", false)
	private := mk("Private list", "private", false)
	system := mk("Wishlist", "public", true)

	for _, pair := range []struct {
		l  *ent.List
		id int
		at string
	}{
		{pub, d1.ID, "2026-01-01T00:00:00Z"},
		{pub, d2.ID, "2026-02-01T00:00:00Z"},
		{system, d1.ID, "2026-03-01T00:00:00Z"},
	} {
		if _, err := h.server.Auth.Client.ListItem.Create().
			SetListID(pair.l.ID).SetDrinkID(pair.id).SetAddedAt(pair.at).SetNote("note").Save(ctx); err != nil {
			t.Fatalf("item: %v", err)
		}
	}

	bobToken := h.tokenFor(t, h.bob)

	// Owner sees all owned lists, including the system wishlist.
	rec := h.do(t, http.MethodGet, "/api/v1/me/lists", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("me lists: %d %s", rec.Code, rec.Body.String())
	}
	lists := dataList(t, rec)
	if len(lists) != 4 {
		t.Fatalf("me lists = %d, want 4", len(lists))
	}
	if strings.Contains(rec.Body.String(), "total") {
		t.Fatalf("me lists leak totals: %s", rec.Body.String())
	}

	// Public list is visible to a stranger; items are newest-first and bounded.

	// Public list is visible to a stranger; items are newest-first and bounded.
	rec = h.doWith(t, bobToken, http.MethodGet, "/api/v1/lists/"+pub.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public list: %d %s", rec.Code, rec.Body.String())
	}
	detail := dataMap(t, rec)
	items := detail["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["drink_id"].(float64) != float64(d2.ID) {
		t.Fatalf("items = %+v, want newest first", items)
	}
	if detail["item_count"].(float64) != 2 {
		t.Fatalf("item_count = %v", detail["item_count"])
	}

	for _, hidden := range []string{friends.ID, private.ID, system.ID} {
		if rec := h.doWith(t, bobToken, http.MethodGet, "/api/v1/lists/"+hidden, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("hidden list %s: got %d, want 404", hidden, rec.Code)
		}
	}
	unknown := h.doWith(t, bobToken, http.MethodGet, "/api/v1/lists/does-not-exist", "")
	hidden := h.doWith(t, bobToken, http.MethodGet, "/api/v1/lists/"+private.ID, "")
	if unknown.Code != hidden.Code || unknown.Body.String() != hidden.Body.String() {
		t.Fatalf("unknown and hidden list responses differ: %d %q vs %d %q", unknown.Code, unknown.Body.String(), hidden.Code, hidden.Body.String())
	}

	// Friendship opens the friends-only list; a block closes everything.
	befriend(t, h, alice.ID, bob)
	if rec := h.doWith(t, bobToken, http.MethodGet, "/api/v1/lists/"+friends.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("friend list: got %d, want 200", rec.Code)
	}
	if rec := h.doWith(t, bobToken, http.MethodGet, "/api/v1/lists/"+system.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("system list to friend: got %d, want 404", rec.Code)
	}
	if err := h.server.Social.Block(ctx, alice.ID, "bob"); err != nil {
		t.Fatalf("block: %v", err)
	}
	if rec := h.doWith(t, bobToken, http.MethodGet, "/api/v1/lists/"+pub.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("blocked-owner list: got %d, want 404", rec.Code)
	}
}

func TestMixAPI(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d1 := newDrink(t, h, "Cola", "mix-cola", nil)
	d2 := newDrink(t, h, "Lemon", "mix-lemon", nil)

	body := `{"name":"Sunset","notes":"bright","visibility":"private","components":[` +
		`{"drink_id":` + strconv.Itoa(d1.ID) + `,"volume_ml":120},` +
		`{"drink_id":` + strconv.Itoa(d2.ID) + `,"volume_ml":80}]}`
	rec := h.do(t, http.MethodPost, "/api/v1/mix", body, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create mix: %d %s", rec.Code, rec.Body.String())
	}
	created := dataMap(t, rec)
	mixID := int(created["id"].(float64))
	components := created["components"].([]any)
	if len(components) != 2 || components[0].(map[string]any)["volume_ml"].(float64) != 120 {
		t.Fatalf("components = %+v", components)
	}

	rec = h.do(t, http.MethodGet, "/api/v1/mix", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("mix list: %d", rec.Code)
	}
	if len(dataList(t, rec)) != 1 || strings.Contains(rec.Body.String(), "total") {
		t.Fatalf("mix list = %s", rec.Body.String())
	}
	rec = h.do(t, http.MethodGet, "/api/v1/mix/"+strconv.Itoa(mixID), "", true)
	if rec.Code != http.StatusOK || dataMap(t, rec)["slug"] != created["slug"] {
		t.Fatalf("mix get: %d %s", rec.Code, rec.Body.String())
	}
	// A private mix is invisible to another user.
	bobToken := h.tokenFor(t, h.bob)
	if rec := h.doWith(t, bobToken, http.MethodGet, "/api/v1/mix/"+strconv.Itoa(mixID), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("bob mix get: got %d, want 404", rec.Code)
	}
	if rec := h.do(t, http.MethodGet, "/api/v1/mix/999999", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown mix: got %d, want 404", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/api/v1/mix", `{"name":"Bad","components":[{"drink_id":`+strconv.Itoa(d1.ID)+`}]}`, true); rec.Code != http.StatusBadRequest {
		t.Fatalf("one-component mix: got %d, want 400", rec.Code)
	}

	carol, err := h.server.Auth.Client.User.Create().
		SetHandle("carol").SetEmail("carol@example.com").SetDisplayName("Carol").Save(ctx)
	if err != nil {
		t.Fatalf("carol: %v", err)
	}
	carolToken := h.tokenFor(t, carol.ID)
	if rec := h.doWith(t, carolToken, http.MethodPost, "/api/v1/mix", body); rec.Code != http.StatusForbidden {
		t.Fatalf("unverified mix: got %d, want 403", rec.Code)
	}
}

func TestTasteEndpoints(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	bob := mustUser(t, h, h.bob)
	befriend(t, h, alice.ID, bob)

	rate := func(u *ent.User, drinkID, rating int) {
		t.Helper()
		r := rating
		if _, _, err := h.server.Checkins.Create(ctx, u, checkins.CreateInput{DrinkID: drinkID, Rating: &r, Visibility: "public"}); err != nil {
			t.Fatalf("rate: %v", err)
		}
	}
	for i := 1; i <= 3; i++ {
		d := newDrink(t, h, "Common "+strconv.Itoa(i), "common-"+strconv.Itoa(i), nil)
		rate(alice, d.ID, 20)
		rate(bob, d.ID, 20)
	}
	recDrink := newDrink(t, h, "Hidden Gem", "hidden-gem", nil)
	rate(bob, recDrink.ID, 20)

	rec := h.do(t, http.MethodGet, "/api/v1/taste/twins", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("twins: %d %s", rec.Code, rec.Body.String())
	}
	twins := dataList(t, rec)
	if len(twins) != 1 || twins[0].(map[string]any)["handle"] != "bob" {
		t.Fatalf("twins = %+v", twins)
	}
	if strings.Contains(rec.Body.String(), "total") {
		t.Fatalf("twins leak totals: %s", rec.Body.String())
	}

	rec = h.do(t, http.MethodGet, "/api/v1/taste/recommendations", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("recommendations: %d %s", rec.Code, rec.Body.String())
	}
	recs := dataList(t, rec)
	if len(recs) != 1 || recs[0].(map[string]any)["slug"] != "hidden-gem" {
		t.Fatalf("recommendations = %+v", recs)
	}
}

func TestBadgesAndPassport(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := mustUser(t, h, h.alice)
	d := newDrink(t, h, "Solo", "solo", nil)
	checkinFor(t, h, alice, d.ID, "public")

	if _, err := h.server.Auth.Client.Badge.Create().
		SetSlug("test-first-pour").SetName("Test First Pour").SetDescription("Log one").SetRule("checkins>=1").Save(ctx); err != nil {
		t.Fatalf("badge 1: %v", err)
	}
	if _, err := h.server.Auth.Client.Badge.Create().
		SetSlug("test-century").SetName("Test Century").SetDescription("Log 100").SetRule("checkins>=100").Save(ctx); err != nil {
		t.Fatalf("badge 2: %v", err)
	}
	if _, err := h.server.Badges.Evaluate(ctx, alice.ID); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/badges", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("badges: %d %s", rec.Code, rec.Body.String())
	}
	rows := dataList(t, rec)
	bySlug := map[string]map[string]any{}
	for _, raw := range rows {
		row := raw.(map[string]any)
		bySlug[row["slug"].(string)] = row
	}
	if bySlug["test-first-pour"]["earned"] != true || bySlug["test-first-pour"]["awarded_at"] == "" {
		t.Fatalf("earned badge = %+v", bySlug["test-first-pour"])
	}
	if bySlug["test-century"]["earned"] != false || bySlug["test-century"]["target"].(float64) != 100 ||
		bySlug["test-century"]["progress"].(float64) != 1 {
		t.Fatalf("unearned badge = %+v", bySlug["test-century"])
	}

	rec = h.do(t, http.MethodGet, "/api/v1/passport", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("passport: %d %s", rec.Code, rec.Body.String())
	}
	p := dataMap(t, rec)
	stats := p["stats"].(map[string]any)
	if stats["checkins"].(float64) != 1 || stats["unique_drinks"].(float64) != 1 {
		t.Fatalf("passport stats = %+v", stats)
	}
	badges := p["badges"].([]any)
	foundEarned := false
	for _, raw := range badges {
		b := raw.(map[string]any)
		if b["slug"] == "test-first-pour" {
			foundEarned = true
		}
		if b["slug"] == "test-century" {
			t.Fatalf("unearned badge leaked into passport: %+v", b)
		}
	}
	if !foundEarned {
		t.Fatalf("passport badges = %+v, want test-first-pour", badges)
	}
}

func TestNewRoutesScopeAndDeletedTargets(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	readToken, _, err := h.server.Auth.CreateAPIToken(ctx, h.alice, "read-only", []string{"read"})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/v1/users/bob/follow", ""},
		{http.MethodDelete, "/api/v1/users/bob/follow", ""},
		{http.MethodPost, "/api/v1/mix", `{"name":"Nope","components":[]}`},
	} {
		rec := h.doWith(t, readToken, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with read scope: got %d, want 403", tc.method, tc.path, rec.Code)
		}
	}

	// Reads with the same token are allowed (not 403) even when the row is absent.
	for _, path := range []string{"/api/v1/lists/missing", "/api/v1/mix/999999"} {
		if rec := h.doWith(t, readToken, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", path, rec.Code)
		}
	}

	bob := mustUser(t, h, h.bob)
	if err := h.server.Auth.Client.User.UpdateOneID(bob.ID).SetStatus("deleted").Exec(ctx); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if rec := h.do(t, http.MethodGet, "/api/v1/users/bob", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted profile: got %d, want 404", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/api/v1/users/bob/follow", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted follow target: got %d, want 404", rec.Code)
	}
}
