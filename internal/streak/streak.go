// Package streak computes check-in day streaks for a user.
//
// Only UTC calendar days matter: every distinct date(created_at) of a
// non-deleted check-in is one day in the run, no matter how many check-ins
// happened that day. The queries are parameterized and all arithmetic happens
// in Go so the behaviour is identical across SQLite versions.
package streak

import (
	"context"
	"database/sql"
	"time"
)

const dateLayout = "2006-01-02"

// Current reports the user's active streak in days. A run that ends today
// counts, and a run that ended yesterday still counts until a full day has
// been missed. A user with no check-in yesterday or today has a streak of 0.
func Current(ctx context.Context, sqlDB *sql.DB, userID string, now time.Time) (int, error) {
	days, err := checkinDays(ctx, sqlDB, userID)
	if err != nil {
		return 0, err
	}
	if len(days) == 0 {
		return 0, nil
	}

	today := utcDate(now)
	start := today
	if !days[today] {
		start = today.AddDate(0, 0, -1)
		if !days[start] {
			return 0, nil
		}
	}

	streak := 0
	for day := start; days[day]; day = day.AddDate(0, 0, -1) {
		streak++
	}
	return streak, nil
}

// Longest reports the longest run of consecutive check-in days the user has
// ever had.
func Longest(ctx context.Context, sqlDB *sql.DB, userID string) (int, error) {
	days, err := checkinDays(ctx, sqlDB, userID)
	if err != nil {
		return 0, err
	}
	if len(days) == 0 {
		return 0, nil
	}

	ordered := make([]time.Time, 0, len(days))
	for day := range days {
		ordered = append(ordered, day)
	}
	// Insertion-sort is plenty for a personal check-in history and keeps the
	// dependency surface small.
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].Before(ordered[j-1]); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}

	best, run := 1, 1
	for i := 1; i < len(ordered); i++ {
		if ordered[i].Equal(ordered[i-1].AddDate(0, 0, 1)) {
			run++
		} else {
			run = 1
		}
		if run > best {
			best = run
		}
	}
	return best, nil
}

// checkinDays returns the set of distinct UTC dates with a check-in.
func checkinDays(ctx context.Context, sqlDB *sql.DB, userID string) (map[time.Time]bool, error) {
	rows, err := sqlDB.QueryContext(ctx, `
		SELECT DISTINCT date(created_at) FROM checkins
		WHERE user_id = ? AND deleted_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	days := make(map[time.Time]bool)
	for rows.Next() {
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if !raw.Valid {
			continue
		}
		day, err := time.Parse(dateLayout, raw.String)
		if err != nil {
			// A malformed timestamp should not break streaks entirely.
			continue
		}
		days[day] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return days, nil
}

func utcDate(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
