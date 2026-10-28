package badges

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"sodapop/ent"
	"sodapop/ent/badge"
	"sodapop/ent/userbadge"
)

// Admin-facing badge management errors.
var (
	ErrBadgeInvalid  = errors.New("invalid badge")
	ErrBadgeNotFound = errors.New("badge not found")
	ErrBadgeAwarded  = errors.New("badge has been awarded")
)

var badgeSlugPattern = regexp.MustCompile(`^[a-z0-9-]{3,48}$`)

// progressRuleKeys lists the counters parseRule may reference. Keeping the set
// in one place means a rule that the evaluator cannot satisfy is rejected at
// write time instead of silently never awarding.
var progressRuleKeys = []string{
	"checkins",
	"distinct_styles",
	"cans",
	"glass",
	"bottle",
	"fountain",
	"sparkling_water",
	"late_night",
}

var progressRuleKeySet = func() map[string]bool {
	out := make(map[string]bool, len(progressRuleKeys))
	for _, key := range progressRuleKeys {
		out[key] = true
	}
	return out
}()

// AdminBadge is a badge definition plus its award count for the admin list.
type AdminBadge struct {
	ID          int
	Slug        string
	Name        string
	Description string
	Kind        string
	Rule        string
	RuleKey     string
	Need        int
	Awarded     int
	CanDelete   bool
}

// BadgeInput is the validated form payload for creating or updating a badge.
type BadgeInput struct {
	Slug        string
	Name        string
	Description string
	Kind        string
	Rule        string
}

// AdminList returns every badge with how many users have earned it. It is the
// only place that exposes award counts; public listings never do.
func (s *Service) AdminList(ctx context.Context) ([]AdminBadge, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT b.id, b.slug, b.name, b.description, b.kind, b.rule,
		       (SELECT COUNT(*) FROM user_badges ub WHERE ub.badge_id = b.id)
		FROM badges b
		ORDER BY b.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AdminBadge, 0, 16)
	for rows.Next() {
		var (
			b     AdminBadge
			kind  string
			rule  string
			award int
		)
		if err := rows.Scan(&b.ID, &b.Slug, &b.Name, &b.Description, &kind, &rule, &award); err != nil {
			return nil, err
		}
		b.Kind = kind
		b.Rule = rule
		if key, need, ok := parseRule(rule); ok {
			b.RuleKey = key
			b.Need = need
		}
		b.Awarded = award
		b.CanDelete = award == 0
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateBadge validates and inserts a badge definition.
func (s *Service) CreateBadge(ctx context.Context, in BadgeInput) (*ent.Badge, error) {
	in, err := normalizeBadgeInput(in)
	if err != nil {
		return nil, err
	}
	exists, err := s.Client.Badge.Query().Where(badge.Slug(in.Slug)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: slug is already in use", ErrBadgeInvalid)
	}
	created, err := s.Client.Badge.Create().
		SetSlug(in.Slug).
		SetName(in.Name).
		SetDescription(in.Description).
		SetKind(badge.Kind(in.Kind)).
		SetRule(in.Rule).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, fmt.Errorf("%w: slug is already in use", ErrBadgeInvalid)
		}
		return nil, err
	}
	return created, nil
}

// UpdateBadge changes a badge's display fields and rule. The slug is immutable:
// awards and external links refer to it, so renaming would orphan them.
func (s *Service) UpdateBadge(ctx context.Context, id int, in BadgeInput) error {
	if id <= 0 {
		return ErrBadgeNotFound
	}
	in, err := normalizeBadgeInput(in)
	if err != nil {
		return err
	}
	if _, err := s.Client.Badge.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return ErrBadgeNotFound
		}
		return err
	}
	_, err = s.Client.Badge.UpdateOneID(id).
		SetName(in.Name).
		SetDescription(in.Description).
		SetKind(badge.Kind(in.Kind)).
		SetRule(in.Rule).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrBadgeNotFound
		}
		return err
	}
	return nil
}

// DeleteBadge removes an unawarded badge definition. Awarded badges are kept so
// user history and notification subject ids stay resolvable.
func (s *Service) DeleteBadge(ctx context.Context, id int) error {
	if id <= 0 {
		return ErrBadgeNotFound
	}
	if _, err := s.Client.Badge.Get(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return ErrBadgeNotFound
		}
		return err
	}
	awarded, err := s.Client.UserBadge.Query().
		Where(userbadge.BadgeID(id)).
		Count(ctx)
	if err != nil {
		return err
	}
	if awarded > 0 {
		return ErrBadgeAwarded
	}
	if err := s.Client.Badge.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return ErrBadgeNotFound
		}
		return err
	}
	return nil
}

// normalizeBadgeInput trims and validates every field, returning the canonical
// values to persist.
func normalizeBadgeInput(in BadgeInput) (BadgeInput, error) {
	in.Slug = strings.TrimSpace(in.Slug)
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Rule = strings.TrimSpace(in.Rule)

	if !badgeSlugPattern.MatchString(in.Slug) {
		return BadgeInput{}, fmt.Errorf("%w: slug must be 3-48 lowercase letters, digits or hyphens", ErrBadgeInvalid)
	}
	if n := len([]rune(in.Name)); n < 1 || n > 80 {
		return BadgeInput{}, fmt.Errorf("%w: name must be 1-80 characters", ErrBadgeInvalid)
	}
	if len([]rune(in.Description)) > 240 {
		return BadgeInput{}, fmt.Errorf("%w: description must be at most 240 characters", ErrBadgeInvalid)
	}
	switch badge.Kind(in.Kind) {
	case badge.KindCore, badge.KindLocal, badge.KindSpecial:
	default:
		return BadgeInput{}, fmt.Errorf("%w: kind must be core, local or special", ErrBadgeInvalid)
	}
	key, need, ok := parseRule(in.Rule)
	if !ok || !progressRuleKeySet[key] {
		return BadgeInput{}, fmt.Errorf("%w: rule must look like key>=N using a known counter", ErrBadgeInvalid)
	}
	if need < 1 || need > 100000 {
		return BadgeInput{}, fmt.Errorf("%w: threshold must be between 1 and 100000", ErrBadgeInvalid)
	}
	in.Rule = key + ">=" + fmt.Sprintf("%d", need)
	return in, nil
}
