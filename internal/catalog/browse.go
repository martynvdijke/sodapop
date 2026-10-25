package catalog

import (
	"context"
	"database/sql"
	"strings"
)

// BrowseDrinksPerPage is the page size for the catalog browse grid. One extra
// row is fetched so HasNext can be reported without a COUNT query.
const browsePerPage = 24

// BrowseFilter narrows the server-rendered catalog browse page. Unknown
// status or sort values are ignored rather than rejected.
type BrowseFilter struct {
	Style, Brand, Flavour, Status, Sort string
	CaffeineFree, SugarFree             bool
	Page                                int // 1-based
}

// BrowseDrink is one card in a browse/brand/style listing.
type BrowseDrink struct {
	ID          int
	Slug        string
	Name        string
	Brand       string
	Style       string
	Container   string
	VolumeML    int
	ImageURL    string
	AvgRating   *float64
	RaterCount  int
	RatingShown bool
	Status      string
}

// BrowseResult is one page of drinks plus the cursor hint.
type BrowseResult struct {
	Drinks  []BrowseDrink
	HasNext bool
	Page    int
}

// BrandSummary is one row on the brands index.
type BrandSummary struct {
	ID          int
	Name        string
	Slug        string
	Country     string
	DrinkCount  int
	LogoMediaID string
}

// BrandPage is a brand with its visible drinks.
type BrandPage struct {
	ID          int
	Name        string
	Slug        string
	Country     string
	Description string
	LogoMediaID string
	Drinks      []BrowseDrink
	Count       int
}

// StyleSummary is one family (with children) or one sub-style row.
type StyleSummary struct {
	ID          int
	ParentID    int
	Name        string
	Slug        string
	Description string
	DrinkCount  int
	Children    []StyleSummary
}

// StylePage is a style with its parent, children and drinks.
type StylePage struct {
	ID          int
	Name        string
	Slug        string
	Description string
	ParentName  string
	Children    []StyleSummary
	Drinks      []BrowseDrink
	Count       int
}

// Drops groups limited, seasonal and retired sodas for the drops page.
type Drops struct {
	Limited      []BrowseDrink
	Seasonal     []BrowseDrink
	Discontinued []BrowseDrink
}

// browseSorts is the ORDER BY whitelist. Values are fixed SQL fragments; the
// caller's sort string is only ever used as a map key.
var browseSorts = map[string]string{
	"name":   "d.name COLLATE NOCASE ASC, d.id ASC",
	"rated":  "CASE WHEN r.avg_rating IS NULL THEN 1 ELSE 0 END ASC, r.avg_rating DESC, d.name COLLATE NOCASE ASC, d.id ASC",
	"newest": "d.created_at DESC, d.id DESC",
}

// browseStatuses is the set a caller may explicitly select. Merged and
// provisional rows are never browsable.
var browseStatuses = map[string]bool{
	"active":       true,
	"limited":      true,
	"seasonal":     true,
	"discontinued": true,
}

// browsableStatusesSQL is the default visibility set shared by every list.
const browsableStatusesSQL = `d.status IN ('active','limited','seasonal')`

const browseSelect = `
	SELECT d.id, d.slug, d.name, COALESCE(b.name, ''), COALESCE(st.name, ''),
	       d.container, d.volume_ml, COALESCE(d.image_url, ''), d.status,
	       r.avg_rating, COALESCE(r.rater_count, 0)
	FROM drinks d
	LEFT JOIN brands b ON b.id = d.brand_id
	LEFT JOIN styles st ON st.id = d.style_id
	LEFT JOIN drink_ratings r ON r.drink_id = d.id`

// BrowseDrinks lists catalog drinks, excluding merged/provisional rows and
// defaulting to active, limited and seasonal. One query per page.
func (s *Service) BrowseDrinks(ctx context.Context, f BrowseFilter) (*BrowseResult, error) {
	page := f.Page
	if page < 1 {
		page = 1
	}

	where := []string{
		`d.status <> 'merged'`,
		`d.status <> 'provisional'`,
	}
	args := []any{}

	status := strings.ToLower(strings.TrimSpace(f.Status))
	if browseStatuses[status] {
		where = append(where, `d.status = ?`)
		args = append(args, status)
	} else {
		where = append(where, browsableStatusesSQL)
	}

	if brand := strings.TrimSpace(f.Brand); brand != "" {
		where = append(where, `b.slug = ?`)
		args = append(args, brand)
	}
	if style := strings.TrimSpace(f.Style); style != "" {
		where = append(where, `(st.slug = ? OR st.parent_id IN (SELECT id FROM styles WHERE slug = ?))`)
		args = append(args, style, style)
	}
	if flavour := strings.TrimSpace(f.Flavour); flavour != "" {
		where = append(where, `d.id IN (
			SELECT df.drink_id FROM drink_flavours df
			JOIN flavours fl ON fl.id = df.flavour_id
			WHERE fl.slug = ?)`)
		args = append(args, flavour)
	}
	if f.CaffeineFree {
		where = append(where, `d.caffeine_mg_per_100ml = 0`)
	}
	if f.SugarFree {
		where = append(where, `d.sugar_g_per_100ml = 0`)
	}

	order, ok := browseSorts[strings.ToLower(strings.TrimSpace(f.Sort))]
	if !ok {
		order = browseSorts["name"]
	}

	args = append(args, browsePerPage+1, (page-1)*browsePerPage)
	drinks, err := s.queryBrowseDrinks(ctx, where, args, order, "? OFFSET ?")
	if err != nil {
		return nil, err
	}

	res := &BrowseResult{Page: page, Drinks: drinks}
	if len(res.Drinks) > browsePerPage {
		res.Drinks = res.Drinks[:browsePerPage]
		res.HasNext = true
	}
	if res.Drinks == nil {
		res.Drinks = []BrowseDrink{}
	}
	return res, nil
}

// queryBrowseDrinks runs the shared browse projection. limit adds a LIMIT
// clause; order is a fixed fragment from browseSorts or a caller constant.
func (s *Service) queryBrowseDrinks(ctx context.Context, where []string, args []any, order, limit string) ([]BrowseDrink, error) {
	q := browseSelect + "\n\tWHERE " + strings.Join(where, " AND ")
	if order != "" {
		q += "\n\tORDER BY " + order
	}
	if limit != "" {
		q += "\n\tLIMIT " + limit
	}
	rows, err := s.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []BrowseDrink{}
	for rows.Next() {
		var d BrowseDrink
		var avg sql.NullFloat64
		if err := rows.Scan(&d.ID, &d.Slug, &d.Name, &d.Brand, &d.Style,
			&d.Container, &d.VolumeML, &d.ImageURL, &d.Status,
			&avg, &d.RaterCount); err != nil {
			return nil, err
		}
		if avg.Valid && d.RaterCount >= MinRaters {
			d.AvgRating = &avg.Float64
			d.RatingShown = true
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListBrands returns every brand with a count of its browsable drinks.
func (s *Service) ListBrands(ctx context.Context) ([]BrandSummary, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT b.id, b.name, b.slug, COALESCE(b.country, ''),
		       COALESCE(b.logo_media_id, ''), COUNT(d.id)
		FROM brands b
		LEFT JOIN drinks d
		       ON d.brand_id = b.id
		      AND d.status IN ('active','limited','seasonal','discontinued')
		GROUP BY b.id, b.name, b.slug, b.country, b.logo_media_id
		ORDER BY b.name COLLATE NOCASE ASC, b.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []BrandSummary{}
	for rows.Next() {
		var b BrandSummary
		if err := rows.Scan(&b.ID, &b.Name, &b.Slug, &b.Country, &b.LogoMediaID, &b.DrinkCount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetBrand returns one brand and its visible drinks.
func (s *Service) GetBrand(ctx context.Context, slug string) (*BrandPage, error) {
	var b BrandPage
	err := s.SQL.QueryRowContext(ctx, `
		SELECT id, name, slug, COALESCE(country, ''), description, COALESCE(logo_media_id, '')
		FROM brands WHERE slug = ?`, slug).
		Scan(&b.ID, &b.Name, &b.Slug, &b.Country, &b.Description, &b.LogoMediaID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	drinks, err := s.queryBrowseDrinks(ctx,
		[]string{`d.brand_id = ?`, `d.status IN ('active','limited','seasonal','discontinued')`},
		[]any{b.ID}, browseSorts["name"], "")
	if err != nil {
		return nil, err
	}
	b.Drinks = drinks
	b.Count = len(drinks)
	return &b, nil
}

// ListStyles returns top-level style families with their children and drink
// counts (a family counts its children's drinks too).
func (s *Service) ListStyles(ctx context.Context) ([]StyleSummary, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT id, COALESCE(parent_id, 0), name, slug, description
		FROM styles
		ORDER BY name COLLATE NOCASE ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type styleRow struct {
		ID, ParentID int
		Name, Slug   string
		Description  string
	}
	var all []styleRow
	for rows.Next() {
		var st styleRow
		if err := rows.Scan(&st.ID, &st.ParentID, &st.Name, &st.Slug, &st.Description); err != nil {
			return nil, err
		}
		all = append(all, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	counts, err := s.styleDrinkCounts(ctx)
	if err != nil {
		return nil, err
	}

	families := []StyleSummary{}
	index := map[int]int{}
	for _, st := range all {
		if st.ParentID != 0 {
			continue
		}
		families = append(families, StyleSummary{
			ID: st.ID, Name: st.Name, Slug: st.Slug, Description: st.Description,
			DrinkCount: counts[st.ID],
		})
		index[st.ID] = len(families) - 1
	}
	for _, st := range all {
		if st.ParentID == 0 {
			continue
		}
		i, ok := index[st.ParentID]
		if !ok {
			continue
		}
		child := StyleSummary{
			ID: st.ID, ParentID: st.ParentID, Name: st.Name, Slug: st.Slug,
			Description: st.Description, DrinkCount: counts[st.ID],
		}
		families[i].Children = append(families[i].Children, child)
		families[i].DrinkCount += child.DrinkCount
	}
	return families, nil
}

// styleDrinkCounts returns visible drink counts keyed by style id.
func (s *Service) styleDrinkCounts(ctx context.Context) (map[int]int, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT style_id, COUNT(*) FROM drinks
		WHERE status IN ('active','limited','seasonal','discontinued')
		  AND style_id IS NOT NULL
		GROUP BY style_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[int]int{}
	for rows.Next() {
		var id, n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// GetStyle returns one style with its parent name, children and drinks.
func (s *Service) GetStyle(ctx context.Context, slug string) (*StylePage, error) {
	var st StylePage
	var parentID int
	err := s.SQL.QueryRowContext(ctx, `
		SELECT id, COALESCE(parent_id, 0), name, slug, description
		FROM styles WHERE slug = ?`, slug).
		Scan(&st.ID, &parentID, &st.Name, &st.Slug, &st.Description)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if parentID != 0 {
		var name string
		err := s.SQL.QueryRowContext(ctx, `SELECT name FROM styles WHERE id = ?`, parentID).Scan(&name)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		st.ParentName = name
	}

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT id, COALESCE(parent_id, 0), name, slug, description
		FROM styles WHERE parent_id = ?
		ORDER BY name COLLATE NOCASE ASC, id ASC`, st.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var child StyleSummary
		if err := rows.Scan(&child.ID, &child.ParentID, &child.Name, &child.Slug, &child.Description); err != nil {
			return nil, err
		}
		st.Children = append(st.Children, child)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	drinks, err := s.queryBrowseDrinks(ctx,
		[]string{
			`(d.style_id = ? OR d.style_id IN (SELECT id FROM styles WHERE parent_id = ?))`,
			`d.status IN ('active','limited','seasonal','discontinued')`,
		},
		[]any{st.ID, st.ID}, browseSorts["name"], "")
	if err != nil {
		return nil, err
	}
	st.Drinks = drinks
	st.Count = len(drinks)
	return &st, nil
}

// BrowseDrops returns limited, seasonal and discontinued sodas, each capped.
func (s *Service) BrowseDrops(ctx context.Context) (*Drops, error) {
	limited, err := s.dropsList(ctx,
		[]string{`d.status = 'limited'`, `COALESCE(d.limited_until, '') <> ''`})
	if err != nil {
		return nil, err
	}
	seasonal, err := s.dropsList(ctx, []string{`d.status = 'seasonal'`})
	if err != nil {
		return nil, err
	}
	discontinued, err := s.dropsList(ctx,
		[]string{`d.status = 'discontinued'`, `COALESCE(d.discontinued_at, '') <> ''`})
	if err != nil {
		return nil, err
	}
	return &Drops{Limited: limited, Seasonal: seasonal, Discontinued: discontinued}, nil
}

func (s *Service) dropsList(ctx context.Context, where []string) ([]BrowseDrink, error) {
	return s.queryBrowseDrinks(ctx, where, nil, browseSorts["name"], "50")
}
