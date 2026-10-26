package auth

import (
	"context"
	"testing"
	"time"

	"sodapop/ent/loginattempt"
)

func TestLoginLockoutThresholdAndClear(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	email := "Locked@Example.com"

	for i := 0; i < LoginFailureLimit-1; i++ {
		if err := svc.RecordLoginFailure(ctx, email); err != nil {
			t.Fatalf("failure %d: %v", i, err)
		}
	}
	if svc.LoginLocked(ctx, "locked@example.com") {
		t.Fatal("locked before the tenth failure")
	}
	if err := svc.RecordLoginFailure(ctx, "locked@example.com"); err != nil {
		t.Fatalf("tenth failure: %v", err)
	}
	if !svc.LoginLocked(ctx, email) {
		t.Fatal("not locked after the tenth failure")
	}
	if svc.LoginLocked(ctx, "someone@example.com") {
		t.Fatal("a different key must not be locked")
	}

	if err := svc.ClearLoginFailures(ctx, email); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if svc.LoginLocked(ctx, email) {
		t.Fatal("still locked after a successful login cleared attempts")
	}
}

func TestPasswordAttemptKeysAreNormalizedAndHashed(t *testing.T) {
	if LoginKey(" User@Example.com ") != LoginKey("user@example.com") {
		t.Fatal("login keys differ after normalization")
	}
	if key := LoginKey("user@example.com"); key == "user@example.com" || len(key) < 32 {
		t.Fatalf("login key is not opaque: %q", key)
	}
}

func TestLoginAttemptsPruned(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	if _, err := database.Client.LoginAttempt.Create().
		SetKey("email:stale").SetAttemptedAt(old).Save(ctx); err != nil {
		t.Fatalf("stale attempt: %v", err)
	}

	if err := svc.RecordLoginFailure(ctx, "fresh@example.com"); err != nil {
		t.Fatalf("record: %v", err)
	}
	n, err := database.Client.LoginAttempt.Query().
		Where(loginattempt.KeyEQ("email:stale")).Count(ctx)
	if err != nil {
		t.Fatalf("count stale: %v", err)
	}
	if n != 0 {
		t.Fatalf("stale rows remaining = %d, want 0", n)
	}
}

func TestEmptyIdentifierDoesNotWrite(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	if err := svc.RecordLoginFailure(ctx, "  "); err != nil {
		t.Fatalf("record empty: %v", err)
	}
	if svc.LoginLocked(ctx, "") {
		t.Fatal("empty identifier must not be locked")
	}
	n, err := database.Client.LoginAttempt.Query().Count(ctx)
	if err != nil || n != 0 {
		t.Fatalf("attempts = %d, %v; want 0", n, err)
	}
}
