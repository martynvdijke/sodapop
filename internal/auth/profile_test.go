package auth

import (
	"context"
	"strconv"
	"strings"
	"testing"
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
