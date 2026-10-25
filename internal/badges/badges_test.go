package badges

import (
	"context"
	"path/filepath"
	"testing"

	"sodapop/ent/activity"
	"sodapop/ent/checkin"
	"sodapop/ent/notification"
	"sodapop/ent/userbadge"
	"sodapop/internal/db"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "badges.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestEvaluateFirstPourAndProgress(t *testing.T) {
	ctx := context.Background()
	database := newTestDB(t)
	svc := New(database)

	user, err := database.Client.User.Create().
		SetHandle("ada").
		SetEmail("ada@example.com").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	style, err := database.Client.Style.Create().
		SetName("Cola").
		SetSlug("cola").
		Save(ctx)
	if err != nil {
		t.Fatalf("create style: %v", err)
	}

	drink, err := database.Client.Drink.Create().
		SetName("Test Cola").
		SetSlug("test-cola").
		SetStyleID(style.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create drink: %v", err)
	}

	if _, err := database.Client.Checkin.Create().
		SetUserID(user.ID).
		SetDrinkID(drink.ID).
		SetServing(checkin.ServingCan).
		SetCreatedAt("2024-01-15T12:00:00Z").
		Save(ctx); err != nil {
		t.Fatalf("create checkin: %v", err)
	}

	progress, err := svc.Progress(ctx, user.ID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	wantProgress := map[string]int{"checkins": 1, "distinct_styles": 1, "cans": 1, "glass": 0, "bottle": 0, "fountain": 0, "sparkling_water": 0, "late_night": 0}
	for key, want := range wantProgress {
		if got := progress[key]; got != want {
			t.Errorf("progress[%q] = %d, want %d", key, got, want)
		}
	}

	awarded, err := svc.Evaluate(ctx, user.ID)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !contains(awarded, "first-pour") {
		t.Fatalf("first evaluation awarded %v, want first-pour", awarded)
	}

	// A second evaluation must be idempotent and award nothing new.
	again, err := svc.Evaluate(ctx, user.ID)
	if err != nil {
		t.Fatalf("second evaluate: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second evaluation awarded %v, want nothing", again)
	}

	count, err := database.Client.UserBadge.Query().
		Where(userbadge.UserID(user.ID)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count badges: %v", err)
	}
	if count != 1 {
		t.Fatalf("user has %d badges, want 1", count)
	}

	notifications, err := database.Client.Notification.Query().
		Where(notification.UserID(user.ID), notification.KindEQ(notification.KindBadge)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if notifications != 1 {
		t.Fatalf("user has %d badge notifications, want 1", notifications)
	}

	activities, err := database.Client.Activity.Query().
		Where(activity.UserID(user.ID), activity.KindEQ(activity.KindBadge)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count activities: %v", err)
	}
	if activities != 1 {
		t.Fatalf("user has %d badge activities, want 1", activities)
	}

	list, err := svc.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("list is empty, want seeded badges")
	}
	var firstPour, tenPours *Status
	for i := range list {
		switch list[i].Slug {
		case "first-pour":
			firstPour = &list[i]
		case "ten-pours":
			tenPours = &list[i]
		}
	}
	if firstPour == nil || tenPours == nil {
		t.Fatalf("seeded badges missing from list: %+v", list)
	}
	if !firstPour.Earned || firstPour.Level != 1 || firstPour.AwardedAt == "" {
		t.Errorf("first-pour status = %+v, want earned level 1 with timestamp", *firstPour)
	}
	if tenPours.Earned || tenPours.Level != 0 || tenPours.AwardedAt != "" {
		t.Errorf("ten-pours status = %+v, want not earned", *tenPours)
	}
}

func TestEvaluateIgnoresUnknownRules(t *testing.T) {
	ctx := context.Background()
	database := newTestDB(t)
	svc := New(database)

	user, err := database.Client.User.Create().
		SetHandle("grace").
		SetEmail("grace@example.com").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := database.Client.Badge.Create().
		SetSlug("mystery").
		SetName("Mystery").
		SetRule("unknown_metric>=1").
		Save(ctx); err != nil {
		t.Fatalf("create badge: %v", err)
	}

	awarded, err := svc.Evaluate(ctx, user.ID)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if contains(awarded, "mystery") {
		t.Fatalf("unknown rule was awarded: %v", awarded)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
