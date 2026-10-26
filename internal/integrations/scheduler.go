package integrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	digestSchedulerName = "weekly_digest"
	digestLockTTL       = 10 * time.Minute
	digestTickInterval  = 10 * time.Minute
	digestCatchUpWindow = 7 * 24 * time.Hour

	// schedulerTimeFormat always includes nanoseconds so that lexical
	// comparison of the RFC 3339 timestamps stored in SQLite is correct.
	schedulerTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"
)

// digestScheduler runs the weekly digest on a fixed UTC Monday 09:00 schedule.
// It keeps its progress and lease in the scheduler_state table so that a
// restart neither loses nor duplicates a run and multiple replicas can share
// the same database safely.
type digestScheduler struct {
	svc  *Service
	now  func() time.Time
	tick time.Duration
	send func(context.Context) error
}

func newDigestScheduler(s *Service, now func() time.Time) *digestScheduler {
	if now == nil {
		now = time.Now
	}
	return &digestScheduler{
		svc:  s,
		now:  now,
		tick: digestTickInterval,
		send: s.WeeklyDigest,
	}
}

// RunDigestScheduler queues the weekly digest every Monday at 09:00 UTC until
// the context is cancelled. A missed run is replayed at most once (when the
// process restarts less than seven days late); a longer outage is baselined
// forward without a burst of catch-up digests.
func (s *Service) RunDigestScheduler(ctx context.Context) {
	newDigestScheduler(s, time.Now).loop(ctx)
}

// loop wakes immediately on startup (to reconcile state and catch up if
// needed) and then at most every tick interval.
func (d *digestScheduler) loop(ctx context.Context) {
	for {
		if err := d.runOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			d.svc.logger().Warn("weekly digest scheduler", "err", err)
		}
		delay := d.tick
		if delay <= 0 {
			delay = digestTickInterval
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// runOnce performs a single scheduling decision. It is safe to call from every
// replica on every tick; only the lease winner sends a digest.
func (d *digestScheduler) runOnce(ctx context.Context) error {
	if err := d.ensureState(ctx); err != nil {
		return err
	}
	now := d.now().UTC()
	last, err := d.lastRun(ctx)
	if err != nil {
		return err
	}
	if last == nil {
		// First ever run: record a baseline without sending. The first digest
		// is the next scheduled Monday.
		return d.setLastRun(ctx, now, now)
	}

	due := nextDigestTime(*last)
	if due.After(now) {
		return nil
	}
	if now.Sub(due) > digestCatchUpWindow {
		// Too far behind to replay safely. Baseline to the most recent
		// scheduled slot and resume from the next one.
		return d.setLastRun(ctx, previousDigestTime(now), now)
	}
	return d.runDigest(ctx, now)
}

// runDigest acquires the lease, sends the digest once and advances the
// baseline only when the send completed successfully.
func (d *digestScheduler) runDigest(ctx context.Context, now time.Time) error {
	owner := newLockOwner()
	acquired, err := d.acquireLock(ctx, owner, now)
	if err != nil {
		return err
	}
	if !acquired {
		// Another replica is on it. It will advance last_run_at itself.
		return nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := d.releaseLock(releaseCtx, owner, d.now().UTC()); err != nil {
			d.svc.logger().Warn("weekly digest lock release failed", "err", err)
		}
	}()

	if err := d.send(ctx); err != nil {
		return err
	}
	return d.setLastRun(ctx, now, now)
}

func (d *digestScheduler) ensureState(ctx context.Context) error {
	_, err := d.svc.SQL.ExecContext(ctx,
		`INSERT OR IGNORE INTO scheduler_state (name, lock_owner, updated_at) VALUES (?, '', ?)`,
		digestSchedulerName, schedulerTime(d.now().UTC()))
	return err
}

func (d *digestScheduler) lastRun(ctx context.Context) (*time.Time, error) {
	var raw sql.NullString
	err := d.svc.SQL.QueryRowContext(ctx,
		`SELECT last_run_at FROM scheduler_state WHERE name = ?`, digestSchedulerName).Scan(&raw)
	if err != nil {
		return nil, err
	}
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return nil, fmt.Errorf("parse weekly digest last_run_at: %w", err)
	}
	t = t.UTC()
	return &t, nil
}

func (d *digestScheduler) setLastRun(ctx context.Context, lastRun, now time.Time) error {
	_, err := d.svc.SQL.ExecContext(ctx,
		`UPDATE scheduler_state SET last_run_at = ?, updated_at = ? WHERE name = ?`,
		schedulerTime(lastRun), schedulerTime(now), digestSchedulerName)
	return err
}

func (d *digestScheduler) acquireLock(ctx context.Context, owner string, now time.Time) (bool, error) {
	res, err := d.svc.SQL.ExecContext(ctx, `
		UPDATE scheduler_state
		SET lock_owner = ?, locked_until = ?, updated_at = ?
		WHERE name = ? AND (locked_until IS NULL OR locked_until < ?)`,
		owner, schedulerTime(now.Add(digestLockTTL)), schedulerTime(now),
		digestSchedulerName, schedulerTime(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (d *digestScheduler) releaseLock(ctx context.Context, owner string, now time.Time) error {
	_, err := d.svc.SQL.ExecContext(ctx, `
		UPDATE scheduler_state
		SET locked_until = NULL, lock_owner = '', updated_at = ?
		WHERE name = ? AND lock_owner = ?`,
		schedulerTime(now), digestSchedulerName, owner)
	return err
}

func newLockOwner() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("owner-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func schedulerTime(t time.Time) string {
	return t.UTC().Format(schedulerTimeFormat)
}

func nextDigestTime(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC)
	for next.Weekday() != time.Monday || !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func previousDigestTime(now time.Time) time.Time {
	prev := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC)
	for prev.Weekday() != time.Monday || prev.After(now) {
		prev = prev.AddDate(0, 0, -1)
	}
	return prev
}
