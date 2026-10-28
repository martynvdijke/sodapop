package badges

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sodapop/internal/db"
)

func validBadgeInput() BadgeInput {
	return BadgeInput{
		Slug:        "admin-test-pour",
		Name:        "First pour",
		Description: "Log your first soda.",
		Kind:        "core",
		Rule:        "checkins>=1",
	}
}

func TestCreateBadgeValidation(t *testing.T) {
	ctx := context.Background()
	svc := New(newTestDB(t))

	tests := []struct {
		name   string
		mutate func(*BadgeInput)
	}{
		{"slug too short", func(in *BadgeInput) { in.Slug = "ab" }},
		{"slug uppercase", func(in *BadgeInput) { in.Slug = "First-Pour" }},
		{"slug bad character", func(in *BadgeInput) { in.Slug = "first_pour" }},
		{"name empty", func(in *BadgeInput) { in.Name = "  " }},
		{"name too long", func(in *BadgeInput) { in.Name = strings.Repeat("x", 81) }},
		{"description too long", func(in *BadgeInput) { in.Description = strings.Repeat("x", 241) }},
		{"kind unknown", func(in *BadgeInput) { in.Kind = "global" }},
		{"rule unknown key", func(in *BadgeInput) { in.Rule = "unicorns>=1" }},
		{"rule missing operator", func(in *BadgeInput) { in.Rule = "checkins 1" }},
		{"rule zero threshold", func(in *BadgeInput) { in.Rule = "checkins>=0" }},
		{"rule too large", func(in *BadgeInput) { in.Rule = "checkins>=100001" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := validBadgeInput()
			tc.mutate(&in)
			if _, err := svc.CreateBadge(ctx, in); !errors.Is(err, ErrBadgeInvalid) {
				t.Fatalf("CreateBadge(%+v) err = %v, want ErrBadgeInvalid", in, err)
			}
		})
	}
}

func TestCreateBadgeAndAdminList(t *testing.T) {
	ctx := context.Background()
	svc := New(newTestDB(t))

	created, err := svc.CreateBadge(ctx, validBadgeInput())
	if err != nil {
		t.Fatalf("CreateBadge: %v", err)
	}
	if created.Slug != "admin-test-pour" || created.Kind != "core" || created.Rule != "checkins>=1" {
		t.Fatalf("created badge = %+v", created)
	}

	rows, err := svc.AdminList(ctx)
	if err != nil {
		t.Fatalf("AdminList: %v", err)
	}
	var found *AdminBadge
	for i := range rows {
		if rows[i].Slug == "admin-test-pour" {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("AdminList = %+v, want admin-test-pour", rows)
	}
	if found.Awarded != 0 || !found.CanDelete {
		t.Fatalf("new badge = %+v, want 0 awards and deletable", *found)
	}
}

func TestCreateBadgeDuplicateSlug(t *testing.T) {
	ctx := context.Background()
	svc := New(newTestDB(t))

	if _, err := svc.CreateBadge(ctx, validBadgeInput()); err != nil {
		t.Fatalf("CreateBadge: %v", err)
	}
	if _, err := svc.CreateBadge(ctx, validBadgeInput()); !errors.Is(err, ErrBadgeInvalid) {
		t.Fatalf("duplicate CreateBadge err = %v, want ErrBadgeInvalid", err)
	}
}

func TestAdminListCountsAwardsAndBlocksDelete(t *testing.T) {
	ctx := context.Background()
	database := newTestDB(t)
	svc := New(database)

	created, err := svc.CreateBadge(ctx, validBadgeInput())
	if err != nil {
		t.Fatalf("CreateBadge: %v", err)
	}
	user, err := database.Client.User.Create().
		SetHandle("awarded").
		SetEmail("awarded@example.com").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := database.Client.UserBadge.Create().
		SetUserID(user.ID).
		SetBadgeID(created.ID).
		SetLevel(1).
		SetAwardedAt(db.Now()).
		Save(ctx); err != nil {
		t.Fatalf("create user badge: %v", err)
	}

	rows, err := svc.AdminList(ctx)
	if err != nil {
		t.Fatalf("AdminList: %v", err)
	}
	var found *AdminBadge
	for i := range rows {
		if rows[i].Slug == "admin-test-pour" {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("AdminList = %+v, want admin-test-pour", rows)
	}
	if found.Awarded != 1 || found.CanDelete {
		t.Fatalf("AdminList row = %+v, want 1 award and CanDelete false", *found)
	}
	if err := svc.DeleteBadge(ctx, created.ID); !errors.Is(err, ErrBadgeAwarded) {
		t.Fatalf("DeleteBadge awarded err = %v, want ErrBadgeAwarded", err)
	}
}

func TestDeleteBadgeUnawardedAndNotFound(t *testing.T) {
	ctx := context.Background()
	database := newTestDB(t)
	svc := New(database)

	created, err := svc.CreateBadge(ctx, validBadgeInput())
	if err != nil {
		t.Fatalf("CreateBadge: %v", err)
	}
	if err := svc.DeleteBadge(ctx, created.ID); err != nil {
		t.Fatalf("DeleteBadge: %v", err)
	}
	if err := svc.DeleteBadge(ctx, created.ID); !errors.Is(err, ErrBadgeNotFound) {
		t.Fatalf("second DeleteBadge err = %v, want ErrBadgeNotFound", err)
	}
}

func TestUpdateBadgeKeepsSlug(t *testing.T) {
	ctx := context.Background()
	database := newTestDB(t)
	svc := New(database)

	created, err := svc.CreateBadge(ctx, validBadgeInput())
	if err != nil {
		t.Fatalf("CreateBadge: %v", err)
	}
	in := validBadgeInput()
	in.Slug = "renamed-slug"
	in.Name = "Renamed"
	in.Rule = "cans>=3"
	if err := svc.UpdateBadge(ctx, created.ID, in); err != nil {
		t.Fatalf("UpdateBadge: %v", err)
	}
	got, err := database.Client.Badge.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get badge: %v", err)
	}
	if got.Slug != "admin-test-pour" {
		t.Fatalf("slug = %q, want original admin-test-pour", got.Slug)
	}
	if got.Name != "Renamed" || got.Rule != "cans>=3" {
		t.Fatalf("updated badge = %+v", got)
	}
	if err := svc.UpdateBadge(ctx, 99999, in); !errors.Is(err, ErrBadgeNotFound) {
		t.Fatalf("UpdateBadge missing err = %v, want ErrBadgeNotFound", err)
	}
}
