package auth

import (
	"context"
	"testing"
)

func TestRevokeSessionScopedToOwner(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	owner := createUser(t, s, "sessionowner", "sessionowner@example.test", "originalpass")
	intruder := createUser(t, s, "intruder", "intruder@example.test", "originalpass")

	if _, err := s.CreateSession(ctx, owner.ID, "192.0.2.1", "owner-agent"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListSessions(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("sessions = %d, want 1", len(rows))
	}
	target := rows[0].ID

	if err := s.RevokeSession(ctx, intruder.ID, itoaTest(target)); err == nil {
		t.Fatal("another user's session should not be revocable")
	}
	if got := countSessions(t, s, owner.ID); got != 1 {
		t.Fatalf("sessions after foreign revoke = %d, want 1", got)
	}
	if err := s.RevokeSession(ctx, owner.ID, itoaTest(target)); err != nil {
		t.Fatalf("owner revoke: %v", err)
	}
	if got := countSessions(t, s, owner.ID); got != 0 {
		t.Fatalf("sessions after revoke = %d, want 0", got)
	}
	if err := s.RevokeSession(ctx, owner.ID, "not-a-number"); err == nil {
		t.Fatal("invalid session id should be rejected")
	}
}

func TestRevokeAllSessionsKeepsException(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "all", "all@example.test", "originalpass")

	if _, err := s.CreateSession(ctx, u.ID, "192.0.2.1", "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSession(ctx, u.ID, "192.0.2.2", "two"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSession(ctx, u.ID, "192.0.2.3", "three"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListSessions(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("sessions = %d, want 3", len(rows))
	}
	keep := rows[0].ID
	if err := s.RevokeAllSessions(ctx, u.ID, itoaTest(keep)); err != nil {
		t.Fatal(err)
	}
	remaining, err := s.ListSessions(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != keep {
		t.Fatalf("remaining = %v, want only %d", remaining, keep)
	}
	if err := s.RevokeAllSessions(ctx, u.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := countSessions(t, s, u.ID); got != 0 {
		t.Fatalf("sessions = %d, want 0", got)
	}
}
