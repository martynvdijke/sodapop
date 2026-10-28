package checkins_test

import (
	"testing"
)

// TestStatsIncludesNonNegativeStreaks guards the streak fields on the personal
// stats payload. A check-in today also makes the current streak meaningful.
func TestStatsIncludesNonNegativeStreaks(t *testing.T) {
	e := newTestEnv(t)
	e.createCheckin(t, e.alice, "public")

	stats, err := e.svc.Stats(e.ctx, e.alice.ID)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.CurrentStreak < 0 || stats.LongestStreak < 0 {
		t.Fatalf("streaks must be non-negative: current=%d longest=%d", stats.CurrentStreak, stats.LongestStreak)
	}
	if stats.CurrentStreak == 0 {
		t.Fatalf("current streak = 0 with a check-in today")
	}
	if stats.LongestStreak < stats.CurrentStreak {
		t.Fatalf("longest streak %d < current streak %d", stats.LongestStreak, stats.CurrentStreak)
	}
}
