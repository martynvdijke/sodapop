package checkins_test

import (
	"testing"

	"sodapop/ent/list"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
)

func ids(items []checkins.ProfileItem) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, it := range items {
		out[it.ID] = true
	}
	return out
}

func TestProfileVisitFor(t *testing.T) {
	e := newTestEnv(t)
	e.befriend(t, e.alice, e.bob)

	cases := []struct {
		name   string
		viewer string
		target string
		want   checkins.ProfileVisit
	}{
		{"anonymous", "", e.alice.ID, checkins.ProfileAnonymous},
		{"owner", e.alice.ID, e.alice.ID, checkins.ProfileOwner},
		{"friend", e.bob.ID, e.alice.ID, checkins.ProfileFriend},
		{"stranger", e.carol.ID, e.alice.ID, checkins.ProfileStranger},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.svc.ProfileVisitFor(e.ctx, tc.viewer, tc.target); got != tc.want {
				t.Fatalf("visit = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProfileCheckinsAnonymous(t *testing.T) {
	e := newTestEnv(t)
	pub := e.createCheckin(t, e.alice, "public")
	e.createCheckin(t, e.alice, "friends")
	e.createCheckin(t, e.alice, "private")

	items, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{TargetID: e.alice.ID})
	if err != nil {
		t.Fatalf("profile checkins: %v", err)
	}
	if len(items) != 1 || items[0].ID != pub.ID {
		t.Fatalf("anonymous items = %+v, want only %s", items, pub.ID)
	}
	if items[0].Handle != "alice" || items[0].DisplayName != "Alice" {
		t.Fatalf("author fields = %q/%q", items[0].Handle, items[0].DisplayName)
	}
	if items[0].DrinkSlug != "ginger-beer" || items[0].DrinkName != "Ginger Beer" {
		t.Fatalf("drink fields = %+v", items[0])
	}
}

func TestProfileCheckinsCarriesServing(t *testing.T) {
	e := newTestEnv(t)
	if _, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
		DrinkID: e.drink.ID, Visibility: "public", Serving: "bottle",
	}); err != nil {
		t.Fatalf("checkin: %v", err)
	}

	items, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{TargetID: e.alice.ID})
	if err != nil {
		t.Fatalf("profile checkins: %v", err)
	}
	if len(items) != 1 || items[0].Serving != "bottle" {
		t.Fatalf("items = %+v, want one row with serving=bottle", items)
	}
}

func TestProfileCheckinsPrivateProfileGate(t *testing.T) {
	// The query layer does not know about the public_profile toggle; it returns
	// public rows when called. The handler gate is the access rule below.
	e := newTestEnv(t)
	e.createCheckin(t, e.alice, "public")

	items, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{TargetID: e.alice.ID})
	if err != nil {
		t.Fatalf("profile checkins: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("query should return public rows, got %d", len(items))
	}

	cases := []struct {
		name       string
		public     bool
		deleted    bool
		viewer     string
		areFriends bool
		want       checkins.ProfileAccess
	}{
		{"anon private", false, false, "", false, checkins.ProfileNeedsLogin},
		{"anon public", true, false, "", false, checkins.ProfileVisible},
		{"stranger private", false, false, e.carol.ID, false, checkins.ProfileHidden},
		{"stranger public", true, false, e.carol.ID, false, checkins.ProfileVisible},
		{"friend private", false, false, e.bob.ID, true, checkins.ProfileVisible},
		{"owner private", false, false, e.alice.ID, false, checkins.ProfileVisible},
		{"deleted public", true, true, "", false, checkins.ProfileHidden},
		{"deleted owner", true, true, e.alice.ID, false, checkins.ProfileHidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkins.DecideProfileAccess(tc.public, tc.deleted, tc.viewer, e.alice.ID, tc.areFriends)
			if got != tc.want {
				t.Fatalf("access = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProfileCheckinsFriendAndOwner(t *testing.T) {
	t.Run("friend sees public and friends", func(t *testing.T) {
		e := newTestEnv(t)
		e.befriend(t, e.alice, e.bob)
		pub := e.createCheckin(t, e.alice, "public")
		friends := e.createCheckin(t, e.alice, "friends")
		e.createCheckin(t, e.alice, "private")

		items, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{
			ViewerID: e.bob.ID, TargetID: e.alice.ID,
		})
		if err != nil {
			t.Fatalf("friend profile checkins: %v", err)
		}
		got := ids(items)
		if len(items) != 2 || !got[pub.ID] || !got[friends.ID] {
			t.Fatalf("friend items = %+v, want public+friends", items)
		}
	})

	t.Run("stranger sees only public", func(t *testing.T) {
		e := newTestEnv(t)
		pub := e.createCheckin(t, e.alice, "public")
		e.createCheckin(t, e.alice, "friends")
		e.createCheckin(t, e.alice, "private")

		items, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{
			ViewerID: e.carol.ID, TargetID: e.alice.ID,
		})
		if err != nil {
			t.Fatalf("stranger profile checkins: %v", err)
		}
		if len(items) != 1 || items[0].ID != pub.ID {
			t.Fatalf("stranger items = %+v, want only public", items)
		}
	})

	t.Run("owner sees all", func(t *testing.T) {
		e := newTestEnv(t)
		e.createCheckin(t, e.alice, "public")
		e.createCheckin(t, e.alice, "friends")
		e.createCheckin(t, e.alice, "private")

		items, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{
			ViewerID: e.alice.ID, TargetID: e.alice.ID,
		})
		if err != nil {
			t.Fatalf("owner profile checkins: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("owner items = %d, want 3", len(items))
		}
	})
}

func TestProfileCheckinsExcludesDeleted(t *testing.T) {
	t.Run("deleted check-in", func(t *testing.T) {
		e := newTestEnv(t)
		gone := e.createCheckin(t, e.alice, "public")
		kept := e.createCheckin(t, e.alice, "public")
		if err := e.svc.Delete(e.ctx, e.alice.ID, gone.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		items, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{TargetID: e.alice.ID})
		if err != nil {
			t.Fatalf("profile checkins: %v", err)
		}
		if len(items) != 1 || items[0].ID != kept.ID {
			t.Fatalf("items = %+v, want only kept row", items)
		}
	})

	t.Run("deleted user", func(t *testing.T) {
		e := newTestEnv(t)
		e.createCheckin(t, e.alice, "public")
		if err := e.db.Client.User.UpdateOneID(e.alice.ID).
			SetStatus(user.StatusDeleted).Exec(e.ctx); err != nil {
			t.Fatalf("delete user: %v", err)
		}
		items, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{TargetID: e.alice.ID})
		if err != nil {
			t.Fatalf("profile checkins: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("deleted user items = %+v, want none", items)
		}
	})

	t.Run("soft-deleted user", func(t *testing.T) {
		e := newTestEnv(t)
		e.createCheckin(t, e.alice, "public")
		if err := e.db.Client.User.UpdateOneID(e.alice.ID).
			SetDeletedAt("2026-01-01T00:00:00Z").Exec(e.ctx); err != nil {
			t.Fatalf("soft delete user: %v", err)
		}
		items, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{TargetID: e.alice.ID})
		if err != nil {
			t.Fatalf("profile checkins: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("soft-deleted user items = %+v, want none", items)
		}
	})
}

func TestProfileCheckinsCursor(t *testing.T) {
	e := newTestEnv(t)
	made := make([]string, 0, 4)
	for i, at := range []string{
		"2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z",
		"2026-01-03T00:00:00Z", "2026-01-04T00:00:00Z",
	} {
		c := e.createCheckin(t, e.alice, "public")
		if err := e.db.Client.Checkin.UpdateOneID(c.ID).SetCreatedAt(at).Exec(e.ctx); err != nil {
			t.Fatalf("set created_at %d: %v", i, err)
		}
		made = append(made, c.ID)
	}

	first, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{
		TargetID: e.alice.ID, Limit: 2,
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first) != 2 || first[0].ID != made[3] || first[1].ID != made[2] {
		t.Fatalf("first page = %+v, want newest two", first)
	}

	second, err := e.svc.ProfileCheckins(e.ctx, checkins.ProfileCheckinsFilter{
		TargetID: e.alice.ID, Limit: 2, Before: first[len(first)-1].CreatedAt,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second) != 2 || second[0].ID != made[1] || second[1].ID != made[0] {
		t.Fatalf("second page = %+v, want older two", second)
	}
	seen := ids(first)
	for _, it := range second {
		if seen[it.ID] {
			t.Fatalf("duplicate row %s across pages", it.ID)
		}
	}
}

// profileFilterFixture builds four public check-ins on alice's profile:
//
//	a newest-poor: 2026-01-01, rating 5,  0 toasts, no photo
//	b             2026-01-02, rating 10, 2 toasts, photo
//	c             2026-01-03, rating 20, 1 toast,  no photo
//	d newest-best:2026-01-04, no rating, 3 toasts, photo
func profileFilterFixture(t *testing.T) (*testEnv, map[string]string) {
	t.Helper()
	e := newTestEnv(t)
	ids := map[string]string{}
	type spec struct {
		name   string
		at     string
		rating *int
		photo  bool
		likes  []string
	}
	r5, r10, r20 := 5, 10, 20
	specs := []spec{
		{"a", "2026-01-01T00:00:00Z", &r5, false, nil},
		{"b", "2026-01-02T00:00:00Z", &r10, true, []string{e.bob.ID, e.carol.ID}},
		{"c", "2026-01-03T00:00:00Z", &r20, false, []string{e.bob.ID}},
		{"d", "2026-01-04T00:00:00Z", nil, true, []string{e.alice.ID, e.bob.ID, e.carol.ID}},
	}
	for _, sp := range specs {
		c := e.createCheckin(t, e.alice, "public")
		upd := e.db.Client.Checkin.UpdateOneID(c.ID).SetCreatedAt(sp.at)
		if sp.rating != nil {
			upd = upd.SetRating(*sp.rating)
		}
		if sp.photo {
			upd = upd.SetPhotoMediaID("photo-" + sp.name)
		}
		if err := upd.Exec(e.ctx); err != nil {
			t.Fatalf("update check-in %s: %v", sp.name, err)
		}
		for _, uid := range sp.likes {
			if _, err := e.db.Client.CheckinLike.Create().
				SetCheckinID(c.ID).SetUserID(uid).Save(e.ctx); err != nil {
				t.Fatalf("toast %s: %v", sp.name, err)
			}
		}
		ids[sp.name] = c.ID
	}
	return e, ids
}

func assertNames(t *testing.T, items []checkins.ProfileItem, ids map[string]string, want ...string) {
	t.Helper()
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d (%v)", len(items), len(want), want)
	}
	for i, name := range want {
		if items[i].ID != ids[name] {
			t.Fatalf("item %d = %s, want %s", i, items[i].ID, name)
		}
	}
}

func TestProfileCheckinsFilters(t *testing.T) {
	e, ids := profileFilterFixture(t)
	min5, min10, max10, minLikes := 5, 10, 10, 2

	cases := []struct {
		name string
		f    checkins.ProfileCheckinsFilter
		want []string
	}{
		{
			name: "from is inclusive",
			f:    checkins.ProfileCheckinsFilter{TargetID: e.alice.ID, From: "2026-01-02T00:00:00Z"},
			want: []string{"d", "c", "b"},
		},
		{
			name: "to is exclusive",
			f:    checkins.ProfileCheckinsFilter{TargetID: e.alice.ID, To: "2026-01-03T00:00:00Z"},
			want: []string{"b", "a"},
		},
		{
			name: "from and to window",
			f: checkins.ProfileCheckinsFilter{
				TargetID: e.alice.ID,
				From:     "2026-01-02T00:00:00Z",
				To:       "2026-01-04T00:00:00Z",
			},
			want: []string{"c", "b"},
		},
		{
			name: "rating min excludes null ratings",
			f:    checkins.ProfileCheckinsFilter{TargetID: e.alice.ID, RatingMin: &min10},
			want: []string{"c", "b"},
		},
		{
			name: "rating max",
			f:    checkins.ProfileCheckinsFilter{TargetID: e.alice.ID, RatingMax: &max10},
			want: []string{"b", "a"},
		},
		{
			name: "rating range",
			f:    checkins.ProfileCheckinsFilter{TargetID: e.alice.ID, RatingMin: &min5, RatingMax: &max10},
			want: []string{"b", "a"},
		},
		{
			name: "minimum toasts",
			f:    checkins.ProfileCheckinsFilter{TargetID: e.alice.ID, MinLikes: &minLikes},
			want: []string{"d", "b"},
		},
		{
			name: "has photo",
			f:    checkins.ProfileCheckinsFilter{TargetID: e.alice.ID, HasPhoto: true},
			want: []string{"d", "b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := e.svc.ProfileCheckinsPage(e.ctx, tc.f)
			if err != nil {
				t.Fatalf("profile checkins page: %v", err)
			}
			assertNames(t, res.Items, ids, tc.want...)
			if !res.Filtered || res.Mode != checkins.ProfileModeOffset {
				t.Fatalf("mode = %q filtered=%v, want offset/true", res.Mode, res.Filtered)
			}
			if res.HasNext {
				t.Fatalf("unexpected HasNext for a short page")
			}
		})
	}
}

func TestProfileCheckinsSorts(t *testing.T) {
	e, ids := profileFilterFixture(t)

	cases := []struct {
		name string
		sort string
		want []string
	}{
		{"default newest", "", []string{"d", "c", "b", "a"}},
		{"explicit newest", "newest", []string{"d", "c", "b", "a"}},
		{"oldest", "oldest", []string{"a", "b", "c", "d"}},
		{"rating high puts nulls last", "rating_high", []string{"c", "b", "a", "d"}},
		{"rating low puts nulls last", "rating_low", []string{"a", "b", "c", "d"}},
		{"likes", "likes", []string{"d", "b", "c", "a"}},
		{"unknown falls back to newest", "bogus", []string{"d", "c", "b", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
				TargetID: e.alice.ID, Sort: tc.sort,
			})
			if err != nil {
				t.Fatalf("profile checkins page: %v", err)
			}
			assertNames(t, res.Items, ids, tc.want...)
		})
	}
}

func TestProfileCheckinsPagination(t *testing.T) {
	e, ids := profileFilterFixture(t)

	t.Run("offset pages", func(t *testing.T) {
		first, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
			TargetID: e.alice.ID, Sort: "newest", Page: 1, PageSize: 2,
		})
		if err != nil {
			t.Fatalf("page 1: %v", err)
		}
		assertNames(t, first.Items, ids, "d", "c")
		if !first.Filtered || first.Mode != checkins.ProfileModeOffset || first.Page != 1 || first.PageSize != 2 {
			t.Fatalf("page 1 metadata = %+v", first)
		}
		if !first.HasNext {
			t.Fatalf("page 1 should have a next page")
		}

		second, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
			TargetID: e.alice.ID, Sort: "newest", Page: 2, PageSize: 2,
		})
		if err != nil {
			t.Fatalf("page 2: %v", err)
		}
		assertNames(t, second.Items, ids, "b", "a")
		if second.HasNext {
			t.Fatalf("page 2 should be the last page")
		}

		empty, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
			TargetID: e.alice.ID, Sort: "newest", Page: 3, PageSize: 2,
		})
		if err != nil {
			t.Fatalf("page 3: %v", err)
		}
		if len(empty.Items) != 0 || empty.HasNext {
			t.Fatalf("page 3 = %+v, want empty with no next", empty)
		}
	})

	t.Run("cursor path unchanged", func(t *testing.T) {
		first, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
			TargetID: e.alice.ID, Limit: 2,
		})
		if err != nil {
			t.Fatalf("cursor page 1: %v", err)
		}
		assertNames(t, first.Items, ids, "d", "c")
		if first.Filtered || first.Mode != checkins.ProfileModeCursor {
			t.Fatalf("cursor metadata = %+v, want cursor/unfiltered", first)
		}
		if !first.HasNext {
			t.Fatalf("cursor page 1 should have a next page")
		}

		second, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
			TargetID: e.alice.ID, Limit: 2, Before: first.Items[len(first.Items)-1].CreatedAt,
		})
		if err != nil {
			t.Fatalf("cursor page 2: %v", err)
		}
		assertNames(t, second.Items, ids, "b", "a")
		if second.HasNext {
			t.Fatalf("cursor page 2 should be the last page")
		}
	})
}

func TestProfileCheckinsVisibilityFilter(t *testing.T) {
	e := newTestEnv(t)
	e.befriend(t, e.alice, e.bob)
	pub := e.createCheckin(t, e.alice, "public")
	friends := e.createCheckin(t, e.alice, "friends")
	private := e.createCheckin(t, e.alice, "private")

	t.Run("owner may narrow by visibility", func(t *testing.T) {
		res, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
			ViewerID: e.alice.ID, TargetID: e.alice.ID, Visibility: "private",
		})
		if err != nil {
			t.Fatalf("owner filter: %v", err)
		}
		if len(res.Items) != 1 || res.Items[0].ID != private.ID {
			t.Fatalf("owner private filter = %+v, want only private", res.Items)
		}
	})

	t.Run("invalid visibility ignored for owner", func(t *testing.T) {
		res, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
			ViewerID: e.alice.ID, TargetID: e.alice.ID, Visibility: "secret",
		})
		if err != nil {
			t.Fatalf("owner invalid filter: %v", err)
		}
		if len(res.Items) != 3 {
			t.Fatalf("owner invalid filter returned %d rows, want all 3", len(res.Items))
		}
	})

	t.Run("non-owner filter is ignored and cannot widen", func(t *testing.T) {
		res, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
			ViewerID: e.bob.ID, TargetID: e.alice.ID, Visibility: "private",
		})
		if err != nil {
			t.Fatalf("friend filter: %v", err)
		}
		got := ids(res.Items)
		if len(res.Items) != 2 || !got[pub.ID] || !got[friends.ID] || got[private.ID] {
			t.Fatalf("friend filter = %+v, want public+friends only", res.Items)
		}

		stranger, err := e.svc.ProfileCheckinsPage(e.ctx, checkins.ProfileCheckinsFilter{
			ViewerID: e.carol.ID, TargetID: e.alice.ID, Visibility: "friends",
		})
		if err != nil {
			t.Fatalf("stranger filter: %v", err)
		}
		if len(stranger.Items) != 1 || stranger.Items[0].ID != pub.ID {
			t.Fatalf("stranger filter = %+v, want only public", stranger.Items)
		}
	})
}

func TestProfileStatsAndLists(t *testing.T) {
	e := newTestEnv(t)

	venue, err := e.db.Client.Venue.Create().
		SetName("Corner Shop").SetSlug("corner-shop").Save(e.ctx)
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	e.createCheckin(t, e.alice, "public")
	if _, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
		DrinkID: e.drink.ID, Visibility: "public", VenueID: &venue.ID,
	}); err != nil {
		t.Fatalf("venue checkin: %v", err)
	}
	if _, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
		DrinkID: e.drink.ID, Visibility: "private",
	}); err != nil {
		t.Fatalf("private checkin: %v", err)
	}

	anon, err := e.svc.ProfileStats(e.ctx, "", e.alice.ID)
	if err != nil {
		t.Fatalf("anon stats: %v", err)
	}
	if anon.Pours != 2 || anon.Venues != 1 || anon.Styles != 0 {
		t.Fatalf("anon stats = %+v, want pours=2 venues=1 styles=0", anon)
	}

	owner, err := e.svc.ProfileStats(e.ctx, e.alice.ID, e.alice.ID)
	if err != nil {
		t.Fatalf("owner stats: %v", err)
	}
	if owner.Pours != 3 || owner.Venues != 1 {
		t.Fatalf("owner stats = %+v, want pours=3 venues=1", owner)
	}

	for _, visibility := range []string{"public", "friends", "private"} {
		if _, err := e.db.Client.List.Create().
			SetUserID(e.alice.ID).
			SetName("List " + visibility).
			SetVisibility(list.Visibility(visibility)).
			Save(e.ctx); err != nil {
			t.Fatalf("list %s: %v", visibility, err)
		}
	}
	anonLists, err := e.svc.ProfileLists(e.ctx, "", e.alice.ID)
	if err != nil {
		t.Fatalf("anon lists: %v", err)
	}
	if len(anonLists) != 1 || anonLists[0].Name != "List public" {
		t.Fatalf("anon lists = %+v, want only public", anonLists)
	}
	ownerLists, err := e.svc.ProfileLists(e.ctx, e.alice.ID, e.alice.ID)
	if err != nil {
		t.Fatalf("owner lists: %v", err)
	}
	if len(ownerLists) != 3 {
		t.Fatalf("owner lists = %d, want 3", len(ownerLists))
	}
}
