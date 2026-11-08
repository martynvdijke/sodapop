package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"sodapop/internal/checkins"
)

func feedContainsID(t *testing.T, body []byte, id string) (map[string]any, bool) {
	t.Helper()
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode feed: %v (%s)", err, string(body))
	}
	for _, item := range payload.Data {
		if item["id"] == id {
			return item, true
		}
	}
	return nil, false
}

func TestTaggedFriendVisibilityOnCheckinAndFeed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner := mustUser(t, h, h.alice)
	friend := apiFriend(t, h, "privacy-friend")
	stranger := mustUser(t, h, h.bob)
	drink := newDrink(t, h, "Privacy Cola", "privacy-cola", nil)
	lat, lng := 51.5, -0.12
	friendToken := h.tokenFor(t, friend.ID)
	strangerToken := h.tokenFor(t, stranger.ID)

	friendsOnly, _, err := h.server.Checkins.Create(ctx, owner, checkins.CreateInput{
		DrinkID: drink.ID, Visibility: "friends", Lat: &lat, Lng: &lng,
		FriendIDs: []string{friend.ID},
	})
	if err != nil {
		t.Fatalf("friends-only check-in: %v", err)
	}
	private, _, err := h.server.Checkins.Create(ctx, owner, checkins.CreateInput{
		DrinkID: drink.ID, Visibility: "private", Lat: &lat, Lng: &lng,
		FriendIDs: []string{friend.ID},
	})
	if err != nil {
		t.Fatalf("private check-in: %v", err)
	}

	t.Run("friend sees tags on friends-only detail", func(t *testing.T) {
		rec := h.doWith(t, friendToken, http.MethodGet, "/api/v1/checkins/"+friendsOnly.ID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("friend detail: %d %s", rec.Code, rec.Body.String())
		}
		tags, _ := dataMap(t, rec)["tagged_friends"].([]any)
		if len(tags) != 1 || tags[0].(map[string]any)["id"] != friend.ID {
			t.Fatalf("friend tagged_friends = %v", tags)
		}
	})

	t.Run("stranger cannot see friends-only or private detail", func(t *testing.T) {
		for _, id := range []string{friendsOnly.ID, private.ID} {
			rec := h.doWith(t, strangerToken, http.MethodGet, "/api/v1/checkins/"+id, "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("stranger detail %s: %d %s", id, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("friend sees private detail", func(t *testing.T) {
		rec := h.doWith(t, friendToken, http.MethodGet, "/api/v1/checkins/"+private.ID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("friend private detail: %d %s", rec.Code, rec.Body.String())
		}
		tags, _ := dataMap(t, rec)["tagged_friends"].([]any)
		if len(tags) != 1 || tags[0].(map[string]any)["id"] != friend.ID {
			t.Fatalf("friend tagged_friends on private = %v", tags)
		}
	})

	t.Run("owner always sees tags", func(t *testing.T) {
		for _, id := range []string{friendsOnly.ID, private.ID} {
			rec := h.do(t, http.MethodGet, "/api/v1/checkins/"+id, "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("owner detail %s: %d %s", id, rec.Code, rec.Body.String())
			}
			if tags, _ := dataMap(t, rec)["tagged_friends"].([]any); len(tags) != 1 {
				t.Fatalf("owner tagged_friends for %s = %v", id, tags)
			}
		}
	})

	t.Run("feed scopes tags to visible rows", func(t *testing.T) {
		rec := h.doWith(t, friendToken, http.MethodGet, "/api/v1/feed", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("friend feed: %d %s", rec.Code, rec.Body.String())
		}
		item, ok := feedContainsID(t, rec.Body.Bytes(), friendsOnly.ID)
		if !ok {
			t.Fatalf("friend feed missing friends-only check-in")
		}
		if tags, _ := item["tagged_friends"].([]any); len(tags) != 1 {
			t.Fatalf("friend feed tagged_friends = %v", item["tagged_friends"])
		}
		// Private is readable by accepted friends, so it appears in their feed
		// with tags scoped to that viewer.
		privateItem, ok := feedContainsID(t, rec.Body.Bytes(), private.ID)
		if !ok {
			t.Fatalf("friend feed missing private check-in")
		}
		if tags, _ := privateItem["tagged_friends"].([]any); len(tags) != 1 {
			t.Fatalf("friend feed private tagged_friends = %v", privateItem["tagged_friends"])
		}

		rec = h.doWith(t, strangerToken, http.MethodGet, "/api/v1/feed", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("stranger feed: %d %s", rec.Code, rec.Body.String())
		}
		for _, id := range []string{friendsOnly.ID, private.ID} {
			if _, leaked := feedContainsID(t, rec.Body.Bytes(), id); leaked {
				t.Fatalf("stranger feed leaked %s", id)
			}
		}
	})
}

func TestNearbyFeedStripsLocationAndTagsForTaggedCheckin(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner := mustUser(t, h, h.alice)
	friend := apiFriend(t, h, "nearby-privacy-friend")
	drink := nearbyTestDrink(t, h, "nearby-privacy-cola")
	venue := nearbyTestVenue(t, h, "nearby-privacy-venue", 51.5, -0.12)
	lat, lng := 51.5, -0.12
	venueID := venue.ID

	c, _, err := h.server.Checkins.Create(ctx, owner, checkins.CreateInput{
		DrinkID: drink.ID, VenueID: &venueID, Visibility: "public",
		Lat: &lat, Lng: &lng, FriendIDs: []string{friend.ID},
	})
	if err != nil {
		t.Fatalf("check-in: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/feed/nearby?lat=51.5&lng=-0.12&radius=2", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("nearby: %d %s", rec.Code, rec.Body.String())
	}
	rows, _ := decodeNearby(t, rec.Body.Bytes())
	var found map[string]any
	for _, row := range rows {
		if row["id"] == c.ID {
			found = row
			break
		}
	}
	if found == nil {
		t.Fatalf("nearby feed missing tagged check-in: %v", rows)
	}
	for _, key := range []string{"lat", "lng", "tagged_friends"} {
		if _, ok := found[key]; ok {
			t.Fatalf("nearby feed leaked %q: %v", key, found)
		}
	}
}
