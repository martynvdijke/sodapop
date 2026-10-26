// Package mix owns user-authored drink mixes: recipes built from two to six
// existing catalog drinks. A mix is stored as a drinks row with kind=mix so it
// can be checked in and displayed with the rest of the catalog.
package mix

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/ent/friendship"
	"sodapop/ent/user"
	"sodapop/internal/catalog"
	"sodapop/internal/db"
)

var (
	ErrNotFound     = errors.New("mix not found")
	ErrInvalidInput = errors.New("invalid mix")
)

const (
	minComponents = 2
	maxComponents = 6
	maxNotes      = 1000
	defaultVolume = 355
	listCap       = 30
)

// Service owns mix creation and reads.
type Service struct {
	Client *ent.Client
	SQL    *sql.DB
}

// New builds a mix service from the shared database bundle.
func New(database *db.DB) *Service {
	return &Service{Client: database.Client, SQL: database.SQL}
}

// Component is one line of a recipe: an existing drink and its part count.
// VolumeML is an optional absolute pour volume; when set it is used instead of
// the drink's own package size. Persisted recipes carry whichever the author
// supplied so existing parts-based mixes keep rendering unchanged.
type Component struct {
	DrinkID  int    `json:"drink_id"`
	Slug     string `json:"slug,omitempty"`
	Name     string `json:"name,omitempty"`
	Parts    int    `json:"parts"`
	VolumeML int    `json:"volume_ml,omitempty"`
}

// Input describes a mix to create.
type Input struct {
	UserID     string
	Name       string
	Notes      string
	Visibility string
	Components []Component
}

// Mix is a stored recipe plus its display data.
type Mix struct {
	ID         int         `json:"id"`
	Slug       string      `json:"slug"`
	Name       string      `json:"name"`
	Notes      string      `json:"notes,omitempty"`
	Visibility string      `json:"visibility"`
	CreatedBy  string      `json:"created_by"`
	CreatedAt  string      `json:"created_at"`
	Components []Component `json:"components"`
}

// recipe is the JSON document stored in drinks.recipe_json. Visibility lives
// here because the shared drinks table has no visibility column.
type recipe struct {
	Notes      string      `json:"notes"`
	Visibility string      `json:"visibility,omitempty"`
	Components []Component `json:"components"`
}

// Create validates the recipe, then writes the mix as a drinks row. The drink
// slug is unique: mix-<slugified-name>-<4hex>.
func (s *Service) Create(ctx context.Context, in Input) (*Mix, error) {
	name := strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(name); n < 2 || n > 80 {
		return nil, fmt.Errorf("%w: name must be 2..80 characters", ErrInvalidInput)
	}
	if utf8.RuneCountInString(in.Notes) > maxNotes {
		return nil, fmt.Errorf("%w: notes must be at most %d characters", ErrInvalidInput, maxNotes)
	}
	if in.UserID == "" {
		return nil, fmt.Errorf("%w: user is required", ErrInvalidInput)
	}
	if len(in.Components) < minComponents || len(in.Components) > maxComponents {
		return nil, fmt.Errorf("%w: a mix needs %d..%d components", ErrInvalidInput, minComponents, maxComponents)
	}

	u, err := s.Client.User.Query().Where(user.ID(in.UserID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	visibility, err := normalizeVisibility(in.Visibility, string(u.DefaultVisibility))
	if err != nil {
		return nil, err
	}

	components := make([]Component, 0, len(in.Components))
	seen := make(map[int]bool, len(in.Components))
	ids := make([]int, 0, len(in.Components))
	for _, c := range in.Components {
		if c.DrinkID <= 0 {
			return nil, fmt.Errorf("%w: component drink is required", ErrInvalidInput)
		}
		if seen[c.DrinkID] {
			return nil, fmt.Errorf("%w: component drinks must be distinct", ErrInvalidInput)
		}
		seen[c.DrinkID] = true
		parts := c.Parts
		if parts == 0 {
			parts = 1
		}
		if parts < 1 || parts > 10 {
			return nil, fmt.Errorf("%w: parts must be 1..10", ErrInvalidInput)
		}
		if c.VolumeML < 0 || c.VolumeML > 4000 {
			return nil, fmt.Errorf("%w: component volume is out of range", ErrInvalidInput)
		}
		components = append(components, Component{
			DrinkID: c.DrinkID, Parts: parts, VolumeML: c.VolumeML,
		})
		ids = append(ids, c.DrinkID)
	}

	drinks, err := s.Client.Drink.Query().
		Where(drink.IDIn(ids...), drink.StatusNEQ(drink.StatusMerged)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	if len(drinks) != len(ids) {
		return nil, fmt.Errorf("%w: component drink not found", ErrInvalidInput)
	}

	byID := make(map[int]*ent.Drink, len(drinks))
	for _, d := range drinks {
		byID[d.ID] = d
	}
	for _, c := range components {
		d := byID[c.DrinkID]
		if d.Kind == drink.KindMix {
			return nil, fmt.Errorf("%w: a mix cannot contain another mix", ErrInvalidInput)
		}
	}

	volume := weightedVolume(components, byID)

	rec := recipe{
		Notes:      strings.TrimSpace(in.Notes),
		Visibility: visibility,
		Components: components,
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}

	now := db.Now()
	for attempt := 0; attempt < 8; attempt++ {
		row, err := s.Client.Drink.Create().
			SetName(name).
			SetSlug(mixSlug(name)).
			SetDescription(strings.TrimSpace(in.Notes)).
			SetContainer(drink.ContainerGlass).
			SetVolumeMl(volume).
			SetKind(drink.KindMix).
			SetRecipeJSON(string(body)).
			SetSource(drink.SourceUser).
			SetStatus(drink.StatusActive).
			SetCreatedBy(in.UserID).
			SetCreatedAt(now).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				continue // slug collision, retry with a fresh suffix
			}
			return nil, err
		}
		return s.mixFromDrink(ctx, row, rec)
	}
	return nil, fmt.Errorf("%w: could not allocate a slug", ErrInvalidInput)
}

// Get returns a public mix by slug. Use GetForViewer to include the owner's or
// a friend's non-public mix.
func (s *Service) Get(ctx context.Context, slug string) (*Mix, error) {
	return s.GetForViewer(ctx, slug, "")
}

// GetForViewer returns a mix the viewer may see: public, their own, or a
// friend's friends-only row. Everything else is reported as not found.
func (s *Service) GetForViewer(ctx context.Context, slug, viewerID string) (*Mix, error) {
	d, err := s.Client.Drink.Query().
		Where(
			drink.SlugEQ(slug),
			drink.KindEQ(drink.KindMix),
			drink.StatusNEQ(drink.StatusMerged),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.viewerMix(ctx, d, viewerID)
}

// GetForViewerByID is GetForViewer keyed by the numeric drink id that the API
// exposes as the mix id.
func (s *Service) GetForViewerByID(ctx context.Context, id int, viewerID string) (*Mix, error) {
	if id <= 0 {
		return nil, ErrNotFound
	}
	d, err := s.Client.Drink.Query().
		Where(
			drink.ID(id),
			drink.KindEQ(drink.KindMix),
			drink.StatusNEQ(drink.StatusMerged),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.viewerMix(ctx, d, viewerID)
}

func (s *Service) viewerMix(ctx context.Context, d *ent.Drink, viewerID string) (*Mix, error) {
	owner := ""
	if d.CreatedBy != nil {
		owner = *d.CreatedBy
	}
	r := parseRecipe(d.RecipeJSON)
	if !s.canView(ctx, r.Visibility, owner, viewerID) {
		return nil, ErrNotFound
	}
	return s.mixFromDrink(ctx, d, r)
}

// List returns recent public mixes plus the viewer's own, capped at 30.
func (s *Service) List(ctx context.Context, viewerID string, limit int) ([]Mix, error) {
	if limit <= 0 || limit > listCap {
		limit = listCap
	}
	return s.query(ctx, `
		SELECT id, slug, name, description, COALESCE(created_by,''), created_at, recipe_json
		FROM drinks
		WHERE kind = 'mix' AND status != 'merged'
		  AND (COALESCE(json_extract(recipe_json, '$.visibility'), 'public') = 'public'
		       OR created_by = ?)
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, viewerID, limit)
}

// Mine returns every mix owned by userID, newest first.
func (s *Service) Mine(ctx context.Context, userID string) ([]Mix, error) {
	return s.query(ctx, `
		SELECT id, slug, name, description, COALESCE(created_by,''), created_at, recipe_json
		FROM drinks
		WHERE kind = 'mix' AND status != 'merged' AND created_by = ?
		ORDER BY created_at DESC, id DESC`, userID)
}

func (s *Service) query(ctx context.Context, q string, args ...any) ([]Mix, error) {
	rows, err := s.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Mix{}
	for rows.Next() {
		var m Mix
		var raw string
		if err := rows.Scan(&m.ID, &m.Slug, &m.Name, &m.Notes, &m.CreatedBy, &m.CreatedAt, &raw); err != nil {
			return nil, err
		}
		r := parseRecipe(raw)
		m.Visibility = r.Visibility
		if m.Notes == "" {
			m.Notes = r.Notes
		}
		m.Components = r.Components
		if m.Components == nil {
			m.Components = []Component{}
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.enrich(ctx, out)
}

// mixFromDrink converts an Ent row plus its parsed recipe into a Mix,
// enriching components with the current drink names and slugs.
func (s *Service) mixFromDrink(ctx context.Context, d *ent.Drink, r recipe) (*Mix, error) {
	owner := ""
	if d.CreatedBy != nil {
		owner = *d.CreatedBy
	}
	notes := d.Description
	if notes == "" {
		notes = r.Notes
	}
	m := &Mix{
		ID:         d.ID,
		Slug:       d.Slug,
		Name:       d.Name,
		Notes:      notes,
		Visibility: r.Visibility,
		CreatedBy:  owner,
		CreatedAt:  d.CreatedAt,
		Components: r.Components,
	}
	if m.Components == nil {
		m.Components = []Component{}
	}
	enriched, err := s.enrich(ctx, []Mix{*m})
	if err != nil {
		return nil, err
	}
	return &enriched[0], nil
}

// enrich fills component slug and name from the catalog. Missing drinks are
// left with only their ID so old recipes never fail to render.
func (s *Service) enrich(ctx context.Context, mixes []Mix) ([]Mix, error) {
	if len(mixes) == 0 {
		return mixes, nil
	}
	ids := make([]int, 0, len(mixes)*maxComponents)
	seen := map[int]bool{}
	for _, m := range mixes {
		for _, c := range m.Components {
			if c.DrinkID > 0 && !seen[c.DrinkID] {
				seen[c.DrinkID] = true
				ids = append(ids, c.DrinkID)
			}
		}
	}
	if len(ids) == 0 {
		return mixes, nil
	}
	rows, err := s.Client.Drink.Query().Where(drink.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int]*ent.Drink, len(rows))
	for _, d := range rows {
		byID[d.ID] = d
	}
	for i := range mixes {
		if mixes[i].Components == nil {
			mixes[i].Components = []Component{}
		}
		for j := range mixes[i].Components {
			if d, ok := byID[mixes[i].Components[j].DrinkID]; ok {
				mixes[i].Components[j].Slug = d.Slug
				mixes[i].Components[j].Name = d.Name
			}
		}
	}
	return mixes, nil
}

func (s *Service) canView(ctx context.Context, visibility, owner, viewerID string) bool {
	switch visibility {
	case "public":
		return true
	case "friends":
		return viewerID != "" && owner != "" && s.areFriends(ctx, viewerID, owner)
	case "private":
		return viewerID != "" && viewerID == owner
	default:
		return false
	}
}

// areFriends queries the friendship graph directly to avoid importing social.
func (s *Service) areFriends(ctx context.Context, a, b string) bool {
	if a == b {
		return a != ""
	}
	x, y := a, b
	if x > y {
		x, y = y, x
	}
	n, err := s.Client.Friendship.Query().
		Where(
			friendship.UserA(x),
			friendship.UserB(y),
			friendship.StatusEQ(friendship.StatusAccepted),
		).
		Count(ctx)
	return err == nil && n > 0
}

// parseRecipe tolerates malformed or legacy JSON: it returns an empty recipe
// rather than panicking.
func parseRecipe(raw string) recipe {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "null" {
		return recipe{Visibility: "public"}
	}
	var r recipe
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return recipe{Visibility: "public"}
	}
	if r.Visibility == "" {
		r.Visibility = "public"
	}
	return r
}

func normalizeVisibility(v, fallback string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		v = fallback
	}
	switch v {
	case "public", "friends", "private":
		return v, nil
	default:
		return "", fmt.Errorf("%w: visibility must be public, friends or private", ErrInvalidInput)
	}
}

// weightedVolume returns the parts-weighted average of component volumes.
func weightedVolume(components []Component, byID map[int]*ent.Drink) int {
	totalParts := 0
	totalVolume := 0
	for _, c := range components {
		d, ok := byID[c.DrinkID]
		if !ok {
			continue
		}
		totalParts += c.Parts
		totalVolume += componentVolume(c, d) * c.Parts
	}
	if totalParts == 0 || totalVolume == 0 {
		return defaultVolume
	}
	volume := totalVolume / totalParts
	if volume <= 0 {
		return defaultVolume
	}
	return volume
}

// componentVolume returns the absolute pour volume when the recipe supplied one,
// otherwise the drink's package size.
func componentVolume(c Component, d *ent.Drink) int {
	if c.VolumeML > 0 {
		return c.VolumeML
	}
	return d.VolumeMl
}

// mixSlug builds mix-<slugified-name>-<4hex>.
func mixSlug(name string) string {
	base := catalog.Slugify(name)
	if base == "" {
		base = "soda"
	}
	return "mix-" + base + "-" + catalog.RandomSuffix()
}
