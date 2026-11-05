package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/drink"
)

func makePreviewPublic(t *testing.T, h *harness, userID string) {
	t.Helper()
	if _, err := h.server.DB.Client.UserSetting.Create().
		SetUserID(userID).
		SetPublicProfile(true).
		Save(context.Background()); err != nil {
		t.Fatalf("setting %s: %v", userID, err)
	}
}

func previewCheckin(t *testing.T, h *harness, userID string, visibility checkin.Visibility) *ent.Checkin {
	t.Helper()
	ctx := context.Background()
	d, err := h.server.DB.Client.Drink.Query().Where(drink.SlugEQ("preview-cola")).Only(ctx)
	if err != nil {
		d, err = h.server.DB.Client.Drink.Create().
			SetName("Preview Cola").SetSlug("preview-cola").
			SetStatus(drink.StatusActive).Save(ctx)
		if err != nil {
			t.Fatalf("drink: %v", err)
		}
	}
	c, err := h.server.DB.Client.Checkin.Create().
		SetUserID(userID).
		SetDrinkID(d.ID).
		SetRating(16).
		SetVisibility(visibility).
		Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func TestPublicPreviewFeedOnlyShowsPublicRows(t *testing.T) {
	h := newHarness(t)
	makePreviewPublic(t, h, h.alice)
	pub := previewCheckin(t, h, h.alice, checkin.VisibilityPublic)
	friends := previewCheckin(t, h, h.alice, checkin.VisibilityFriends)
	previewCheckin(t, h, h.alice, checkin.VisibilityPrivate)
	bobPub := previewCheckin(t, h, h.bob, checkin.VisibilityPublic)

	rec := h.do(t, http.MethodGet, "/api/v1/public/feed", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("Cache-Control = %q", got)
	}
	var payload struct {
		Data       []map[string]any `json:"data"`
		NextBefore string           `json:"next_before"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Data) != 2 {
		t.Fatalf("rows = %d, want alice's and bob's public pours", len(payload.Data))
	}
	byID := map[string]map[string]any{}
	for _, row := range payload.Data {
		byID[row["id"].(string)] = row
		for _, forbidden := range []string{"user_id", "drink_id", "visibility", "can_toast", "email"} {
			if _, ok := row[forbidden]; ok {
				t.Errorf("public feed leaks %q", forbidden)
			}
		}
	}
	if _, ok := byID[pub.ID]; !ok {
		t.Errorf("missing alice's pour %s", pub.ID)
	}
	if _, ok := byID[bobPub.ID]; !ok {
		t.Errorf("missing bob's public pour %s despite no public profile opt-in", bobPub.ID)
	}
	author, _ := byID[pub.ID]["user"].(map[string]any)
	if author["handle"] != "alice" {
		t.Errorf("author = %v", byID[pub.ID]["user"])
	}
	if _, ok := byID[friends.ID]; ok {
		t.Error("friends-only row leaked")
	}
	if payload.NextBefore != "" {
		t.Errorf("next_before = %q, want empty for a short page", payload.NextBefore)
	}
}

func TestPublicPreviewPeopleOnlyListsOptedInProfiles(t *testing.T) {
	h := newHarness(t)
	makePreviewPublic(t, h, h.alice)
	previewCheckin(t, h, h.alice, checkin.VisibilityPublic)

	rec := h.do(t, http.MethodGet, "/api/v1/public/people", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0]["handle"] != "alice" {
		t.Fatalf("people = %+v, want only alice", payload.Data)
	}
	if payload.Data[0]["pours"] != float64(1) {
		t.Errorf("pours = %v, want 1", payload.Data[0]["pours"])
	}
}

func TestPublicPreviewProfileAndCheckinRespectVisibility(t *testing.T) {
	h := newHarness(t)
	makePreviewPublic(t, h, h.alice)
	pub := previewCheckin(t, h, h.alice, checkin.VisibilityPublic)
	friends := previewCheckin(t, h, h.alice, checkin.VisibilityFriends)

	rec := h.do(t, http.MethodGet, "/api/v1/public/users/alice", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodGet, "/api/v1/public/users/bob", "", false)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("private profile = %d, want 404", rec.Code)
	}
	rec = h.do(t, http.MethodGet, "/api/v1/public/users/alice/checkins", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("user checkins = %d: %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Data) != 1 || list.Data[0]["id"] != pub.ID {
		t.Fatalf("checkins = %+v, want only %s", list.Data, pub.ID)
	}

	rec = h.do(t, http.MethodGet, "/api/v1/public/checkins/"+pub.ID, "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("public checkin = %d: %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodGet, "/api/v1/public/checkins/"+friends.ID, "", false)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("friends checkin = %d, want 404", rec.Code)
	}
}

func TestPublicPreviewDoesNotMutate(t *testing.T) {
	h := newHarness(t)
	makePreviewPublic(t, h, h.alice)
	pub := previewCheckin(t, h, h.alice, checkin.VisibilityPublic)

	rec := h.do(t, http.MethodPost, "/api/v1/public/checkins/"+pub.ID+"/toast", "", false)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("mutation status = %d, want 404/405", rec.Code)
	}
}

func TestPublicPreviewRateLimit(t *testing.T) {
	h := newHarness(t)
	makePreviewPublic(t, h, h.alice)
	for i := 0; i < publicPreviewLimit; i++ {
		rec := h.do(t, http.MethodGet, "/api/v1/public/feed", "", false)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := h.do(t, http.MethodGet, "/api/v1/public/feed", "", false)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d = %d, want 429", publicPreviewLimit+1, rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
}
