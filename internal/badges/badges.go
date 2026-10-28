// Package badges evaluates and lists achievement badges for a user.
//
// Badge definitions live in the badges table. A rule is a small, declarative
// string such as "checkins>=10" or a multi-level "distinct_styles>=5,15,30".
// Evaluation is idempotent: awards are upserted against the unique
// user_id + badge_id index, so concurrent evaluations cannot double-award or
// downgrade a level.
package badges

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/activity"
	"sodapop/ent/badge"
	"sodapop/ent/notification"
	"sodapop/internal/db"
	"sodapop/internal/streak"
)

// Service evaluates badge rules and reports earned state.
type Service struct {
	Client *ent.Client
	SQL    *sql.DB
}

// New builds a badge service from an initialized database handle.
func New(database *db.DB) *Service {
	return &Service{Client: database.Client, SQL: database.SQL}
}

// Status is one badge plus the caller's earned state.
type Status struct {
	Slug        string
	Name        string
	Description string
	Kind        string
	Rule        string
	Earned      bool
	Level       int
	AwardedAt   string
}

// ListForUser returns every badge in slug order with earned state attached.
func (s *Service) ListForUser(ctx context.Context, userID string) ([]Status, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT b.slug, b.name, b.description, b.kind, b.rule,
		       ub.level, ub.awarded_at
		FROM badges b
		LEFT JOIN user_badges ub ON ub.badge_id = b.id AND ub.user_id = ?
		ORDER BY b.slug`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Status, 0, 16)
	for rows.Next() {
		var (
			st      Status
			level   sql.NullInt64
			awarded sql.NullString
		)
		if err := rows.Scan(&st.Slug, &st.Name, &st.Description, &st.Kind, &st.Rule, &level, &awarded); err != nil {
			return nil, err
		}
		if level.Valid {
			st.Earned = true
			st.Level = int(level.Int64)
		}
		if awarded.Valid {
			st.AwardedAt = awarded.String
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Evaluate checks every seeded badge rule and returns the slugs that were
// newly awarded or upgraded by this call. Unknown or malformed rules are
// ignored.
func (s *Service) Evaluate(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	progress, err := s.Progress(ctx, userID)
	if err != nil {
		return nil, err
	}
	defs, err := s.Client.Badge.Query().Order(badge.BySlug()).All(ctx)
	if err != nil {
		return nil, err
	}

	var awarded []string
	for _, def := range defs {
		key, thresholds, ok := parseThresholds(def.Rule)
		if !ok {
			continue
		}
		have, known := progress[key]
		if !known {
			continue
		}

		level := levelFor(have, thresholds)
		if level == 0 {
			continue
		}

		res, err := s.SQL.ExecContext(ctx, `
			INSERT INTO user_badges (user_id, badge_id, level, awarded_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, badge_id) DO UPDATE SET
				level = excluded.level,
				awarded_at = excluded.awarded_at
			WHERE excluded.level > user_badges.level`, userID, def.ID, level, db.Now())
		if err != nil {
			return awarded, err
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return awarded, err
		}
		if changed == 0 {
			continue // already at this level, possibly by a concurrent evaluation
		}

		awarded = append(awarded, def.Slug)
		text := "Badge unlocked: " + def.Name
		if len(thresholds) > 1 {
			text += " · Level " + strconv.Itoa(level)
		}
		// Secondary rows are best-effort: the award itself is authoritative.
		_, _ = s.Client.Notification.Create().
			SetUserID(userID).
			SetKind(notification.KindBadge).
			SetSubjectID(def.Slug).
			SetText(text).
			Save(ctx)
		// Activity is feed fan-out; ignore failures if the schema differs.
		_, _ = s.Client.Activity.Create().
			SetUserID(userID).
			SetKind(activity.KindBadge).
			SetSubjectID(def.Slug).
			SetVisibility(activity.VisibilityPublic).
			Save(ctx)
	}
	return awarded, nil
}

// levelFor counts how many of the ascending thresholds the counter has reached.
func levelFor(have int, thresholds []int) int {
	level := 0
	for _, need := range thresholds {
		if have >= need {
			level++
			continue
		}
		break
	}
	return level
}

// Progress reports the current counter for every rule key the evaluator
// understands, so the UI can show "8 / 10" style progress.
func (s *Service) Progress(ctx context.Context, userID string) (map[string]int, error) {
	progress := map[string]int{
		"checkins":             0,
		"distinct_styles":      0,
		"distinct_brands":      0,
		"distinct_countries":   0,
		"distinct_venue_kinds": 0,
		"friends":              0,
		"streak_current":       0,
		"cans":                 0,
		"glass":                0,
		"bottle":               0,
		"fountain":             0,
		"sparkling_water":      0,
		"late_night":           0,
	}

	var checkins int
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT count(*) FROM checkins
		WHERE user_id = ? AND deleted_at IS NULL`, userID).Scan(&checkins); err != nil {
		return nil, err
	}
	progress["checkins"] = checkins

	var distinctStyles int
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT count(DISTINCT d.style_id)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.style_id IS NOT NULL`, userID).
		Scan(&distinctStyles); err != nil {
		return nil, err
	}
	progress["distinct_styles"] = distinctStyles

	var distinctBrands int
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT count(DISTINCT d.brand_id)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND d.brand_id IS NOT NULL`, userID).
		Scan(&distinctBrands); err != nil {
		return nil, err
	}
	progress["distinct_brands"] = distinctBrands

	var distinctCountries int
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT count(DISTINCT v.country)
		FROM checkins c
		JOIN venues v ON v.id = c.venue_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL AND v.country <> ''`, userID).
		Scan(&distinctCountries); err != nil {
		return nil, err
	}
	progress["distinct_countries"] = distinctCountries

	var distinctVenueKinds int
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT count(DISTINCT v.kind)
		FROM checkins c
		JOIN venues v ON v.id = c.venue_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL`, userID).
		Scan(&distinctVenueKinds); err != nil {
		return nil, err
	}
	progress["distinct_venue_kinds"] = distinctVenueKinds

	var friends int
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT count(*) FROM friendships
		WHERE status = 'accepted' AND (user_a = ? OR user_b = ?)`, userID, userID).
		Scan(&friends); err != nil {
		return nil, err
	}
	progress["friends"] = friends

	currentStreak, err := streak.Current(ctx, s.SQL, userID, time.Now())
	if err != nil {
		return nil, err
	}
	progress["streak_current"] = currentStreak

	servingKeys := map[string]string{
		"can":      "cans",
		"glass":    "glass",
		"bottle":   "bottle",
		"fountain": "fountain",
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT serving, count(*) FROM checkins
		WHERE user_id = ? AND deleted_at IS NULL
		GROUP BY serving`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			serving string
			n       int
		)
		if err := rows.Scan(&serving, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if key, ok := servingKeys[serving]; ok {
			progress[key] = n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var sparkling int
	if err := s.SQL.QueryRowContext(ctx, `
		SELECT count(*)
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN styles st ON st.id = d.style_id
		LEFT JOIN styles parent ON parent.id = st.parent_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		  AND (st.slug LIKE 'sparkling%' OR parent.slug LIKE 'sparkling%')`, userID).
		Scan(&sparkling); err != nil {
		return nil, err
	}
	progress["sparkling_water"] = sparkling

	rows, err = s.SQL.QueryContext(ctx, `
		SELECT created_at FROM checkins
		WHERE user_id = ? AND deleted_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var created string
		if err := rows.Scan(&created); err != nil {
			rows.Close()
			return nil, err
		}
		if isLateNight(created) {
			progress["late_night"]++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	return progress, nil
}

func isLateNight(created string) bool {
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return false
	}
	return t.UTC().Hour() < 4
}

// parseThresholds splits "key>=N" or "key>=N1,N2,N3" into the counter key and
// ascending level thresholds. Malformed, negative, empty, or unsorted input is
// rejected so the evaluator ignores the badge instead of panicking.
func parseThresholds(rule string) (string, []int, bool) {
	key, value, ok := strings.Cut(strings.TrimSpace(rule), ">=")
	if !ok {
		return "", nil, false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return "", nil, false
	}

	parts := strings.Split(value, ",")
	thresholds := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", nil, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return "", nil, false
		}
		if len(thresholds) > 0 && n <= thresholds[len(thresholds)-1] {
			return "", nil, false
		}
		thresholds = append(thresholds, n)
	}
	return key, thresholds, true
}

// parseRule splits "key>=N" into its counter key and first threshold. It is the
// single-level view used by admin validation; the evaluator uses
// parseThresholds so multi-level rules keep every level.
func parseRule(rule string) (string, int, bool) {
	key, thresholds, ok := parseThresholds(rule)
	if !ok || len(thresholds) == 0 {
		return "", 0, false
	}
	return key, thresholds[0], true
}
