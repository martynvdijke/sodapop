package auth

import (
	"context"
	"testing"

	"sodapop/ent/usersetting"
)

func TestGetOrCreateSettingsIsIdempotent(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "prefs", "prefs@example.test", "originalpass")

	first, err := s.GetOrCreateSettings(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.GetOrCreateSettings(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("settings ids differ: %d vs %d", first.ID, second.ID)
	}
	n, err := s.Client.UserSetting.Query().Where(usersetting.UserID(u.ID)).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("settings rows = %d, want 1", n)
	}
	if second.Theme != "system" || second.Units != "metric" {
		t.Fatalf("defaults = %q/%q, want system/metric", second.Theme, second.Units)
	}
}
