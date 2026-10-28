package badges

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sodapop/ent/activity"
	"sodapop/ent/badge"
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

func TestParseThresholds(t *testing.T) {
	tests := []struct {
		rule       string
		wantKey    string
		wantLevels []int
		wantOK     bool
	}{
		{rule: "checkins>=10", wantKey: "checkins", wantLevels: []int{10}, wantOK: true},
		{rule: " checkins >= 10 ", wantKey: "checkins", wantLevels: []int{10}, wantOK: true},
		{rule: "distinct_styles>=5,15,30", wantKey: "distinct_styles", wantLevels: []int{5, 15, 30}, wantOK: true},
		{rule: "friends>=1, 10", wantKey: "friends", wantLevels: []int{1, 10}, wantOK: true},
		{rule: "checkins>=0", wantKey: "checkins", wantLevels: []int{0}, wantOK: true},
		{rule: "", wantOK: false},
		{rule: "checkins", wantOK: false},
		{rule: ">=10", wantOK: false},
		{rule: "checkins>=", wantOK: false},
		{rule: "checkins>=abc", wantOK: false},
		{rule: "checkins>=-1", wantOK: false},
		{rule: "checkins>=10,-2", wantOK: false},
		{rule: "checkins>=10,", wantOK: false},
		{rule: "checkins>=10,,20", wantOK: false},
		{rule: "checkins>=20,10", wantOK: false},
		{rule: "checkins>=10,10", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.rule, func(t *testing.T) {
			key, levels, ok := parseThresholds(tt.rule)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if key != tt.wantKey {
				t.Fatalf("key = %q, want %q", key, tt.wantKey)
			}
			if len(levels) != len(tt.wantLevels) {
				t.Fatalf("levels = %v, want %v", levels, tt.wantLevels)
			}
			for i := range levels {
				if levels[i] != tt.wantLevels[i] {
					t.Fatalf("levels = %v, want %v", levels, tt.wantLevels)
				}
			}
		})
	}
}

func TestParseRuleBackwardCompatible(t *testing.T) {
	key, need, ok := parseRule("distinct_styles>=5,15,30")
	if !ok || key != "distinct_styles" || need != 5 {
		t.Fatalf("parseRule = %q, %d, %v", key, need, ok)
	}
	if _, _, ok := parseRule("broken"); ok {
		t.Fatal("broken rule accepted")
	}
}

func TestLevelFor(t *testing.T) {
	thresholds := []int{5, 15, 30}
	cases := map[int]int{0: 0, 4: 0, 5: 1, 14: 1, 15: 2, 29: 2, 30: 3, 99: 3}
	for have, want := range cases {
		if got := levelFor(have, thresholds); got != want {
			t.Fatalf("levelFor(%d) = %d, want %d", have, got, want)
		}
	}
}

func TestEvaluateLevelUpgrade(t *testing.T) {
	ctx := context.Background()
	database := newTestDB(t)
	svc := New(database)

	u, err := database.Client.User.Create().
		SetHandle("tester").
		SetEmail("tester@example.com").
		Save(ctx)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	drink, err := database.Client.Drink.Create().
		SetName("Test Cola").
		SetSlug("test-cola").
		Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	if _, err := database.Client.Badge.Create().
		SetSlug("level-test").
		SetName("Level Test").
		SetRule("checkins>=1,3").
		Save(ctx); err != nil {
		t.Fatalf("badge: %v", err)
	}

	addCheckin := func() {
		if _, err := database.Client.Checkin.Create().
			SetUserID(u.ID).
			SetDrinkID(drink.ID).
			Save(ctx); err != nil {
			t.Fatalf("checkin: %v", err)
		}
	}

	addCheckin()
	awarded, err := svc.Evaluate(ctx, u.ID)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !contains(awarded, "level-test") {
		t.Fatalf("first award missing: %v", awarded)
	}
	assertLevel(t, database, ctx, u.ID, 1)

	addCheckin()
	addCheckin()
	awarded, err = svc.Evaluate(ctx, u.ID)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !contains(awarded, "level-test") {
		t.Fatalf("upgrade missing: %v", awarded)
	}
	assertLevel(t, database, ctx, u.ID, 2)

	// Re-evaluating at the same level is a no-op.
	awarded, err = svc.Evaluate(ctx, u.ID)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if contains(awarded, "level-test") {
		t.Fatalf("unchanged badge reported: %v", awarded)
	}

	notes, err := database.Client.Notification.Query().
		Where(notification.SubjectID("level-test")).
		Order(notification.ByCreatedAt()).
		All(ctx)
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("notifications = %d, want 2", len(notes))
	}
	if !strings.Contains(notes[0].Text, "· Level 1") {
		t.Fatalf("level-1 notification = %q", notes[0].Text)
	}
	if !strings.Contains(notes[1].Text, "· Level 2") {
		t.Fatalf("level-2 notification = %q", notes[1].Text)
	}
}

func assertLevel(t *testing.T, database *db.DB, ctx context.Context, userID string, want int) {
	t.Helper()
	ub, err := database.Client.UserBadge.Query().
		Where(userbadge.UserID(userID), userbadge.BadgeID(badgeID(t, database, "level-test"))).
		Only(ctx)
	if err != nil {
		t.Fatalf("user badge: %v", err)
	}
	if ub.Level != want {
		t.Fatalf("level = %d, want %d", ub.Level, want)
	}
}

func badgeID(t *testing.T, database *db.DB, slug string) int {
	t.Helper()
	b, err := database.Client.Badge.Query().Where(badge.SlugEQ(slug)).Only(context.Background())
	if err != nil {
		t.Fatalf("badge lookup: %v", err)
	}
	return b.ID
}

func TestNormalizeBadgeRuleThresholds(t *testing.T) {
	tests := []struct {
		name     string
		rule     string
		wantRule string
		wantErr  bool
	}{
		{name: "single threshold", rule: "checkins>=1", wantRule: "checkins>=1"},
		{name: "valid multi", rule: "distinct_styles>=5, 15,30", wantRule: "distinct_styles>=5,15,30"},
		{name: "valid two", rule: " friends >= 1 , 10 ", wantRule: "friends>=1,10"},
		{name: "unsorted rejected", rule: "checkins>=20,10", wantErr: true},
		{name: "duplicate thresholds rejected", rule: "checkins>=10,10", wantErr: true},
		{name: "too many thresholds rejected", rule: "checkins>=1,2,3,4", wantErr: true},
		{name: "non-numeric rejected", rule: "checkins>=abc", wantErr: true},
		{name: "partially non-numeric rejected", rule: "checkins>=1,2x", wantErr: true},
		{name: "empty key rejected", rule: ">=5", wantErr: true},
		{name: "empty threshold rejected", rule: "checkins>=", wantErr: true},
		{name: "zero threshold rejected", rule: "checkins>=0", wantErr: true},
		{name: "negative threshold rejected", rule: "checkins>=1,-2", wantErr: true},
		{name: "threshold too large rejected", rule: "checkins>=1,100001", wantErr: true},
		{name: "unknown counter rejected", rule: "unicorns>=1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validBadgeInput()
			in.Rule = tt.rule
			got, err := normalizeBadgeInput(in)
			if tt.wantErr {
				if !errors.Is(err, ErrBadgeInvalid) {
					t.Fatalf("normalizeBadgeInput(%q) err = %v, want ErrBadgeInvalid", tt.rule, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeBadgeInput(%q): %v", tt.rule, err)
			}
			if got.Rule != tt.wantRule {
				t.Fatalf("rule = %q, want %q", got.Rule, tt.wantRule)
			}
		})
	}
}
