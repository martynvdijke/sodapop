package checkins_test

import (
	"errors"
	"testing"

	"sodapop/internal/checkins"
)

func makePublic(t *testing.T, e *testEnv, userID string) {
	t.Helper()
	if _, err := e.db.Client.UserSetting.Create().
		SetUserID(userID).
		SetPublicProfile(true).
		Save(e.ctx); err != nil {
		t.Fatalf("public profile: %v", err)
	}
}

func TestPublicFeedOnlyPublicRows(t *testing.T) {
	e := newTestEnv(t)
	makePublic(t, e, e.alice.ID)

	pub := e.createCheckin(t, e.alice, "public")
	e.createCheckin(t, e.alice, "friends")
	e.createCheckin(t, e.alice, "private")
	// Carol never opted into the people directory, but her public pour is
	// still part of the global public feed.
	carolPub := e.createCheckin(t, e.carol, "public")

	items, err := e.svc.PublicFeed(e.ctx, checkins.PublicFeedFilter{Limit: 20})
	if err != nil {
		t.Fatalf("public feed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v, want alice's and carol's public pours", items)
	}
	byID := map[string]checkins.ProfileItem{}
	for _, it := range items {
		byID[it.ID] = it
		if it.Visibility != "public" {
			t.Fatalf("non-public visibility leaked: %+v", it)
		}
	}
	if _, ok := byID[pub.ID]; !ok {
		t.Fatalf("missing alice's pour %s", pub.ID)
	}
	if _, ok := byID[carolPub.ID]; !ok {
		t.Fatalf("missing carol's public pour %s", carolPub.ID)
	}
}

func TestPublicFeedCursor(t *testing.T) {
	e := newTestEnv(t)
	makePublic(t, e, e.alice.ID)

	first := e.createCheckin(t, e.alice, "public")
	second := e.createCheckin(t, e.alice, "public")

	page, err := e.svc.PublicFeed(e.ctx, checkins.PublicFeedFilter{Limit: 1})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("first page = %+v, want one row", page)
	}
	seen := page[0].ID
	page, err = e.svc.PublicFeed(e.ctx, checkins.PublicFeedFilter{
		Limit: 1, Before: checkins.Cursor(page[0].CreatedAt, page[0].ID),
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("second page = %+v, want one row", page)
	}
	if page[0].ID == seen {
		t.Fatalf("second page repeated %s", seen)
	}
	if got := map[string]bool{page[0].ID: true, seen: true}; !got[first.ID] || !got[second.ID] {
		t.Fatalf("pages covered %v, want %s and %s", got, first.ID, second.ID)
	}
}

func TestPublicPeopleListsOnlyOptedInProfiles(t *testing.T) {
	e := newTestEnv(t)
	makePublic(t, e, e.alice.ID)
	makePublic(t, e, e.bob.ID)
	e.createCheckin(t, e.alice, "public")
	e.createCheckin(t, e.bob, "public")
	e.createCheckin(t, e.alice, "private")

	people, err := e.svc.PublicPeople(e.ctx, 20)
	if err != nil {
		t.Fatalf("public people: %v", err)
	}
	if len(people) != 2 {
		t.Fatalf("people = %+v, want alice and bob", people)
	}
	byHandle := map[string]checkins.PublicPerson{}
	for _, p := range people {
		byHandle[p.Handle] = p
	}
	if byHandle["alice"].Pours != 1 || byHandle["bob"].Pours != 1 {
		t.Fatalf("pours = %+v, want one public pour each", people)
	}
	if byHandle["alice"].Styles != 0 {
		t.Fatalf("alice styles = %d, want 0 for an unclassified drink", byHandle["alice"].Styles)
	}
}

func TestPublicProfileRequiresOptIn(t *testing.T) {
	e := newTestEnv(t)
	makePublic(t, e, e.alice.ID)

	pub := e.createCheckin(t, e.alice, "public")
	e.createCheckin(t, e.alice, "friends")

	profile, stats, err := e.svc.PublicProfile(e.ctx, "alice")
	if err != nil {
		t.Fatalf("public profile: %v", err)
	}
	if profile.ID != e.alice.ID || stats.Pours != 1 {
		t.Fatalf("profile = %+v stats = %+v", profile, stats)
	}

	if _, _, err := e.svc.PublicProfile(e.ctx, "carol"); !errors.Is(err, checkins.ErrNotFound) {
		t.Fatalf("private profile: got %v, want ErrNotFound", err)
	}

	items, err := e.svc.PublicUserCheckins(e.ctx, "alice", 20, "")
	if err != nil {
		t.Fatalf("public user checkins: %v", err)
	}
	if len(items) != 1 || items[0].ID != pub.ID {
		t.Fatalf("items = %+v, want only public", items)
	}
}

func TestPublicCheckinHidesNonPublicRows(t *testing.T) {
	e := newTestEnv(t)
	makePublic(t, e, e.alice.ID)

	pub := e.createCheckin(t, e.alice, "public")
	friends := e.createCheckin(t, e.alice, "friends")

	if _, err := e.svc.PublicCheckin(e.ctx, pub.ID); err != nil {
		t.Fatalf("public checkin: %v", err)
	}
	if _, err := e.svc.PublicCheckin(e.ctx, friends.ID); !errors.Is(err, checkins.ErrNotFound) {
		t.Fatalf("friends checkin: got %v, want ErrNotFound", err)
	}
}
