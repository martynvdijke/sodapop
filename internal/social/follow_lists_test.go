package social_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/friendship"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

type followFixture struct {
	ctx   context.Context
	db    *db.DB
	svc   *social.Service
	alice *ent.User
	bob   *ent.User
	carol *ent.User
	dave  *ent.User
}

func newFollowFixture(t *testing.T) *followFixture {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "follows.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()

	mk := func(handle string) *ent.User {
		u, err := database.Client.User.Create().
			SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(handle).
			Save(ctx)
		if err != nil {
			t.Fatalf("user %s: %v", handle, err)
		}
		return u
	}
	return &followFixture{
		ctx: ctx, db: database, svc: social.New(database),
		alice: mk("alice"), bob: mk("bob"), carol: mk("carol"), dave: mk("dave"),
	}
}

func (f *followFixture) follow(t *testing.T, follower, followee *ent.User) {
	t.Helper()
	if _, err := f.db.Client.Follow.Create().
		SetFollowerID(follower.ID).SetFolloweeID(followee.ID).Save(f.ctx); err != nil {
		t.Fatalf("follow: %v", err)
	}
}

// followAt pins the follow timestamp so ordering assertions do not depend on
// second-precision insert times.
func (f *followFixture) followAt(t *testing.T, follower, followee *ent.User, at string) {
	t.Helper()
	if _, err := f.db.Client.Follow.Create().
		SetFollowerID(follower.ID).SetFolloweeID(followee.ID).
		SetCreatedAt(at).Save(f.ctx); err != nil {
		t.Fatalf("follow: %v", err)
	}
}

func (f *followFixture) befriend(t *testing.T, a, b *ent.User) {
	t.Helper()
	x, y := a.ID, b.ID
	if x > y {
		x, y = y, x
	}
	if _, err := f.db.Client.Friendship.Create().
		SetUserA(x).SetUserB(y).SetStatus(friendship.StatusAccepted).
		SetRequestedBy(a.ID).Save(f.ctx); err != nil {
		t.Fatalf("friendship: %v", err)
	}
}

func handles(users []social.FollowUser) []string {
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.Handle
	}
	return out
}

func TestFollowersDirection(t *testing.T) {
	f := newFollowFixture(t)
	// bob and carol follow alice (carol more recently); alice follows dave.
	f.followAt(t, f.bob, f.alice, "2026-01-01T10:00:00Z")
	f.followAt(t, f.carol, f.alice, "2026-01-01T11:00:00Z")
	f.followAt(t, f.alice, f.dave, "2026-01-01T12:00:00Z")

	followers, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{ViewerID: f.alice.ID})
	if err != nil {
		t.Fatalf("followers: %v", err)
	}
	if got := handles(followers.Users); len(got) != 2 || got[0] != "carol" || got[1] != "bob" {
		t.Fatalf("alice followers = %v", got)
	}

	following, err := f.svc.Following(f.ctx, f.alice.ID, social.FollowListOptions{ViewerID: f.alice.ID})
	if err != nil {
		t.Fatalf("following: %v", err)
	}
	if got := handles(following.Users); len(got) != 1 || got[0] != "dave" {
		t.Fatalf("alice following = %v", got)
	}
}

func TestFollowListViewerFlags(t *testing.T) {
	f := newFollowFixture(t)
	f.follow(t, f.bob, f.alice)
	f.follow(t, f.carol, f.alice)

	// Alice follows bob back and they are friends; carol follows alice but
	// alice does not follow carol.
	f.follow(t, f.alice, f.bob)
	f.befriend(t, f.alice, f.bob)

	page, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{ViewerID: f.alice.ID})
	if err != nil {
		t.Fatalf("followers: %v", err)
	}
	byHandle := map[string]social.FollowUser{}
	for _, u := range page.Users {
		byHandle[u.Handle] = u
	}
	if !byHandle["bob"].FollowedByViewer || !byHandle["bob"].IsFriend {
		t.Fatalf("bob flags = %+v", byHandle["bob"])
	}
	if byHandle["carol"].FollowedByViewer || byHandle["carol"].IsFriend {
		t.Fatalf("carol flags = %+v", byHandle["carol"])
	}

	// An anonymous viewer gets no flags.
	anon, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{})
	if err != nil {
		t.Fatalf("anonymous followers: %v", err)
	}
	for _, u := range anon.Users {
		if u.FollowedByViewer || u.IsFriend {
			t.Fatalf("anonymous flags for %s = %+v", u.Handle, u)
		}
	}
}

func TestFollowListPagination(t *testing.T) {
	f := newFollowFixture(t)
	f.follow(t, f.bob, f.alice)
	f.follow(t, f.carol, f.alice)
	f.follow(t, f.dave, f.alice)

	first, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(first.Users) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("page 1 = %+v", first)
	}
	second, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{
		Limit: 2, Cursor: first.NextCursor,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(second.Users) != 1 || second.HasMore {
		t.Fatalf("page 2 = %+v", second)
	}
	// No member may appear on both pages.
	seen := map[string]bool{}
	for _, u := range append(first.Users, second.Users...) {
		if seen[u.ID] {
			t.Fatalf("%s appeared twice", u.Handle)
		}
		seen[u.ID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("saw %d members, want 3", len(seen))
	}
}

func TestFollowListLimitCapAndEmpty(t *testing.T) {
	f := newFollowFixture(t)
	page, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{Limit: 500})
	if err != nil {
		t.Fatalf("followers: %v", err)
	}
	if len(page.Users) != 0 || page.HasMore || page.NextCursor != "" {
		t.Fatalf("empty page = %+v", page)
	}

	f.follow(t, f.bob, f.alice)
	capped, err := f.svc.Following(f.ctx, f.bob.ID, social.FollowListOptions{Limit: 500})
	if err != nil {
		t.Fatalf("following: %v", err)
	}
	if len(capped.Users) != 1 {
		t.Fatalf("capped = %+v", capped)
	}
}

func TestFollowListInvalidCursor(t *testing.T) {
	f := newFollowFixture(t)
	_, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{Cursor: "not-a-cursor"})
	if !errors.Is(err, social.ErrInvalidCursor) {
		t.Fatalf("err = %v, want ErrInvalidCursor", err)
	}
}

func TestFollowListHidesDeletedAndBlocked(t *testing.T) {
	f := newFollowFixture(t)
	f.follow(t, f.bob, f.alice)
	f.follow(t, f.carol, f.alice)
	f.follow(t, f.dave, f.alice)

	// Carol deletes her account; alice blocks dave.
	if err := f.db.Client.User.UpdateOneID(f.carol.ID).
		SetDeletedAt("2026-01-01T00:00:00Z").Exec(f.ctx); err != nil {
		t.Fatalf("delete carol: %v", err)
	}
	if _, err := f.db.Client.Block.Create().
		SetBlockerID(f.alice.ID).SetBlockedID(f.dave.ID).Save(f.ctx); err != nil {
		t.Fatalf("block: %v", err)
	}

	page, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{ViewerID: f.alice.ID})
	if err != nil {
		t.Fatalf("followers: %v", err)
	}
	if got := handles(page.Users); len(got) != 1 || got[0] != "bob" {
		t.Fatalf("followers = %v, want [bob]", got)
	}

	// Without a viewer the block is not a per-viewer filter, but the deleted
	// account is still hidden.
	anon, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{})
	if err != nil {
		t.Fatalf("anonymous: %v", err)
	}
	if got := handles(anon.Users); len(got) != 2 {
		t.Fatalf("anonymous followers = %v, want bob and dave", got)
	}

}

// A block between the viewer and a listed member hides that member from the
// viewer's page, in either direction, without affecting anyone else's page.
func TestFollowListHidesViewerBlockedMember(t *testing.T) {
	f := newFollowFixture(t)
	f.follow(t, f.bob, f.alice)
	f.follow(t, f.carol, f.alice)

	if _, err := f.db.Client.Block.Create().
		SetBlockerID(f.bob.ID).SetBlockedID(f.carol.ID).Save(f.ctx); err != nil {
		t.Fatalf("block: %v", err)
	}

	view, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{ViewerID: f.carol.ID})
	if err != nil {
		t.Fatalf("followers: %v", err)
	}
	if got := handles(view.Users); len(got) != 1 || got[0] != "carol" {
		t.Fatalf("carol's view = %v, want just herself", got)
	}

	// Alice, who is not part of the block, still sees both.
	owner, err := f.svc.Followers(f.ctx, f.alice.ID, social.FollowListOptions{ViewerID: f.alice.ID})
	if err != nil {
		t.Fatalf("owner view: %v", err)
	}
	if got := handles(owner.Users); len(got) != 2 {
		t.Fatalf("owner's view = %v, want both", got)
	}
}
