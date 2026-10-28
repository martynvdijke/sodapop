package integrations

import (
	"testing"
	"time"
)

func TestDigestStatusCountsRecipientsAndPending(t *testing.T) {
	svc, database, ctx := newTestService(t)

	active := createUser(t, database, "active")
	if _, err := database.Client.UserSetting.Create().
		SetUserID(active.ID).
		SetEmailWeekly(true).
		Save(ctx); err != nil {
		t.Fatalf("active settings: %v", err)
	}

	optedOut := createUser(t, database, "optedout")
	if _, err := database.Client.UserSetting.Create().
		SetUserID(optedOut.ID).
		SetEmailWeekly(false).
		Save(ctx); err != nil {
		t.Fatalf("opted-out settings: %v", err)
	}

	deleted := createUser(t, database, "deleted")
	gone := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Client.User.UpdateOneID(deleted.ID).
		SetNillableDeletedAt(&gone).
		Save(ctx); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := database.Client.UserSetting.Create().
		SetUserID(deleted.ID).
		SetEmailWeekly(true).
		Save(ctx); err != nil {
		t.Fatalf("deleted settings: %v", err)
	}

	now := time.Now().UTC()
	if _, err := database.Client.EmailOutbox.Create().
		SetToEmail("active@example.com").
		SetTemplate("weekly_digest").
		SetPayloadJSON("{}").
		SetNextAttemptAt(now.Format(time.RFC3339)).
		Save(ctx); err != nil {
		t.Fatalf("queue outbox: %v", err)
	}
	if _, err := database.Client.EmailOutbox.Create().
		SetToEmail("active@example.com").
		SetTemplate("weekly_digest").
		SetPayloadJSON("{}").
		SetNextAttemptAt(now.Format(time.RFC3339)).
		SetSentAt(now.Format(time.RFC3339)).
		Save(ctx); err != nil {
		t.Fatalf("queue sent outbox: %v", err)
	}
	if _, err := database.Client.EmailOutbox.Create().
		SetToEmail("active@example.com").
		SetTemplate("password_reset").
		SetPayloadJSON("{}").
		SetNextAttemptAt(now.Format(time.RFC3339)).
		Save(ctx); err != nil {
		t.Fatalf("queue other outbox: %v", err)
	}

	lastRun := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	if _, err := svc.SQL.ExecContext(ctx, `
		INSERT INTO scheduler_state (name, last_run_at, locked_until, lock_owner, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		digestSchedulerName, schedulerTime(lastRun), schedulerTime(lastRun.Add(10*time.Minute)),
		"owner-1", schedulerTime(lastRun)); err != nil {
		t.Fatalf("seed scheduler state: %v", err)
	}

	status, err := svc.DigestStatus(ctx)
	if err != nil {
		t.Fatalf("DigestStatus: %v", err)
	}
	if status.Recipients != 1 {
		t.Fatalf("Recipients = %d, want 1", status.Recipients)
	}
	if status.Pending != 1 {
		t.Fatalf("Pending = %d, want 1", status.Pending)
	}
	if status.LastRunAt != schedulerTime(lastRun) {
		t.Fatalf("LastRunAt = %q, want %q", status.LastRunAt, schedulerTime(lastRun))
	}
	if status.LockedUntil != schedulerTime(lastRun.Add(10*time.Minute)) {
		t.Fatalf("LockedUntil = %q", status.LockedUntil)
	}
	if status.LockOwner != "owner-1" {
		t.Fatalf("LockOwner = %q, want owner-1", status.LockOwner)
	}
}

func TestDigestStatusWithoutSchedulerRow(t *testing.T) {
	svc, _, ctx := newTestService(t)

	status, err := svc.DigestStatus(ctx)
	if err != nil {
		t.Fatalf("DigestStatus: %v", err)
	}
	if status.LastRunAt != "" || status.LockedUntil != "" || status.LockOwner != "" {
		t.Fatalf("empty status = %+v", status)
	}
	if status.Recipients != 0 || status.Pending != 0 {
		t.Fatalf("counts = %+v, want zero", status)
	}
}

func TestNextDigestTimeExported(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	want := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	if got := NextDigestTime(now); !got.Equal(want) {
		t.Fatalf("NextDigestTime = %s, want %s", got, want)
	}
}
