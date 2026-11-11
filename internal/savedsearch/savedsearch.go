// Package savedsearch owns a member's standing catalog queries and the drop
// alerts they produce. A saved search is scoped to one user; evaluation only
// ever notifies the search's owner and never exposes one member's searches to
// another.
package savedsearch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/ent/flavour"
	"sodapop/ent/notification"
	"sodapop/ent/savedsearch"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

var (
	// ErrNotFound means the saved search is missing or belongs to someone else.
	// Those two cases are deliberately indistinguishable.
	ErrNotFound = errors.New("saved search not found")
	// ErrInvalid means the submitted filters cannot be stored.
	ErrInvalid = errors.New("invalid saved search")
	// ErrLimit means the user already holds the maximum number of searches.
	ErrLimit = errors.New("saved search limit reached")
)

const (
	// MaxPerUser bounds one member's saved searches. The cap keeps the drop
	// evaluation a single small query and stops the settings page growing
	// without limit.
	MaxPerUser = 25
	// DebounceWindow is how long a member is left alone after one drop alert.
	// It is applied per search through last_notified_at and per drink through
	// the existing notification row.
	DebounceWindow = 24 * time.Hour

	maxNameRunes   = 80
	maxQueryRunes  = 120
	maxFilterRunes = 80
)

// beverageTypes mirrors the catalog enum. A saved search accepts only these
// values (or "" for any) so a typo cannot create a search that never matches.
var beverageTypes = map[string]bool{
	"soda": true, "water": true, "tea": true, "coffee": true,
	"na_beer": true, "na_wine": true, "na_spirit": true, "other": true,
}

// Service owns saved-search writes, reads and drop evaluation.
type Service struct {
	Client *ent.Client
	Social *social.Service
}

// New builds the saved-search service. A nil social service falls back to
// writing notification rows directly, which keeps bare test setups working.
func New(database *db.DB, soc *social.Service) *Service {
	return &Service{Client: database.Client, Social: soc}
}

// Input carries the submitted filters. Empty strings mean "any". NotifyDrops is
// a pointer so an omitted value defaults to on while an explicit false is kept.
type Input struct {
	Name         string
	Query        string
	BeverageType string
	StyleSlug    string
	BrandSlug    string
	FlavourSlug  string
	CaffeineFree bool
	SugarFree    bool
	NotifyDrops  *bool
}

// Create validates and stores one saved search. At least one filter must be set:
// an unfiltered search would notify its owner about every drink that enters the
// catalog, which is noise rather than a standing query. The per-user cap is a
// soft limit: it is counted before the insert, so a burst of concurrent creates
// can land slightly over it rather than serialising every write.
func (s *Service) Create(ctx context.Context, userID string, in Input) (*ent.SavedSearch, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, ErrInvalid
	}

	name := strings.TrimSpace(in.Name)
	if utf8.RuneCountInString(name) > maxNameRunes {
		return nil, fmt.Errorf("%w: name is too long", ErrInvalid)
	}
	query := strings.TrimSpace(in.Query)
	if utf8.RuneCountInString(query) > maxQueryRunes {
		return nil, fmt.Errorf("%w: query is too long", ErrInvalid)
	}
	bev := strings.ToLower(strings.TrimSpace(in.BeverageType))
	if bev != "" && !beverageTypes[bev] {
		return nil, fmt.Errorf("%w: unknown beverage type", ErrInvalid)
	}
	styleSlug, err := cleanSlug(in.StyleSlug)
	if err != nil {
		return nil, err
	}
	brandSlug, err := cleanSlug(in.BrandSlug)
	if err != nil {
		return nil, err
	}
	flavourSlug, err := cleanSlug(in.FlavourSlug)
	if err != nil {
		return nil, err
	}

	if query == "" && bev == "" && styleSlug == "" && brandSlug == "" && flavourSlug == "" &&
		!in.CaffeineFree && !in.SugarFree {
		return nil, fmt.Errorf("%w: add at least one filter", ErrInvalid)
	}

	count, err := s.Client.SavedSearch.Query().
		Where(savedsearch.UserID(userID)).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	if count >= MaxPerUser {
		return nil, ErrLimit
	}

	if name == "" {
		name = autoName(query, bev, styleSlug, brandSlug, flavourSlug, in.CaffeineFree, in.SugarFree)
	}

	notify := true
	if in.NotifyDrops != nil {
		notify = *in.NotifyDrops
	}

	row, err := s.Client.SavedSearch.Create().
		SetUserID(userID).
		SetName(name).
		SetQuery(query).
		SetBeverageType(bev).
		SetStyleSlug(styleSlug).
		SetBrandSlug(brandSlug).
		SetFlavourSlug(flavourSlug).
		SetCaffeineFree(in.CaffeineFree).
		SetSugarFree(in.SugarFree).
		SetNotifyDrops(notify).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return row, nil
}

// cleanSlug trims a filter slug, lower-cases it and rejects anything that is not
// a plain catalog slug. Slugs are not checked for existence: a saved search may
// name a style or brand before its first drink arrives.
func cleanSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		return "", nil
	}
	if utf8.RuneCountInString(slug) > maxFilterRunes {
		return "", fmt.Errorf("%w: filter slug is too long", ErrInvalid)
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return "", fmt.Errorf("%w: filter slug contains unsupported characters", ErrInvalid)
		}
	}
	return slug, nil
}

// autoName builds a readable fallback label when the member leaves the name
// blank, so a row never renders as an empty heading.
func autoName(query, bev, style, brand, flavour string, caffeineFree, sugarFree bool) string {
	parts := make([]string, 0, 5)
	for _, v := range []string{query, bev, style, brand, flavour} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	if caffeineFree {
		parts = append(parts, "caffeine-free")
	}
	if sugarFree {
		parts = append(parts, "sugar-free")
	}
	if len(parts) == 0 {
		return "Catalog drop"
	}
	name := strings.Join(parts, " · ")
	if utf8.RuneCountInString(name) > maxNameRunes {
		name = string([]rune(name)[:maxNameRunes])
	}
	return name
}

// List returns a member's saved searches, newest first.
func (s *Service) List(ctx context.Context, userID string) ([]*ent.SavedSearch, error) {
	return s.Client.SavedSearch.Query().
		Where(savedsearch.UserID(userID)).
		Order(ent.Desc(savedsearch.FieldCreatedAt), ent.Desc(savedsearch.FieldID)).
		All(ctx)
}

// Delete removes one of the member's own searches. Another member's row and a
// missing row are both ErrNotFound so the id space cannot be probed.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}
	n, err := s.Client.SavedSearch.Delete().
		Where(savedsearch.IDEQ(id), savedsearch.UserID(userID)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Matches reports whether d satisfies the saved search. Edge-based filters
// (style, brand, flavour) read the drink's loaded edges; a filter whose edge was
// not loaded does not match, so callers that filter on those fields must
// hydrate them first (Evaluate does).
func (s *Service) Matches(search *ent.SavedSearch, d *ent.Drink) bool {
	return Matches(search, d)
}

// Matches is the package-level matcher behind Service.Matches.
func Matches(search *ent.SavedSearch, d *ent.Drink) bool {
	if search == nil || d == nil {
		return false
	}
	if q := strings.ToLower(strings.TrimSpace(search.Query)); q != "" {
		haystack := strings.ToLower(d.Name + "\n" + d.Description)
		if d.Edges.Brand != nil {
			haystack += "\n" + strings.ToLower(d.Edges.Brand.Name)
		}
		if !strings.Contains(haystack, q) {
			return false
		}
	}
	if bev := strings.TrimSpace(search.BeverageType); bev != "" && string(d.BeverageType) != bev {
		return false
	}
	if style := strings.TrimSpace(search.StyleSlug); style != "" {
		if d.Edges.Style == nil || d.Edges.Style.Slug != style {
			return false
		}
	}
	if brand := strings.TrimSpace(search.BrandSlug); brand != "" {
		if d.Edges.Brand == nil || d.Edges.Brand.Slug != brand {
			return false
		}
	}
	if flavour := strings.TrimSpace(search.FlavourSlug); flavour != "" {
		if !hasFlavour(d, flavour) {
			return false
		}
	}
	if search.CaffeineFree && d.CaffeineMgPer100ml != 0 {
		return false
	}
	if search.SugarFree && d.SugarGPer100ml != 0 {
		return false
	}
	return true
}

// hasFlavour reports whether any loaded flavour row carries slug.
func hasFlavour(d *ent.Drink, slug string) bool {
	for _, row := range d.Edges.FlavourRows {
		if row.Edges.Flavour != nil && row.Edges.Flavour.Slug == slug {
			return true
		}
	}
	return false
}

// Evaluate raises drop alerts for a drink that has just become visible. Every
// opted-in search that matches the drink notifies its owner once, at most once
// per DebounceWindow per search and at most once per drink per user. It returns
// the number of members notified.
//
// The drink is re-read with its brand, style and flavour edges so the matcher
// sees the same fields a browse filter would.
func (s *Service) Evaluate(ctx context.Context, d *ent.Drink) (int, error) {
	if d == nil || d.ID == 0 {
		return 0, nil
	}
	full, err := s.Client.Drink.Query().
		Where(drink.ID(d.ID)).
		WithBrand().
		WithStyle().
		WithFlavourRows(func(q *ent.DrinkFlavourQuery) {
			q.WithFlavour()
		}).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, nil
		}
		return 0, err
	}
	// Only a drink in the visible catalog is a "drop". The catalog already
	// checks this before calling, but Evaluate is a public entry point and must
	// not be talked into alerting about a provisional or unpublished row.
	if !Visible(full) {
		return 0, nil
	}

	rows, err := s.Client.SavedSearch.Query().
		Where(savedsearch.NotifyDropsEQ(true)).
		All(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339)
	subject := strconv.Itoa(full.ID)
	text := fmt.Sprintf("%s just dropped into the catalog.", full.Name)

	// One notification per member per drink per evaluation, and only when none
	// of that member's matching searches was alerted recently.
	matched := make(map[string][]*ent.SavedSearch)
	for _, search := range rows {
		if !Matches(search, full) {
			continue
		}
		if recentlyNotified(search.LastNotifiedAt, now) {
			continue
		}
		matched[search.UserID] = append(matched[search.UserID], search)
	}

	notified := 0
	for userID, searches := range matched {
		recent, err := s.recentDropNotification(ctx, userID, subject, now)
		if err != nil {
			return notified, err
		}
		if recent {
			continue
		}
		if err := s.notify(ctx, userID, subject, text); err != nil {
			slog.ErrorContext(ctx, "drop alert notification failed", "err", err, "user_id", userID, "drink_id", full.ID)
			continue
		}
		if _, err := s.Client.SavedSearch.Update().
			Where(savedsearch.IDIn(searchIDs(searches)...)).
			SetLastNotifiedAt(stamp).
			SetUpdatedAt(stamp).
			Save(ctx); err != nil {
			return notified, err
		}
		notified++
	}
	return notified, nil
}

// recentlyNotified reports whether a search is still inside its debounce window.
func recentlyNotified(last *string, now time.Time) bool {
	if last == nil || strings.TrimSpace(*last) == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, *last)
	if err != nil {
		return false
	}
	return now.Sub(at.UTC()) < DebounceWindow
}

// recentDropNotification is the per-drink half of the debounce: a member with two
// matching searches still receives a single alert for this drink.
func (s *Service) recentDropNotification(ctx context.Context, userID, subject string, now time.Time) (bool, error) {
	cutoff := now.Add(-DebounceWindow).Format(time.RFC3339)
	return s.Client.Notification.Query().
		Where(
			notification.UserID(userID),
			notification.KindEQ(notification.KindDropAlert),
			notification.SubjectID(subject),
			notification.CreatedAtGT(cutoff),
		).
		Exist(ctx)
}

// notify writes the drop alert through the social service when one is wired in
// so push and outbound integrations see it exactly once; otherwise it writes the
// row directly.
func (s *Service) notify(ctx context.Context, userID, subject, text string) error {
	if s.Social != nil {
		return s.Social.Notify(ctx, userID, string(notification.KindDropAlert), "", subject, text)
	}
	_, err := s.Client.Notification.Create().
		SetUserID(userID).
		SetKind(notification.KindDropAlert).
		SetSubjectID(subject).
		SetText(text).
		Save(ctx)
	return err
}

func searchIDs(rows []*ent.SavedSearch) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out
}

// Visible reports whether a drink is in the part of the catalog that drop alerts
// describe: published and not retired. The catalog calls Evaluate only for a
// drink that has just crossed into this state.
func Visible(d *ent.Drink) bool {
	if d == nil || !d.Published {
		return false
	}
	switch d.Status {
	case drink.StatusActive, drink.StatusLimited, drink.StatusSeasonal:
		return true
	default:
		return false
	}
}

// FlavourChoice is one datalist entry for the web form.
type FlavourChoice struct {
	Name string
	Slug string
}

// Flavours lists the catalog flavours for the form's suggestions. It is capped
// so the settings page stays a form, not a directory dump.
func (s *Service) Flavours(ctx context.Context) ([]FlavourChoice, error) {
	rows, err := s.Client.Flavour.Query().
		Order(ent.Asc(flavour.FieldName)).
		Limit(60).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FlavourChoice, 0, len(rows))
	for _, row := range rows {
		out = append(out, FlavourChoice{Name: row.Name, Slug: row.Slug})
	}
	return out, nil
}
