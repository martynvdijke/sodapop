package integrations

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DigestStatus is the read-only view of the weekly digest scheduler used by the
// admin page. Timestamps are the raw RFC 3339 values stored in scheduler_state;
// an empty string means the scheduler has not recorded one yet.
type DigestStatus struct {
	LastRunAt   string
	LockedUntil string
	LockOwner   string
	Recipients  int
	Pending     int
}

// DigestStatus reads the digest scheduler row and counts opted-in recipients and
// queued weekly digest emails.
func (s *Service) DigestStatus(ctx context.Context) (DigestStatus, error) {
	var (
		status DigestStatus
		last   sql.NullString
		locked sql.NullString
	)
	err := s.SQL.QueryRowContext(ctx, `
		SELECT last_run_at, locked_until, lock_owner
		FROM scheduler_state
		WHERE name = ?`, digestSchedulerName).
		Scan(&last, &locked, &status.LockOwner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return DigestStatus{}, err
	}
	if last.Valid {
		status.LastRunAt = last.String
	}
	if locked.Valid {
		status.LockedUntil = locked.String
	}

	if err := s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM users u
		JOIN user_settings st ON st.user_id = u.id
		WHERE st.email_weekly = 1
		  AND u.deleted_at IS NULL
		  AND u.status = 'active'`).Scan(&status.Recipients); err != nil {
		return DigestStatus{}, err
	}

	if err := s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM email_outboxes
		WHERE template = 'weekly_digest' AND sent_at IS NULL`).Scan(&status.Pending); err != nil {
		return DigestStatus{}, err
	}

	return status, nil
}

// NextDigestTime returns the next scheduled digest slot (Monday 09:00 UTC).
func NextDigestTime(now time.Time) time.Time {
	return nextDigestTime(now)
}
