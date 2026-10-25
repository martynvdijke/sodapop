package taste

import (
	"context"
	"fmt"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/drink"
	"sodapop/internal/db"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	// db.Init appends its own "?_pragma=..." suffix, so end the URI with "&"
	// to keep the shared-cache in-memory parameters intact.
	dsn := fmt.Sprintf("taste-%d?mode=memory&cache=shared&", time.Now().UnixNano())
	database, err := db.Init(dsn)
	if err != nil {
		t.Fatalf("init taste db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestTwinsAndRecommendations(t *testing.T) {
	database := newTestDB(t)
	ctx := context.Background()

	viewer := createUser(t, database, "viewer")
	twin := createUser(t, database, "twin")
	opposite := createUser(t, database, "opposite")

	// Canonical friendships: user_a < user_b.
	friend(t, database, viewer, twin)
	friend(t, database, viewer, opposite)

	style := database.Client.Style.Create().SetName("Cola").SetSlug("cola").SaveX(ctx)
	brand := database.Client.Brand.Create().SetName("Fizz Co").SetSlug("fizz-co").SaveX(ctx)

	makeDrink := func(slug string, merged bool) int {
		builder := database.Client.Drink.Create().
			SetName("Drink " + slug).
			SetSlug(slug).
			SetBrandID(brand.ID).
			SetStyleID(style.ID)
		if merged {
			builder.SetStatus(drink.StatusMerged)
		}
		return builder.SaveX(ctx).ID
	}

	shared := []int{makeDrink("d1", false), makeDrink("d2", false), makeDrink("d3", false), makeDrink("d4", false), makeDrink("d5", false)}
	loved := makeDrink("d6", false)
	liked := makeDrink("d7", false)
	oppositeOnly := makeDrink("d8", false)
	alreadyTried := makeDrink("d9", false)
	merged := makeDrink("d10", true)

	viewerRatings := []int{20, 18, 16, 14, 12}
	for i, drinkID := range shared {
		addCheckin(t, database, viewer.ID, drinkID, viewerRatings[i])
	}
	addCheckin(t, database, viewer.ID, alreadyTried, 10)

	twinRatings := []int{19, 17, 15, 13, 11}
	for i, drinkID := range shared {
		addCheckin(t, database, twin.ID, drinkID, twinRatings[i])
	}
	addCheckin(t, database, twin.ID, loved, 20)
	addCheckin(t, database, twin.ID, liked, 18)
	addCheckin(t, database, twin.ID, alreadyTried, 20)
	addCheckin(t, database, twin.ID, merged, 20)

	oppositeRatings := []int{2, 4, 6, 8, 10}
	for i, drinkID := range shared {
		addCheckin(t, database, opposite.ID, drinkID, oppositeRatings[i])
	}
	addCheckin(t, database, opposite.ID, oppositeOnly, 20)

	service := New(database)

	twins, err := service.Twins(ctx, viewer.ID, 0)
	if err != nil {
		t.Fatalf("twins: %v", err)
	}
	if len(twins) != 2 {
		t.Fatalf("got %d twins, want 2", len(twins))
	}
	if twins[0].UserID != twin.ID {
		t.Errorf("first twin = %s, want %s (score %d vs %d)", twins[0].UserID, twin.ID, twins[0].Score, twins[1].Score)
	}
	if twins[0].Score <= twins[1].Score {
		t.Errorf("twin scores not descending: %d, %d", twins[0].Score, twins[1].Score)
	}
	if twins[0].Common != 6 {
		t.Errorf("first twin common = %d, want 6", twins[0].Common)
	}

	recs, err := service.Recommendations(ctx, viewer.ID, 0)
	if err != nil {
		t.Fatalf("recommendations: %v", err)
	}
	got := map[int]Recommendation{}
	for _, rec := range recs {
		got[rec.DrinkID] = rec
	}
	if _, ok := got[loved]; !ok {
		t.Errorf("missing recommendation for drink %d", loved)
	}
	if _, ok := got[liked]; !ok {
		t.Errorf("missing recommendation for drink %d", liked)
	}
	if _, ok := got[oppositeOnly]; ok {
		t.Errorf("drink %d came from a low-similarity friend and must be excluded", oppositeOnly)
	}
	if _, ok := got[alreadyTried]; ok {
		t.Errorf("drink %d was already checked in by the viewer and must be excluded", alreadyTried)
	}
	if _, ok := got[merged]; ok {
		t.Errorf("merged drink %d must be excluded", merged)
	}
	if len(recs) == 0 || recs[0].DrinkID != loved {
		t.Errorf("first recommendation = %+v, want drink %d", recs, loved)
	}
	if rec := got[loved]; rec.Brand != "Fizz Co" || rec.Style != "Cola" {
		t.Errorf("recommendation brand/style = %q/%q, want Fizz Co/Cola", rec.Brand, rec.Style)
	}
	if rec := got[loved]; rec.Reason != "Loved by 1 taste twin" {
		t.Errorf("reason = %q", rec.Reason)
	}
}

func createUser(t *testing.T, database *db.DB, handle string) *ent.User {
	t.Helper()
	return database.Client.User.Create().
		SetHandle(handle).
		SetEmail(handle + "@example.test").
		SetDisplayName(handle).
		SaveX(context.Background())
}

func friend(t *testing.T, database *db.DB, a, b *ent.User) {
	t.Helper()
	x, y := a.ID, b.ID
	if x > y {
		x, y = y, x
	}
	database.Client.Friendship.Create().
		SetUserA(x).
		SetUserB(y).
		SetStatus("accepted").
		SetRequestedBy(a.ID).
		SaveX(context.Background())
}

func addCheckin(t *testing.T, database *db.DB, userID string, drinkID, rating int) {
	t.Helper()
	database.Client.Checkin.Create().
		SetUserID(userID).
		SetDrinkID(drinkID).
		SetRating(rating).
		SetVisibility(checkin.VisibilityFriends).
		SaveX(context.Background())
}
