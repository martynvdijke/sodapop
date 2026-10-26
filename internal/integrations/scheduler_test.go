package integrations

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type digestTestClock struct {
	now time.Time
}

func (c *digestTestClock) Now() time.Time { return c.now }

type digestStateRow struct {
	lastRun     sql.NullString
	lockedUntil sql.NullString
	lockOwner   string
}

func readDigestState(t *testing.T, svc *Service) digestStateRow {
	t.Helper()
	var row digestStateRow
	if err := svc.SQL.QueryRow(
		`SELECT last_run_at, locked_until, lock_owner FROM scheduler_state WHERE name = ?`,
		digestSchedulerName).Scan(&row.lastRun, &row.lockedUntil, &row.lockOwner); err != nil {
		t.Fatalf("read scheduler state: %v", err)
	}
	return row
}

func seedDigestState(t *testing.T, svc *Service, lastRun time.Time, lockedUntil *time.Time, owner string) {
	t.Helper()
	var locked any
	if lockedUntil != nil {
		locked = schedulerTime(*lockedUntil)
	}
	if _, err := svc.SQL.Exec(
		`INSERT OR REPLACE INTO scheduler_state (name, last_run_at, locked_until, lock_owner, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		digestSchedulerName, schedulerTime(lastRun), locked, owner, schedulerTime(lastRun)); err != nil {
		t.Fatalf("seed scheduler state: %v", err)
	}
}

func requireSchedulerTime(t *testing.T, raw sql.NullString, want time.Time) {
	t.Helper()
	if !raw.Valid {
		t.Fatalf("timestamp is NULL, want %s", want.Format(time.RFC3339))
	}
	got, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		t.Fatalf("parse timestamp %q: %v", raw.String, err)
	}
	if !got.Equal(want) {
		t.Fatalf("timestamp = %s, want %s", got, want)
	}
}

func TestNextDigestTime(t *testing.T) {
	monday := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "friday",
			now:  time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
			want: monday,
		},
		{
			name: "sunday",
			now:  time.Date(2026, 10, 11, 23, 0, 0, 0, time.UTC),
			want: monday,
		},
		{
			name: "monday before nine",
			now:  time.Date(2026, 10, 12, 8, 59, 0, 0, time.UTC),
			want: monday,
		},
		{
			name: "monday exactly nine",
			now:  monday,
			want: monday.AddDate(0, 0, 7),
		},
		{
			name: "monday after nine",
			now:  time.Date(2026, 10, 12, 9, 1, 0, 0, time.UTC),
			want: monday.AddDate(0, 0, 7),
		},
		{
			name: "tuesday",
			now:  time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
			want: monday.AddDate(0, 0, 7),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextDigestTime(tc.now); !got.Equal(tc.want) {
				t.Fatalf("nextDigestTime(%s) = %s, want %s", tc.now, got, tc.want)
			}
		})
	}
}

func TestPreviousDigestTime(t *testing.T) {
	monday := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "tuesday",
			now:  time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
			want: monday,
		},
		{
			name: "monday before nine",
			now:  time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC),
			want: monday.AddDate(0, 0, -7),
		},
		{
			name: "monday exactly nine",
			now:  monday,
			want: monday,
		},
		{
			name: "monday after nine",
			now:  monday.Add(500 * time.Millisecond),
			want: monday,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := previousDigestTime(tc.now); !got.Equal(tc.want) {
				t.Fatalf("previousDigestTime(%s) = %s, want %s", tc.now, got, tc.want)
			}
		})
	}
}

func TestDigestSchedulerFirstRunBaselinesWithoutSending(t *testing.T) {
	svc, _, ctx := newTestService(t)
	clock := &digestTestClock{now: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)} // Wednesday
	sched := newDigestScheduler(svc, clock.Now)
	var sends int32
	sched.send = func(context.Context) error {
		atomic.AddInt32(&sends, 1)
		return nil
	}

	if err := sched.runOnce(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := atomic.LoadInt32(&sends); got != 0 {
		t.Fatalf("first run sent %d digests, want 0", got)
	}
	state := readDigestState(t, svc)
	requireSchedulerTime(t, state.lastRun, clock.now)
	if state.lockedUntil.Valid || state.lockOwner != "" {
		t.Fatalf("lock not clear after baseline: %+v", state)
	}

	// Repeated ticks before the next Monday must not send either.
	for i := 0; i < 3; i++ {
		if err := sched.runOnce(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&sends); got != 0 {
		t.Fatalf("ticks sent %d digests, want 0", got)
	}

	// The next Monday 09:00 UTC is the first scheduled digest.
	clock.now = time.Date(2026, 10, 19, 9, 0, 5, 0, time.UTC)
	if err := sched.runOnce(ctx); err != nil {
		t.Fatalf("scheduled run: %v", err)
	}
	if got := atomic.LoadInt32(&sends); got != 1 {
		t.Fatalf("scheduled run sent %d digests, want 1", got)
	}
	requireSchedulerTime(t, readDigestState(t, svc).lastRun, clock.now)
}

func TestDigestSchedulerTuesdayCatchUpOnce(t *testing.T) {
	svc, _, ctx := newTestService(t)
	lastMonday := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	seedDigestState(t, svc, lastMonday, nil, "")

	clock := &digestTestClock{now: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)} // Tuesday, ~1 day late
	sched := newDigestScheduler(svc, clock.Now)
	var sends int32
	sched.send = func(context.Context) error {
		atomic.AddInt32(&sends, 1)
		return nil
	}

	for i := 0; i < 4; i++ {
		if err := sched.runOnce(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&sends); got != 1 {
		t.Fatalf("sent %d digests, want exactly 1 catch-up", got)
	}
	requireSchedulerTime(t, readDigestState(t, svc).lastRun, clock.now)
}

func TestDigestSchedulerNoCatchUpWhenMoreThanSevenDaysLate(t *testing.T) {
	svc, _, ctx := newTestService(t)
	lastMonday := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	seedDigestState(t, svc, lastMonday, nil, "")

	clock := &digestTestClock{now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)} // Wednesday, 9 days after the missed slot
	sched := newDigestScheduler(svc, clock.Now)
	var sends int32
	sched.send = func(context.Context) error {
		atomic.AddInt32(&sends, 1)
		return nil
	}

	if err := sched.runOnce(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := atomic.LoadInt32(&sends); got != 0 {
		t.Fatalf("sent %d digests, want none after a long outage", got)
	}
	// Baseline moves to the most recent scheduled slot (Monday 19 Oct).
	requireSchedulerTime(t, readDigestState(t, svc).lastRun,
		time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC))

	// Resumes normally on the following Monday.
	clock.now = time.Date(2026, 10, 26, 9, 0, 1, 0, time.UTC)
	if err := sched.runOnce(ctx); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := atomic.LoadInt32(&sends); got != 1 {
		t.Fatalf("resumed run sent %d digests, want 1", got)
	}
}

func TestDigestSchedulerLockContentionOnlyOneSender(t *testing.T) {
	svc, _, ctx := newTestService(t)
	seedDigestState(t, svc, time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC), nil, "")

	clock := &digestTestClock{now: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}
	first := newDigestScheduler(svc, clock.Now)
	second := newDigestScheduler(svc, clock.Now)

	started := make(chan struct{})
	release := make(chan struct{})
	var firstSends, secondSends int32
	first.send = func(context.Context) error {
		close(started)
		<-release
		atomic.AddInt32(&firstSends, 1)
		return nil
	}
	second.send = func(context.Context) error {
		atomic.AddInt32(&secondSends, 1)
		return nil
	}

	firstErr := make(chan error, 1)
	go func() { firstErr <- first.runOnce(ctx) }()
	<-started // the first replica now owns the lease

	if err := second.runOnce(ctx); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := atomic.LoadInt32(&secondSends); got != 0 {
		t.Fatalf("second replica sent %d digests while lease held, want 0", got)
	}

	close(release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := atomic.LoadInt32(&firstSends); got != 1 {
		t.Fatalf("first replica sent %d digests, want 1", got)
	}
	requireSchedulerTime(t, readDigestState(t, svc).lastRun, clock.now)
}

func TestDigestSchedulerStaleLockTakeover(t *testing.T) {
	svc, _, ctx := newTestService(t)
	lastMonday := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	clock := &digestTestClock{now: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}
	stale := clock.now.Add(-time.Minute)
	seedDigestState(t, svc, lastMonday, &stale, "crashed-replica")

	sched := newDigestScheduler(svc, clock.Now)
	var sends int32
	sched.send = func(context.Context) error {
		atomic.AddInt32(&sends, 1)
		return nil
	}

	if err := sched.runOnce(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := atomic.LoadInt32(&sends); got != 1 {
		t.Fatalf("sent %d digests after stale lease, want 1", got)
	}
	state := readDigestState(t, svc)
	requireSchedulerTime(t, state.lastRun, clock.now)
	if state.lockedUntil.Valid || state.lockOwner != "" {
		t.Fatalf("lock not released: %+v", state)
	}
}

func TestDigestSchedulerFailedDigestReleasesLockWithoutAdvancing(t *testing.T) {
	svc, _, ctx := newTestService(t)
	lastMonday := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	seedDigestState(t, svc, lastMonday, nil, "")

	clock := &digestTestClock{now: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}
	sched := newDigestScheduler(svc, clock.Now)
	var attempts int32
	sched.send = func(context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return errors.New("smtp unavailable")
	}

	if err := sched.runOnce(ctx); err == nil {
		t.Fatal("run error = nil, want failure")
	}
	state := readDigestState(t, svc)
	requireSchedulerTime(t, state.lastRun, lastMonday)
	if state.lockedUntil.Valid || state.lockOwner != "" {
		t.Fatalf("lock not released after failure: %+v", state)
	}

	// A later tick retries and succeeds, advancing the baseline once.
	sched.send = func(context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return nil
	}
	if err := sched.runOnce(ctx); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
	requireSchedulerTime(t, readDigestState(t, svc).lastRun, clock.now)
}

func TestDigestSchedulerNoDuplicateSendsAcrossRepeatedTicks(t *testing.T) {
	svc, _, ctx := newTestService(t)
	seedDigestState(t, svc, time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC), nil, "")
	clock := &digestTestClock{now: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}

	sched := newDigestScheduler(svc, clock.Now)
	var (
		mu    sync.Mutex
		sends int
	)
	sched.send = func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		sends++
		return nil
	}

	for i := 0; i < 10; i++ {
		if err := sched.runOnce(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if sends != 1 {
		t.Fatalf("sent %d digests across repeated ticks, want 1", sends)
	}
}
