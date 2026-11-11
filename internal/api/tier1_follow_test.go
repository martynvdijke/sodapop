package api

import (
	"context"
	"net/http"
	"testing"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/ent/usersetting"
)

func (h *harness) mkUser(t *testing.T, handle string) *ent.User {
	t.Helper()
	ctx := context.Background()
	if u, err := h.server.Auth.Client.User.Query().
		Where(user.HandleEQ(handle)).Only(ctx); err == nil {
		return u
	}
	u, err := h.server.Auth.Client.User.Create().
		SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(handle).
		SetEmailVerifiedAt("2026-01-01T00:00:00Z").Save(ctx)
	if err != nil {
		t.Fatalf("user %s: %v", handle, err)
	}
	return u
}

func (h *harness) mkFollow(t *testing.T, follower, followee *ent.User, at string) {
	t.Helper()
	if _, err := h.server.Auth.Client.Follow.Create().
		SetFollowerID(follower.ID).SetFolloweeID(followee.ID).
		SetCreatedAt(at).Save(context.Background()); err != nil {
		t.Fatalf("follow %s->%s: %v", follower.Handle, followee.Handle, err)
	}
}

func (h *harness) setPublicProfile(t *testing.T, u *ent.User) {
	t.Helper()
	ctx := context.Background()
	existing, err := h.server.Auth.Client.UserSetting.Query().
		Where(usersetting.UserID(u.ID)).Only(ctx)
	if err == nil {
		if err := h.server.Auth.Client.UserSetting.UpdateOneID(existing.ID).
			SetPublicProfile(true).Exec(ctx); err != nil {
			t.Fatalf("public profile: %v", err)
		}
		return
	}
	if err := h.server.Auth.Client.UserSetting.Create().
		SetUserID(u.ID).SetPublicProfile(true).Exec(ctx); err != nil {
		t.Fatalf("public profile: %v", err)
	}
}

func TestUserFollowersAndFollowing(t *testing.T) {
	h := newHarness(t)
	alice := h.mkUser(t, "alice")
	bob := h.mkUser(t, "bob")
	carol := h.mkUser(t, "carol")
	h.setPublicProfile(t, alice)

	// bob and carol follow alice; alice follows bob back.
	h.mkFollow(t, bob, alice, "2026-01-01T10:00:00Z")
	h.mkFollow(t, carol, alice, "2026-01-01T11:00:00Z")
	h.mkFollow(t, alice, bob, "2026-01-01T12:00:00Z")

	token := h.tokenFor(t, alice.ID)

	rec := h.doWith(t, token, http.MethodGet, "/api/v1/users/alice/followers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("followers status = %d: %s", rec.Code, rec.Body.String())
	}
	rows := dataList(t, rec)
	if len(rows) != 2 {
		t.Fatalf("followers = %+v", rows)
	}
	// Newest follow first: carol then bob.
	first := rows[0].(map[string]any)
	second := rows[1].(map[string]any)
	if first["handle"] != "carol" || second["handle"] != "bob" {
		t.Fatalf("order = %v, %v", first["handle"], second["handle"])
	}
	if second["followed_by_viewer"] != true {
		t.Fatalf("bob followed_by_viewer = %v", second["followed_by_viewer"])
	}
	if first["followed_by_viewer"] != false {
		t.Fatalf("carol followed_by_viewer = %v", first["followed_by_viewer"])
	}
	for _, key := range []string{"display_name", "bio", "is_friend"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("missing %q in %+v", key, first)
		}
	}
	if _, ok := first["avatar_url"]; !ok {
		t.Fatalf("missing avatar_url in %+v", first)
	}
	if first["avatar_url"] != nil {
		t.Fatalf("avatar_url = %v, want null", first["avatar_url"])
	}

	following := h.doWith(t, token, http.MethodGet, "/api/v1/users/alice/following", "")
	if following.Code != http.StatusOK {
		t.Fatalf("following status = %d: %s", following.Code, following.Body.String())
	}
	followingRows := dataList(t, following)
	if len(followingRows) != 1 || followingRows[0].(map[string]any)["handle"] != "bob" {
		t.Fatalf("following = %+v", followingRows)
	}
}

func TestUserFollowersPagination(t *testing.T) {
	h := newHarness(t)
	alice := h.mkUser(t, "alice")
	h.setPublicProfile(t, alice)
	token := h.tokenFor(t, alice.ID)

	for i, handle := range []string{"one", "two", "three"} {
		u := h.mkUser(t, handle)
		h.mkFollow(t, u, alice, "2026-01-0"+string(rune('1'+i))+"T10:00:00Z")
	}

	first := h.doWith(t, token, http.MethodGet, "/api/v1/users/alice/followers?limit=2", "")
	if first.Code != http.StatusOK {
		t.Fatalf("page 1 = %d: %s", first.Code, first.Body.String())
	}
	pageOne := dataList(t, first)
	meta := decodeBody(t, first)["meta"].(map[string]any)
	if len(pageOne) != 2 || meta["has_more"] != true {
		t.Fatalf("page 1 = %+v meta %+v", pageOne, meta)
	}
	cursor, _ := meta["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("next_cursor missing: %+v", meta)
	}

	second := h.doWith(t, token, http.MethodGet,
		"/api/v1/users/alice/followers?limit=2&before="+cursor, "")
	if second.Code != http.StatusOK {
		t.Fatalf("page 2 = %d: %s", second.Code, second.Body.String())
	}
	pageTwo := dataList(t, second)
	if len(pageTwo) != 1 {
		t.Fatalf("page 2 = %+v", pageTwo)
	}
	metaTwo := decodeBody(t, second)["meta"].(map[string]any)
	if metaTwo["has_more"] != false || metaTwo["next_cursor"] != nil {
		t.Fatalf("page 2 meta = %+v", metaTwo)
	}
	// The two pages together cover all three members, with no repeats.
	seen := map[string]bool{}
	for _, row := range append(pageOne, pageTwo...) {
		handle := row.(map[string]any)["handle"].(string)
		if seen[handle] {
			t.Fatalf("%s repeated across pages", handle)
		}
		seen[handle] = true
	}
	if len(seen) != 3 {
		t.Fatalf("saw %d members, want 3", len(seen))
	}

	bad := h.doWith(t, token, http.MethodGet, "/api/v1/users/alice/followers?before=nonsense", "")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor status = %d: %s", bad.Code, bad.Body.String())
	}
}

func TestUserFollowListsPrivacyAndBlocks(t *testing.T) {
	h := newHarness(t)
	alice := h.mkUser(t, "alice")
	bob := h.mkUser(t, "bob")
	token := h.tokenFor(t, alice.ID)
	h.mkFollow(t, bob, alice, "2026-01-01T10:00:00Z")

	t.Run("private profile is 404", func(t *testing.T) {
		rec := h.doWith(t, token, http.MethodGet, "/api/v1/users/bob/followers", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("public profile is visible to a non-friend", func(t *testing.T) {
		h.setPublicProfile(t, bob)
		rec := h.doWith(t, token, http.MethodGet, "/api/v1/users/bob/following", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("blocked pair is 404", func(t *testing.T) {
		if _, err := h.server.Auth.Client.Block.Create().
			SetBlockerID(bob.ID).SetBlockedID(alice.ID).
			Save(context.Background()); err != nil {
			t.Fatalf("block: %v", err)
		}
		rec := h.doWith(t, token, http.MethodGet, "/api/v1/users/bob/followers", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown handle is 404", func(t *testing.T) {
		rec := h.doWith(t, token, http.MethodGet, "/api/v1/users/nobody/followers", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("anonymous is 401", func(t *testing.T) {
		rec := h.do(t, http.MethodGet, "/api/v1/users/alice/followers", "", false)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

// A member the viewer blocked disappears from the viewer's page of someone
// else's list.
func TestUserFollowersHidesBlockedMember(t *testing.T) {
	h := newHarness(t)
	alice := h.mkUser(t, "alice")
	bob := h.mkUser(t, "bob")
	carol := h.mkUser(t, "carol")
	h.setPublicProfile(t, alice)
	h.mkFollow(t, bob, alice, "2026-01-01T10:00:00Z")
	h.mkFollow(t, carol, alice, "2026-01-01T11:00:00Z")

	if _, err := h.server.Auth.Client.Block.Create().
		SetBlockerID(alice.ID).SetBlockedID(carol.ID).
		Save(context.Background()); err != nil {
		t.Fatalf("block: %v", err)
	}

	rec := h.doWith(t, h.tokenFor(t, alice.ID), http.MethodGet, "/api/v1/users/alice/followers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	rows := dataList(t, rec)
	if len(rows) != 1 || rows[0].(map[string]any)["handle"] != "bob" {
		t.Fatalf("rows = %+v, want only bob", rows)
	}
}
