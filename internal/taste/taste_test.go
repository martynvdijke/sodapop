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
	addCheckinWithVisibility(t, database, userID, drinkID, rating, checkin.VisibilityFriends)
}

func addCheckinWithVisibility(t *testing.T, database *db.DB, userID string, drinkID, rating int, visibility checkin.Visibility) {
	t.Helper()
	database.Client.Checkin.Create().
		SetUserID(userID).
		SetDrinkID(drinkID).
		SetRating(rating).
		SetVisibility(visibility).
		SaveX(context.Background())
}

func TestQueue(t *testing.T) {
	database := newTestDB(t)
	ctx := context.Background()

	viewer := createUser(t, database, "queue-viewer")
	twin := createUser(t, database, "queue-twin")
	friend(t, database, viewer, twin)

	style := database.Client.Style.Create().SetName("Cola").SetSlug("cola").SaveX(ctx)
	brand := database.Client.Brand.Create().SetName("Fizz Co").SetSlug("fizz-co").SaveX(ctx)
	newStyle := database.Client.Style.Create().SetName("Root Beer").SetSlug("root-beer").SaveX(ctx)
	newBrand := database.Client.Brand.Create().SetName("Other Co").SetSlug("other-co").SaveX(ctx)

	makeDrink := func(slug string, brandID, styleID int) int {
		return database.Client.Drink.Create().
			SetName("Drink " + slug).
			SetSlug(slug).
			SetBrandID(brandID).
			SetStyleID(styleID).
			SaveX(ctx).ID
	}

	shared := []int{
		makeDrink("s1", brand.ID, style.ID),
		makeDrink("s2", brand.ID, style.ID),
		makeDrink("s3", brand.ID, style.ID),
	}
	for i, drinkID := range shared {
		addCheckin(t, database, viewer.ID, drinkID, []int{20, 18, 16}[i])
		addCheckin(t, database, twin.ID, drinkID, []int{19, 17, 15}[i])
	}

	// Twin-only candidates, best-first. Distinct brand and style mean the
	// viewer has never had them.
	candidates := []int{
		makeDrink("q1", newBrand.ID, newStyle.ID),
		makeDrink("q2", newBrand.ID, newStyle.ID),
		makeDrink("q3", newBrand.ID, newStyle.ID),
		makeDrink("q4", newBrand.ID, newStyle.ID),
		makeDrink("q5", newBrand.ID, newStyle.ID),
		makeDrink("q6", newBrand.ID, newStyle.ID),
	}
	for i, drinkID := range candidates {
		addCheckin(t, database, twin.ID, drinkID, []int{20, 18, 16, 15, 15, 14}[i])
	}

	// Already checked in by the viewer: must never appear.
	tried := makeDrink("qtried", brand.ID, style.ID)
	addCheckin(t, database, viewer.ID, tried, 20)
	addCheckin(t, database, twin.ID, tried, 20)

	service := New(database)
	queue, err := service.Queue(ctx, viewer.ID, 0)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if len(queue) != 5 {
		t.Fatalf("queue length = %d, want 5", len(queue))
	}
	if queue[0].DrinkID != candidates[0] {
		t.Fatalf("first queue item = %d, want %d", queue[0].DrinkID, candidates[0])
	}
	for i := 1; i < len(queue); i++ {
		if queue[i-1].Score < queue[i].Score {
			t.Fatalf("queue not sorted by score desc: %+v", queue)
		}
	}
	for _, item := range queue {
		if item.DrinkID == tried {
			t.Errorf("already tried drink %d must be excluded", tried)
		}
		if item.TwinCount != 1 {
			t.Errorf("twin count for drink %d = %d, want 1", item.DrinkID, item.TwinCount)
		}
	}

	first := queue[0]
	if !hasReason(first.Reasons, "1 taste twin rated it 20+") {
		t.Errorf("reasons = %v, want twin rating reason", first.Reasons)
	}
	if !hasReason(first.Reasons, "New style for you") {
		t.Errorf("reasons = %v, want new style reason", first.Reasons)
	}
	if !hasReason(first.Reasons, "New brand for you") {
		t.Errorf("reasons = %v, want new brand reason", first.Reasons)
	}
	if len(first.Reasons) > 3 {
		t.Errorf("got %d reasons, want at most 3", len(first.Reasons))
	}

	limited, err := service.Queue(ctx, viewer.ID, 2)
	if err != nil {
		t.Fatalf("limited queue: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("limited queue length = %d, want 2", len(limited))
	}
}

func TestQueuePrivacy(t *testing.T) {
	database := newTestDB(t)
	ctx := context.Background()

	viewer := createUser(t, database, "privacy-viewer")
	twin := createUser(t, database, "privacy-twin")
	privateOnly := createUser(t, database, "privacy-only")
	friend(t, database, viewer, twin)
	friend(t, database, viewer, privateOnly)

	style := database.Client.Style.Create().SetName("Cola").SetSlug("cola").SaveX(ctx)
	brand := database.Client.Brand.Create().SetName("Fizz Co").SetSlug("fizz-co").SaveX(ctx)

	makeDrink := func(slug string) int {
		return database.Client.Drink.Create().
			SetName("Drink " + slug).
			SetSlug(slug).
			SetBrandID(brand.ID).
			SetStyleID(style.ID).
			SaveX(ctx).ID
	}

	shared := []int{makeDrink("p1"), makeDrink("p2"), makeDrink("p3")}
	for i, drinkID := range shared {
		addCheckin(t, database, viewer.ID, drinkID, []int{20, 18, 16}[i])
		addCheckin(t, database, twin.ID, drinkID, []int{19, 17, 15}[i])
	}

	// A fourth shared drink rated privately must not extend the twin overlap.
	hiddenCommon := makeDrink("p4")
	addCheckinWithVisibility(t, database, twin.ID, hiddenCommon, 1, checkin.VisibilityPrivate)

	// A private check-in on a brand-new drink must never become a suggestion.
	secret := makeDrink("secret")
	addCheckinWithVisibility(t, database, twin.ID, secret, 20, checkin.VisibilityPrivate)

	// A friend whose shared ratings are entirely private has no taste twin.
	for i, drinkID := range shared {
		addCheckinWithVisibility(t, database, privateOnly.ID, drinkID, []int{20, 18, 16}[i], checkin.VisibilityPrivate)
	}
	privateOnlyDrink := makeDrink("private-only")
	addCheckinWithVisibility(t, database, privateOnly.ID, privateOnlyDrink, 20, checkin.VisibilityPrivate)

	service := New(database)

	twins, err := service.Twins(ctx, viewer.ID, 0)
	if err != nil {
		t.Fatalf("twins: %v", err)
	}
	for _, tw := range twins {
		if tw.UserID == privateOnly.ID {
			t.Fatalf("private check-ins produced a twin: %+v", tw)
		}
		if tw.UserID == twin.ID && tw.Common != 3 {
			t.Errorf("twin common = %d, want 3 (private check-in ignored)", tw.Common)
		}
	}

	recs, err := service.Recommendations(ctx, viewer.ID, 0)
	if err != nil {
		t.Fatalf("recommendations: %v", err)
	}
	for _, rec := range recs {
		if rec.DrinkID == secret || rec.DrinkID == privateOnlyDrink || rec.DrinkID == hiddenCommon {
			t.Errorf("private check-in leaked into recommendations: %+v", rec)
		}
	}

	queue, err := service.Queue(ctx, viewer.ID, 0)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	for _, item := range queue {
		if item.DrinkID == secret || item.DrinkID == privateOnlyDrink || item.DrinkID == hiddenCommon {
			t.Errorf("private check-in leaked into queue: %+v", item)
		}
	}
}

func hasReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
