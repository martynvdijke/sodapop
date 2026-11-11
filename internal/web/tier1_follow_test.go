package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/usersetting"
	"sodapop/internal/social"
)

func (h *v12Harness) followUser(t *testing.T, follower, followee *ent.User) {
	t.Helper()
	if _, err := h.db.Client.Follow.Create().
		SetFollowerID(follower.ID).SetFolloweeID(followee.ID).
		Save(context.Background()); err != nil {
		t.Fatalf("follow: %v", err)
	}
}

func (h *v12Harness) makePublicProfile(t *testing.T, u *ent.User) {
	t.Helper()
	ctx := context.Background()
	existing, err := h.db.Client.UserSetting.Query().
		Where(usersetting.UserID(u.ID)).Only(ctx)
	if err == nil {
		if err := h.db.Client.UserSetting.UpdateOneID(existing.ID).
			SetPublicProfile(true).Exec(ctx); err != nil {
			t.Fatalf("public profile: %v", err)
		}
		return
	}
	if err := h.db.Client.UserSetting.Create().
		SetUserID(u.ID).SetPublicProfile(true).Exec(ctx); err != nil {
		t.Fatalf("public profile: %v", err)
	}
}

func (h *v12Harness) followPage(t *testing.T, viewer *ent.User, handle string, followers bool, query string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/u/" + handle + "/following"
	if followers {
		path = "/u/" + handle + "/followers"
	}
	if query != "" {
		path += "?" + query
	}
	rec := httptest.NewRecorder()
	req := h.request(http.MethodGet, path, viewer, url.Values{})
	req.SetPathValue("handle", handle)
	if followers {
		h.srv.followersPage(rec, req)
	} else {
		h.srv.followingPage(rec, req)
	}
	return rec
}

func TestFollowersPageRenders(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	alice := h.user(t, "alice")
	bob := h.user(t, "bob")
	carol := h.user(t, "carol")
	h.followUser(t, bob, alice)
	h.followUser(t, carol, alice)
	// Alice follows bob back, so his row offers Unfollow while carol's offers
	// Follow.
	h.followUser(t, alice, bob)

	rec := h.followPage(t, alice, "alice", true, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Followers", "@bob", "@carol",
		`/u/alice/following`, `aria-current="page"`,
		`action="/u/bob/unfollow"`, `action="/u/carol/follow"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("followers page missing %q", want)
		}
	}
	if strings.Contains(body, `action="/u/alice/follow"`) {
		t.Fatal("the viewer offered to follow themselves")
	}
}

func TestFollowingPageRenders(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	alice := h.user(t, "alice")
	dave := h.user(t, "dave")
	h.followUser(t, alice, dave)

	rec := h.followPage(t, alice, "alice", false, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Following", "@dave", `/u/alice/followers`, `action="/u/dave/unfollow"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("following page missing %q", want)
		}
	}
}

func TestFollowListEmptyState(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	alice := h.user(t, "alice")

	rec := h.followPage(t, alice, "alice", true, "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(body, "No followers yet") {
		t.Fatalf("missing empty state: %s", body)
	}
}

func TestFollowListPaginationWeb(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	alice := h.user(t, "alice")

	// One more follower than a page holds.
	followers := make([]*ent.User, 0, social.DefaultFollowPageSize+2)
	for i := 0; i < social.DefaultFollowPageSize+2; i++ {
		u := h.user(t, "follower"+string(rune('a'+i)))
		h.followUser(t, u, alice)
		followers = append(followers, u)
	}

	rec := h.followPage(t, alice, "alice", true, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if got := strings.Count(body, `class="follow-row"`); got != social.DefaultFollowPageSize {
		t.Fatalf("rows on page 1 = %d, want %d", got, social.DefaultFollowPageSize)
	}
	if !strings.Contains(body, "Next page") {
		t.Fatal("missing next-page link")
	}

	// The service's cursor is the same one the page rendered; following it
	// returns the remainder.
	page, err := h.srv.Social.Followers(context.Background(), alice.ID, social.FollowListOptions{ViewerID: alice.ID})
	if err != nil {
		t.Fatalf("followers: %v", err)
	}
	if page.NextCursor == "" {
		t.Fatal("service returned no cursor")
	}
	next := h.followPage(t, alice, "alice", true, "before="+url.QueryEscape(page.NextCursor))
	if next.Code != http.StatusOK {
		t.Fatalf("page 2 status = %d: %s", next.Code, next.Body.String())
	}
	body2 := next.Body.String()
	if got := strings.Count(body2, `class="follow-row"`); got != 2 {
		t.Fatalf("rows on page 2 = %d, want 2", got)
	}
	if !strings.Contains(body2, "Back to the newest") {
		t.Fatal("missing back link on page 2")
	}
}

func TestFollowListStaleCursorFallsBackToFirstPage(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	alice := h.user(t, "alice")
	bob := h.user(t, "bob")
	h.followUser(t, bob, alice)

	rec := h.followPage(t, alice, "alice", true, "before=not-a-cursor")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "@bob") {
		t.Fatal("stale cursor did not fall back to the first page")
	}
}

func TestFollowListVisibility(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	owner := h.user(t, "owner")
	other := h.user(t, "other")
	h.followUser(t, other, owner)

	t.Run("private profile is 404 for a stranger", func(t *testing.T) {
		rec := h.followPage(t, other, "owner", true, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("public profile is visible", func(t *testing.T) {
		h.makePublicProfile(t, owner)
		rec := h.followPage(t, other, "owner", true, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("blocked viewer is 404", func(t *testing.T) {
		if _, err := h.db.Client.Block.Create().
			SetBlockerID(owner.ID).SetBlockedID(other.ID).
			Save(context.Background()); err != nil {
			t.Fatalf("block: %v", err)
		}
		rec := h.followPage(t, other, "owner", true, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown handle is 404", func(t *testing.T) {
		rec := h.followPage(t, other, "nobody", true, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})

	t.Run("owner always sees their own lists", func(t *testing.T) {
		rec := h.followPage(t, owner, "owner", false, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestFollowListRequiresUser(t *testing.T) {
	h := newV12Harness(t)
	h.renderWith(t, "follow_list", "unused")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/u/alice/followers", nil)
	req.SetPathValue("handle", "alice")
	h.srv.followersPage(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Fatalf("location = %q", loc)
	}
}

// The follower and following counts on a profile are links to these pages.
func TestProfileCountsLinkToFollowLists(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	alice := h.user(t, "alice")

	rec := httptest.NewRecorder()
	req := h.request(http.MethodGet, "/u/alice", alice, url.Values{})
	req.SetPathValue("handle", "alice")
	h.srv.profilePage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`href="/u/alice/followers"`, `href="/u/alice/following"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("profile missing %q", want)
		}
	}
}
