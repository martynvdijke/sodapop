package auth

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent/drink"
)

func TestChangePasswordWrongCurrent(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "wrong", "wrong@example.test", "originalpass")

	err := s.ChangePassword(ctx, u.ID, "not-the-password", "brandnewpass", "")
	if err == nil {
		t.Fatal("wrong current password should be rejected")
	}
	refreshed, err := s.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.PasswordHash == nil || !CheckPassword(*refreshed.PasswordHash, "originalpass") {
		t.Fatal("password should not have changed")
	}
}

func TestChangePasswordWeakNext(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "weak", "weak@example.test", "originalpass")

	if err := s.ChangePassword(ctx, u.ID, "originalpass", "short", ""); err == nil {
		t.Fatal("short password should be rejected")
	}
}

func TestChangePasswordKeepsCurrentSession(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "keep", "keep@example.test", "originalpass")

	current, err := s.CreateSession(ctx, u.ID, "10.0.0.1", "current")
	if err != nil {
		t.Fatal(err)
	}
	_ = current
	sessions, err := s.ListSessions(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	currentID := sessions[0].ID
	if _, err := s.CreateSession(ctx, u.ID, "10.0.0.2", "other"); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangePassword(ctx, u.ID, "originalpass", "brandnewpass", strconv.Itoa(currentID)); err != nil {
		t.Fatalf("change password: %v", err)
	}
	remaining, err := s.ListSessions(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != currentID {
		t.Fatalf("remaining sessions = %v, want only current %d", remaining, currentID)
	}
}

func TestUpdateProfileValidation(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "validator", "validator@example.test", "originalpass")

	if err := s.UpdateProfile(ctx, u.ID, "   ", ""); err == nil {
		t.Fatal("empty display name should be rejected")
	}
	if err := s.UpdateProfile(ctx, u.ID, strings.Repeat("a", 61), ""); err == nil {
		t.Fatal("61-rune display name should be rejected")
	}
	if err := s.UpdateProfile(ctx, u.ID, "Fine name", strings.Repeat("b", 301)); err == nil {
		t.Fatal("301-rune bio should be rejected")
	}
	if err := s.UpdateProfile(ctx, u.ID, strings.Repeat("a", 60), strings.Repeat("b", 300)); err != nil {
		t.Fatalf("boundary values should be accepted: %v", err)
	}
	refreshed, err := s.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.DisplayName != strings.Repeat("a", 60) {
		t.Fatalf("display name = %q", refreshed.DisplayName)
	}
}

func TestSetAvatarRejectsForeignMedia(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, s, "owner", "owner@example.test", "originalpass")
	other := createUser(t, s, "other", "other@example.test", "originalpass")

	row, err := s.Client.Media.Create().
		SetUserID(owner.ID).
		SetKind("avatar").
		SetPath("2026/10/a.jpg").
		SetMime("image/jpeg").
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAvatar(ctx, other.ID, row.ID); err == nil {
		t.Fatal("another user's media should be rejected")
	}
	if err := s.SetAvatar(ctx, owner.ID, row.ID); err != nil {
		t.Fatalf("owner should be able to set avatar: %v", err)
	}
	refreshed, err := s.Client.User.Get(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AvatarMediaID == nil || *refreshed.AvatarMediaID != row.ID {
		t.Fatal("avatar was not stored")
	}
}

func TestSetFavouriteDrinkStatuses(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "favourite", "favourite@example.test", "originalpass")

	cases := []struct {
		name    string
		status  drink.Status
		wantErr error
	}{
		{"active", drink.StatusActive, nil},
		{"limited", drink.StatusLimited, nil},
		{"seasonal", drink.StatusSeasonal, nil},
		{"discontinued", drink.StatusDiscontinued, nil},
		{"merged", drink.StatusMerged, ErrDrinkNotFound},
		{"provisional", drink.StatusProvisional, ErrDrinkNotFound},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, err := s.Client.Drink.Create().
				SetName(tc.name).
				SetSlug("favourite-" + tc.name + "-" + strconv.Itoa(i)).
				SetStatus(tc.status).
				Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = s.SetFavouriteDrink(ctx, u.ID, row.ID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SetFavouriteDrink(%s) err = %v, want %v", tc.name, err, tc.wantErr)
			}
		})
	}
}

func TestFavouriteDrinkLifecycle(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "pin", "pin@example.test", "originalpass")

	if err := s.Client.User.UpdateOneID(u.ID).
		SetUpdatedAt("2000-01-01T00:00:00Z").
		Exec(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := s.Client.Drink.Create().
		SetName("Cola").
		SetSlug("favourite-cola").
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetFavouriteDrink(ctx, u.ID, row.ID); err != nil {
		t.Fatalf("set favourite drink: %v", err)
	}
	refreshed, err := s.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.FavouriteDrinkID == nil || *refreshed.FavouriteDrinkID != row.ID {
		t.Fatalf("favourite drink id = %v, want %d", refreshed.FavouriteDrinkID, row.ID)
	}
	if refreshed.UpdatedAt == "2000-01-01T00:00:00Z" {
		t.Fatal("updated_at should be touched when the favourite drink is set")
	}
	got, err := s.FavouriteDrinkID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != row.ID {
		t.Fatalf("FavouriteDrinkID = %v, want %d", got, row.ID)
	}

	// A missing drink must not overwrite the existing pin.
	if err := s.SetFavouriteDrink(ctx, u.ID, row.ID+9999); !errors.Is(err, ErrDrinkNotFound) {
		t.Fatalf("missing drink err = %v, want ErrDrinkNotFound", err)
	}
	if got, err := s.FavouriteDrinkID(ctx, u.ID); err != nil || got == nil || *got != row.ID {
		t.Fatalf("favourite drink after failed set = %v (%v), want %d", got, err, row.ID)
	}

	if err := s.Client.User.UpdateOneID(u.ID).
		SetUpdatedAt("2001-01-01T00:00:00Z").
		Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearFavouriteDrink(ctx, u.ID); err != nil {
		t.Fatalf("clear favourite drink: %v", err)
	}
	cleared, err := s.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.FavouriteDrinkID != nil {
		t.Fatalf("favourite drink id = %v, want nil", cleared.FavouriteDrinkID)
	}
	if cleared.UpdatedAt == "2001-01-01T00:00:00Z" {
		t.Fatal("updated_at should be touched when the favourite drink is cleared")
	}
}
