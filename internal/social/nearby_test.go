package social_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/user"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

// nearbyViewer is a fixed, already-snapped point used by every test.
const (
	nearbyLat = 51.500
	nearbyLng = -0.120
)

func newNearbyFixture(t *testing.T) (context.Context, *db.DB, *social.Service, *ent.User) {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "nearby.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	viewer := newNearbyUser(t, ctx, database, "alice", "Alice")
	return ctx, database, social.New(database), viewer
}

func newNearbyUser(t *testing.T, ctx context.Context, database *db.DB, handle, name string) *ent.User {
	t.Helper()
	u, err := database.Client.User.Create().
		SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(name).Save(ctx)
	if err != nil {
		t.Fatalf("user %s: %v", handle, err)
	}
	return u
}

func newNearbyVenue(t *testing.T, ctx context.Context, database *db.DB, slug string, lat, lng float64) *ent.Venue {
	t.Helper()
	v, err := database.Client.Venue.Create().
		SetName(slug).SetSlug(slug).SetLat(lat).SetLng(lng).Save(ctx)
	if err != nil {
		t.Fatalf("venue %s: %v", slug, err)
	}
	return v
}

func newNearbyFriend(t *testing.T, ctx context.Context, database *db.DB, a, b *ent.User) {
	t.Helper()
	x, y := a.ID, b.ID
	if x > y {
		x, y = y, x
	}
	if _, err := database.Client.Friendship.Create().
		SetUserA(x).SetUserB(y).SetRequestedBy(a.ID).SetStatus("accepted").Save(ctx); err != nil {
		t.Fatalf("friendship: %v", err)
	}
}

func newNearbyDrink(t *testing.T, ctx context.Context, database *db.DB) *ent.Drink {
	t.Helper()
	d, err := database.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	return d
}

func newVenueCheckin(t *testing.T, ctx context.Context, database *db.DB, owner *ent.User, drink *ent.Drink, venue *ent.Venue, visibility, createdAt string) *ent.Checkin {
	t.Helper()
	b := database.Client.Checkin.Create().
		SetUserID(owner.ID).
		SetDrinkID(drink.ID).
		SetVisibility(checkin.Visibility(visibility))
	if venue != nil {
		b = b.SetVenueID(venue.ID)
	}
	if createdAt != "" {
		b = b.SetCreatedAt(createdAt)
	}
	c, err := b.Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return c
}

func nearbyIDs(items []social.NearbyItem) map[string]social.NearbyItem {
	out := make(map[string]social.NearbyItem, len(items))
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}

func TestNearbyVisibilityIsFriendScoped(t *testing.T) {
	ctx, database, svc, alice := newNearbyFixture(t)
	bob := newNearbyUser(t, ctx, database, "bob", "Bob")
	carol := newNearbyUser(t, ctx, database, "carol", "Carol")
	dave := newNearbyUser(t, ctx, database, "dave", "Dave")
	erin := newNearbyUser(t, ctx, database, "erin", "Erin")
	frank := newNearbyUser(t, ctx, database, "frank", "Frank")
	gina := newNearbyUser(t, ctx, database, "gina", "Gina")
	hank := newNearbyUser(t, ctx, database, "hank", "Hank")

	newNearbyFriend(t, ctx, database, alice, bob)
	newNearbyFriend(t, ctx, database, alice, erin)
	newNearbyFriend(t, ctx, database, alice, frank)
	newNearbyFriend(t, ctx, database, alice, gina)
	newNearbyFriend(t, ctx, database, alice, hank)
	if err := svc.Follow(ctx, alice.ID, "carol"); err != nil {
		t.Fatalf("follow carol: %v", err)
	}
	if err := svc.Block(ctx, alice.ID, "erin"); err != nil {
		t.Fatalf("block erin: %v", err)
	}
	if err := svc.Block(ctx, frank.ID, "alice"); err != nil {
		t.Fatalf("frank block alice: %v", err)
	}

	drink := newNearbyDrink(t, ctx, database)
	venue := newNearbyVenue(t, ctx, database, "near", nearbyLat, nearbyLng)
	unlocated := newNearbyVenue(t, ctx, database, "unlocated", 0, 0)
	target := newNearbyVenue(t, ctx, database, "target", nearbyLat, nearbyLng)
	merged := newNearbyVenue(t, ctx, database, "merged", nearbyLat, nearbyLng)
	if err := database.Client.Venue.UpdateOneID(merged.ID).SetMergedIntoID(target.ID).Exec(ctx); err != nil {
		t.Fatalf("merge venue: %v", err)
	}

	ownPublic := newVenueCheckin(t, ctx, database, alice, drink, venue, "public", "")
	ownPrivate := newVenueCheckin(t, ctx, database, alice, drink, venue, "private", "")
	bobPublic := newVenueCheckin(t, ctx, database, bob, drink, venue, "public", "")
	bobFriends := newVenueCheckin(t, ctx, database, bob, drink, venue, "friends", "")
	newVenueCheckin(t, ctx, database, bob, drink, venue, "private", "")
	carolPublic := newVenueCheckin(t, ctx, database, carol, drink, venue, "public", "")
	newVenueCheckin(t, ctx, database, carol, drink, venue, "friends", "")
	newVenueCheckin(t, ctx, database, dave, drink, venue, "public", "")
	newVenueCheckin(t, ctx, database, erin, drink, venue, "public", "")
	newVenueCheckin(t, ctx, database, frank, drink, venue, "public", "")
	newVenueCheckin(t, ctx, database, bob, drink, unlocated, "public", "")
	newVenueCheckin(t, ctx, database, bob, drink, merged, "public", "")

	deletedCheckin := newVenueCheckin(t, ctx, database, bob, drink, venue, "public", "")
	if err := database.Client.Checkin.UpdateOneID(deletedCheckin.ID).SetDeletedAt(db.Now()).Exec(ctx); err != nil {
		t.Fatalf("delete checkin: %v", err)
	}
	newVenueCheckin(t, ctx, database, bob, drink, nil, "public", "")

	deletedAuthor := newVenueCheckin(t, ctx, database, gina, drink, venue, "public", "")
	if err := database.Client.User.UpdateOneID(gina.ID).
		SetStatus(user.StatusDeleted).SetDeletedAt(db.Now()).Exec(ctx); err != nil {
		t.Fatalf("delete author: %v", err)
	}
	suspendedAuthor := newVenueCheckin(t, ctx, database, hank, drink, venue, "public", "")
	if err := database.Client.User.UpdateOneID(hank.ID).
		SetStatus(user.StatusSuspended).Exec(ctx); err != nil {
		t.Fatalf("suspend author: %v", err)
	}

	items, hasMore, next, err := svc.Nearby(ctx, alice.ID, nearbyLat, nearbyLng, 2, 20, "")
	if err != nil {
		t.Fatalf("nearby: %v", err)
	}
	if hasMore || next != "" {
		t.Fatalf("hasMore=%v next=%q, want no more", hasMore, next)
	}
	got := nearbyIDs(items)
	for _, want := range []string{ownPublic.ID, ownPrivate.ID, bobPublic.ID, bobFriends.ID, carolPublic.ID} {
		if _, ok := got[want]; !ok {
			t.Errorf("nearby missing %s", want)
		}
	}
	for name, id := range map[string]string{
		"deleted checkin":  deletedCheckin.ID,
		"deleted author":   deletedAuthor.ID,
		"suspended author": suspendedAuthor.ID,
	} {
		if _, ok := got[id]; ok {
			t.Errorf("nearby leaked %s (%s)", name, id)
		}
	}
	if len(got) != 5 {
		t.Errorf("nearby has %d items, want 5", len(got))
	}
	if item, ok := got[ownPublic.ID]; ok && item.VenueID != venue.ID {
		t.Errorf("nearby venue id = %d, want %d", item.VenueID, venue.ID)
	}
}

func TestNearbyRadiusBucketAndOrdering(t *testing.T) {
	ctx, database, svc, alice := newNearbyFixture(t)
	drink := newNearbyDrink(t, ctx, database)

	near := newNearbyVenue(t, ctx, database, "near", nearbyLat, nearbyLng)
	about120m := newNearbyVenue(t, ctx, database, "about120", nearbyLat+0.0011, nearbyLng)
	about300m := newNearbyVenue(t, ctx, database, "about300", nearbyLat+0.0027, nearbyLng)
	far := newNearbyVenue(t, ctx, database, "far", nearbyLat+0.05, nearbyLng)

	nearNew := newVenueCheckin(t, ctx, database, alice, drink, near, "public", "2026-01-02T00:00:00Z")
	nearOld := newVenueCheckin(t, ctx, database, alice, drink, near, "public", "2026-01-01T00:00:00Z")
	m120 := newVenueCheckin(t, ctx, database, alice, drink, about120m, "public", "2026-01-03T00:00:00Z")
	m300 := newVenueCheckin(t, ctx, database, alice, drink, about300m, "public", "2026-01-04T00:00:00Z")
	newVenueCheckin(t, ctx, database, alice, drink, far, "public", "")

	items, _, _, err := svc.Nearby(ctx, alice.ID, nearbyLat, nearbyLng, 1, 20, "")
	if err != nil {
		t.Fatalf("nearby: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4 (far venue must be excluded)", len(items))
	}
	wantOrder := []string{nearNew.ID, nearOld.ID, m120.ID, m300.ID}
	for i, want := range wantOrder {
		if items[i].ID != want {
			ids := make([]string, len(items))
			for j, item := range items {
				ids[j] = item.ID
			}
			t.Fatalf("order = %v, want %v", ids, wantOrder)
		}
	}

	byID := nearbyIDs(items)
	if byID[nearNew.ID].DistanceM != 0 || byID[nearOld.ID].DistanceM != 0 {
		t.Errorf("near venue distances = %d/%d, want 0/0", byID[nearNew.ID].DistanceM, byID[nearOld.ID].DistanceM)
	}
	if got := byID[m120.ID].DistanceM; got != 100 {
		t.Errorf("~120m distance bucket = %d, want 100", got)
	}
	if got := byID[m300.ID].DistanceM; got != 300 {
		t.Errorf("~300m distance bucket = %d, want 300", got)
	}
	for _, item := range items {
		if item.DistanceM%100 != 0 {
			t.Errorf("distance %d is not bucketed to 100m", item.DistanceM)
		}
	}
}

func TestNearbyCursorPagination(t *testing.T) {
	ctx, database, svc, alice := newNearbyFixture(t)
	drink := newNearbyDrink(t, ctx, database)
	offsets := []float64{0, 0.0009, 0.0018, 0.0027, 0.0036}
	want := make([]string, 0, len(offsets))
	for i, offset := range offsets {
		v := newNearbyVenue(t, ctx, database, "venue-"+string(rune('a'+i)), nearbyLat+offset, nearbyLng)
		c := newVenueCheckin(t, ctx, database, alice, drink, v, "public", "2026-01-01T00:00:00Z")
		want = append(want, c.ID)
	}

	var got []string
	cursor := ""
	for page := 0; page < 5; page++ {
		items, hasMore, next, err := svc.Nearby(ctx, alice.ID, nearbyLat, nearbyLng, 2, 2, cursor)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, item := range items {
			got = append(got, item.ID)
		}
		if !hasMore {
			if next != "" {
				t.Fatalf("page %d: next = %q with hasMore false", page, next)
			}
			break
		}
		if next == "" {
			t.Fatalf("page %d: hasMore true but empty next cursor", page)
		}
		cursor = next
	}
	if len(got) != len(want) {
		t.Fatalf("paged ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paged ids = %v, want %v", got, want)
		}
	}
}

func TestNearbyRejectsMalformedCursor(t *testing.T) {
	ctx, _, svc, alice := newNearbyFixture(t)
	for _, cursor := range []string{
		"not-base64!!",
		"QQ",                                   // "A"
		"M3xub3QtYS10aW1lfGlk",                 // "3|not-a-time|id"
		"LTEwfDIwMjYtMDEtMDFUMDA6MDA6MDBafGlk", // "-10|2026-01-01T00:00:00Z|id"
		"MHwyMDI2LTAxLTAxVDAwOjAwOjAwWnw",      // "0|2026-01-01T00:00:00Z|"
	} {
		if _, _, _, err := svc.Nearby(ctx, alice.ID, nearbyLat, nearbyLng, 2, 20, cursor); !errors.Is(err, social.ErrInvalidCursor) {
			t.Errorf("cursor %q: got %v, want ErrInvalidCursor", cursor, err)
		}
	}
}

func TestNearbyValidatesPointAndRadius(t *testing.T) {
	ctx, _, svc, alice := newNearbyFixture(t)
	tests := []struct {
		name     string
		lat      float64
		lng      float64
		radiusKM float64
		want     error
	}{
		{"lat too high", 90.1, 0, 2, social.ErrInvalidLocation},
		{"lat too low", -90.1, 0, 2, social.ErrInvalidLocation},
		{"lng too high", 0, 180.1, 2, social.ErrInvalidLocation},
		{"lng too low", 0, -180.1, 2, social.ErrInvalidLocation},
		{"radius below minimum", 51.5, -0.12, 0.09, social.ErrInvalidRadius},
		{"radius above maximum", 51.5, -0.12, 25.1, social.ErrInvalidRadius},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, _, err := svc.Nearby(ctx, alice.ID, tt.lat, tt.lng, tt.radiusKM, 20, ""); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNearbyRejectsInvalidLimit(t *testing.T) {
	ctx, _, svc, alice := newNearbyFixture(t)
	for _, limit := range []int{0, -1, 51} {
		if _, _, _, err := svc.Nearby(ctx, alice.ID, nearbyLat, nearbyLng, 2, limit, ""); !errors.Is(err, social.ErrInvalidLimit) {
			t.Errorf("limit %d: got %v, want ErrInvalidLimit", limit, err)
		}
	}
}
