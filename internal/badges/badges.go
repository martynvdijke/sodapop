// Package badges evaluates and lists achievement badges for a user.
//
// Badge definitions live in the badges table. A rule is a small, declarative
// string such as "checkins>=10" or "sparkling_water>=1". Evaluation is
// idempotent: awards are inserted with INSERT OR IGNORE against the unique
// user_id + badge_id index, so concurrent evaluations cannot double-award.
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
// newly awarded by this call. Unknown or malformed rules are ignored.
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
		key, need, ok := parseRule(def.Rule)
		if !ok {
			continue
		}
		have, known := progress[key]
		if !known || have < need {
			continue
		}

		res, err := s.SQL.ExecContext(ctx, `
			INSERT OR IGNORE INTO user_badges (user_id, badge_id, level, awarded_at)
			VALUES (?, ?, 1, ?)`, userID, def.ID, db.Now())
		if err != nil {
			return awarded, err
		}
		inserted, err := res.RowsAffected()
		if err != nil {
			return awarded, err
		}
		if inserted == 0 {
			continue // already awarded, possibly by a concurrent evaluation
		}

		awarded = append(awarded, def.Slug)
		// Secondary rows are best-effort: the award itself is authoritative.
		_, _ = s.Client.Notification.Create().
			SetUserID(userID).
			SetKind(notification.KindBadge).
			SetSubjectID(def.Slug).
			SetText("Badge unlocked: " + def.Name).
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

// Progress reports the current counter for every rule key the evaluator
// understands, so the UI can show "8 / 10" style progress.
func (s *Service) Progress(ctx context.Context, userID string) (map[string]int, error) {
	progress := map[string]int{
		"checkins":        0,
		"distinct_styles": 0,
		"cans":            0,
		"glass":           0,
		"bottle":          0,
		"fountain":        0,
		"sparkling_water": 0,
		"late_night":      0,
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

// parseRule splits "key>=N" into its counter key and threshold.
func parseRule(rule string) (string, int, bool) {
	key, value, ok := strings.Cut(strings.TrimSpace(rule), ">=")
	if !ok {
		return "", 0, false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return "", 0, false
	}
	return key, n, true
}
