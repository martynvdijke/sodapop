package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"sodapop/ent"
	"sodapop/ent/brand"
	"sodapop/ent/drink"
	"sodapop/ent/drinkbarcode"
	"sodapop/ent/drinkflavour"
	"sodapop/ent/style"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

var (
	ErrQueryTooShort  = errors.New("search query must be at least 2 characters")
	ErrNotFound       = errors.New("not found")
	ErrBarcodeTaken   = errors.New("barcode already belongs to another drink")
	ErrInvalidBarcode = errors.New("invalid barcode")
)

const (
	MinSearchLen = 2
	MaxSearchLen = 10
	MinRaters    = 3
)

// Service implements catalog reads and community-provisional writes.
type Service struct {
	Cfg    *config.Config
	Client *ent.Client
	SQL    *sql.DB
	OFF    *OFFClient
}

func New(cfg *config.Config, database *db.DB) *Service {
	return &Service{
		Cfg:    cfg,
		Client: database.Client,
		SQL:    database.SQL,
		OFF:    NewOFFClient(cfg, database),
	}
}

// SearchResult is a compact catalog card. It deliberately carries no
// exhaustive totals, so a client cannot enumerate the catalog through search.
type SearchResult struct {
	ID          int      `json:"id"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Brand       string   `json:"brand,omitempty"`
	Style       string   `json:"style,omitempty"`
	Container   string   `json:"container"`
	VolumeML    int      `json:"volume_ml"`
	ImageURL    string   `json:"image_url,omitempty"`
	Status      string   `json:"status"`
	AvgRating   *float64 `json:"avg_rating,omitempty"`
	RaterCount  int      `json:"rater_count"`
	RatingShown bool     `json:"rating_shown"`
}

// DrinkDetail is a single-drink payload.
type DrinkDetail struct {
	Drink      *ent.Drink
	BrandName  string
	StyleName  string
	Aliases    []string
	Flavours   []FlavourInfo
	BarcodeNum int
	Rating     Rating
}

type FlavourInfo struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Intensity int    `json:"intensity"`
}

type Rating struct {
	Avg        *float64 `json:"avg_rating,omitempty"`
	RaterCount int      `json:"rater_count"`
	Shown      bool     `json:"rating_shown"`
}

// Search uses the FTS5 index with a hard cap on results.
func (s *Service) Search(ctx context.Context, q, styleSlug string, limit int) ([]SearchResult, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < MinSearchLen {
		return nil, ErrQueryTooShort
	}
	if limit <= 0 || limit > MaxSearchLen {
		limit = MaxSearchLen
	}
	match := ftsQuery(q)
	sqlText := `
		SELECT d.id, d.slug, d.name,
		       COALESCE(b.name, ''), COALESCE(st.name, ''),
		       d.container, d.volume_ml, COALESCE(d.image_url, ''), d.status,
		       r.avg_rating, COALESCE(r.rater_count, 0)
		FROM drinks_fts f
		JOIN drinks d ON d.id = f.rowid
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN styles st ON st.id = d.style_id
		LEFT JOIN drink_ratings r ON r.drink_id = d.id
		WHERE drinks_fts MATCH ? AND d.status != 'merged'`
	args := []any{match}
	if styleSlug != "" {
		sqlText += ` AND st.slug = ?`
		args = append(args, styleSlug)
	}
	sqlText += ` ORDER BY bm25(drinks_fts), d.name LIMIT ?`
	args = append(args, limit)

	rows, err := s.SQL.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchResult{}
	for rows.Next() {
		var r SearchResult
		var avg sql.NullFloat64
		if err := rows.Scan(&r.ID, &r.Slug, &r.Name, &r.Brand, &r.Style,
			&r.Container, &r.VolumeML, &r.ImageURL, &r.Status, &avg, &r.RaterCount); err != nil {
			return nil, err
		}
		if avg.Valid && r.RaterCount >= MinRaters {
			r.AvgRating = &avg.Float64
			r.RatingShown = true
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetBySlug returns one drink with its rating and flavour profile.
func (s *Service) GetBySlug(ctx context.Context, slug string) (*DrinkDetail, error) {
	d, err := s.Client.Drink.Query().
		Where(drink.Slug(slug), drink.StatusNEQ(drink.StatusMerged)).
		WithBrand().
		WithStyle().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.detail(ctx, d)
}

// GetByID returns one drink by numeric id.
func (s *Service) GetByID(ctx context.Context, id int) (*DrinkDetail, error) {
	d, err := s.Client.Drink.Query().
		Where(drink.ID(id), drink.StatusNEQ(drink.StatusMerged)).
		WithBrand().
		WithStyle().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.detail(ctx, d)
}

func (s *Service) detail(ctx context.Context, d *ent.Drink) (*DrinkDetail, error) {
	out := &DrinkDetail{Drink: d}
	if d.Edges.Brand != nil {
		out.BrandName = d.Edges.Brand.Name
	}
	if d.Edges.Style != nil {
		out.StyleName = d.Edges.Style.Name
	}
	aliases, err := s.SQL.QueryContext(ctx, `SELECT alias FROM drink_alias WHERE drink_id = ? ORDER BY alias`, d.ID)
	if err == nil {
		defer aliases.Close()
		for aliases.Next() {
			var a string
			if err := aliases.Scan(&a); err == nil {
				out.Aliases = append(out.Aliases, a)
			}
		}
	}
	flavours, err := s.Client.DrinkFlavour.Query().
		Where(drinkflavour.DrinkID(d.ID)).
		WithFlavour().
		All(ctx)
	if err == nil {
		for _, f := range flavours {
			if f.Edges.Flavour != nil {
				out.Flavours = append(out.Flavours, FlavourInfo{
					Name: f.Edges.Flavour.Name, Slug: f.Edges.Flavour.Slug, Intensity: f.Intensity,
				})
			}
		}
	}
	out.BarcodeNum, _ = s.Client.DrinkBarcode.Query().Where(drinkbarcode.DrinkID(d.ID)).Count(ctx)
	out.Rating = s.rating(ctx, d.ID)
	return out, nil
}

func (s *Service) rating(ctx context.Context, drinkID int) Rating {
	var avg sql.NullFloat64
	var raters int
	_ = s.SQL.QueryRowContext(ctx, `SELECT avg_rating, COALESCE(rater_count,0) FROM drink_ratings WHERE drink_id = ?`, drinkID).Scan(&avg, &raters)
	r := Rating{RaterCount: raters}
	if avg.Valid && raters >= MinRaters {
		r.Avg = &avg.Float64
		r.Shown = true
	}
	return r
}

// CreateInput is the minimum information needed for a provisional soda.
type CreateInput struct {
	Name        string
	Brand       string
	StyleSlug   string
	Container   string
	VolumeML    int
	Sweetener   string
	Carbonation string
	Acid        string
	Barcode     string
	Description string
}

// CreateProvisional creates an unverified community entry unless an equivalent
// drink already exists. The bool reports whether a new row was created.
func (s *Service) CreateProvisional(ctx context.Context, userID string, in CreateInput) (*ent.Drink, bool, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Brand = strings.TrimSpace(in.Brand)
	if in.Name == "" {
		return nil, false, errors.New("drink name is required")
	}
	if in.VolumeML <= 0 {
		in.VolumeML = 355
	}
	if in.Container == "" {
		in.Container = "can"
	}
	if in.Barcode != "" {
		gtin, err := NormalizeGTIN(in.Barcode)
		if err != nil {
			return nil, false, err
		}
		in.Barcode = gtin
	}

	if in.Barcode != "" {
		existing, err := s.Client.DrinkBarcode.Query().
			Where(drinkbarcode.Gtin14(in.Barcode)).
			WithDrink().
			Only(ctx)
		if err == nil && existing.Edges.Drink != nil {
			return existing.Edges.Drink, false, nil
		}
	}

	// Soft key: lower(brand) + normalized(lower(name)) + volume + container.
	var existingID int
	err := s.SQL.QueryRowContext(ctx, `
		SELECT d.id FROM drinks d
		LEFT JOIN brands b ON b.id = d.brand_id
		WHERE lower(d.name) = lower(?)
		  AND d.volume_ml = ? AND d.container = ?
		  AND lower(COALESCE(b.name, '')) = lower(?)
		LIMIT 1`, normalizeName(in.Name), in.VolumeML, in.Container, in.Brand).Scan(&existingID)
	if err == nil {
		d, err := s.Client.Drink.Get(ctx, existingID)
		return d, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	brandID, err := s.resolveBrand(ctx, in.Brand, userID)
	if err != nil {
		return nil, false, err
	}
	styleID, err := s.resolveStyle(ctx, in.StyleSlug)
	if err != nil {
		return nil, false, err
	}

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	b := tx.Drink.Create().
		SetName(in.Name).
		SetSlug(s.uniqueSlug(ctx, in.Name, in.Brand)).
		SetDescription(in.Description).
		SetContainer(containerEnum(in.Container)).
		SetVolumeMl(in.VolumeML).
		SetSweetener(sweetenerEnum(in.Sweetener)).
		SetCarbonation(carbonationEnum(in.Carbonation)).
		SetAcid(acidEnum(in.Acid)).
		SetSource(drink.SourceUser).
		SetStatus(drink.StatusProvisional).
		SetCreatedBy(userID)
	if brandID != nil {
		b.SetBrandID(*brandID)
	}
	if styleID != nil {
		b.SetStyleID(*styleID)
	}
	d, err := b.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	if in.Barcode != "" {
		if _, err := tx.DrinkBarcode.Create().
			SetDrinkID(d.ID).SetGtin14(in.Barcode).SetRawCode(in.Barcode).
			SetSymbology("").SetSource(drinkbarcode.SourceUser).Save(ctx); err != nil {
			_ = tx.Rollback()
			return nil, false, err
		}
	}
	after := map[string]any{
		"name":       d.Name,
		"slug":       d.Slug,
		"brand_id":   brandID,
		"style_id":   styleID,
		"container":  string(d.Container),
		"volume_ml":  d.VolumeMl,
		"status":     string(d.Status),
		"source":     string(d.Source),
		"created_by": d.CreatedBy,
	}
	if in.Barcode != "" {
		after["barcode"] = in.Barcode
	}
	if err := recordRevisionTx(ctx, tx, "drink", d.ID, &userID, "create", nil, after); err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return d, true, nil
}

// AttachBarcode associates an additional GTIN with an existing drink.
func (s *Service) AttachBarcode(ctx context.Context, drinkID int, raw, symbology, source string) (*ent.DrinkBarcode, error) {
	gtin, err := NormalizeGTIN(raw)
	if err != nil {
		return nil, err
	}
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := tx.DrinkBarcode.Query().Where(drinkbarcode.Gtin14(gtin)).Only(ctx)
	if err == nil {
		_ = tx.Rollback()
		if existing.DrinkID != drinkID {
			return nil, ErrBarcodeTaken
		}
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		_ = tx.Rollback()
		return nil, err
	}
	beforeCount, err := tx.DrinkBarcode.Query().Where(drinkbarcode.DrinkID(drinkID)).Count(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	row, err := tx.DrinkBarcode.Create().
		SetDrinkID(drinkID).SetGtin14(gtin).SetRawCode(raw).
		SetSymbology(symbology).SetSource(barcodeSource(source)).Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := recordRevisionTx(ctx, tx, "drink", drinkID, nil, "barcode",
		map[string]any{"barcode_count": beforeCount},
		map[string]any{"barcode_count": beforeCount + 1, "gtin14": gtin, "symbology": symbology, "source": row.Source},
	); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) resolveBrand(ctx context.Context, name string, userID string) (*int, error) {
	if name == "" {
		return nil, nil
	}
	b, err := s.Client.Brand.Query().Where(brand.NameEqualFold(name)).Only(ctx)
	if err == nil {
		return &b.ID, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	slug := s.uniqueBrandSlug(ctx, name)
	b, err = s.Client.Brand.Create().
		SetName(name).SetSlug(slug).SetSource(brand.SourceUser).
		SetNillableCreatedBy(&userID).Save(ctx)
	if err != nil {
		return nil, err
	}
	return &b.ID, nil
}

func (s *Service) resolveStyle(ctx context.Context, slug string) (*int, error) {
	if strings.TrimSpace(slug) == "" {
		return nil, nil
	}
	st, err := s.Client.Style.Query().Where(style.Slug(slug)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &st.ID, nil
}

func (s *Service) uniqueSlug(ctx context.Context, name, brandName string) string {
	base := Slugify(name)
	if brandName != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(brandName)) {
		base = Slugify(brandName + " " + name)
	}
	if base == "" {
		base = "soda"
	}
	slug := base
	for i := 2; i < 100; i++ {
		n, err := s.Client.Drink.Query().Where(drink.Slug(slug)).Count(ctx)
		if err == nil && n == 0 {
			return slug
		}
		slug = base + "-" + strconv.Itoa(i)
	}
	return base + "-" + RandomSuffix()
}

func (s *Service) uniqueBrandSlug(ctx context.Context, name string) string {
	base := Slugify(name)
	if base == "" {
		base = "brand"
	}
	slug := base
	for i := 2; i < 100; i++ {
		n, err := s.Client.Brand.Query().Where(brand.Slug(slug)).Count(ctx)
		if err == nil && n == 0 {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	return base + "-" + RandomSuffix()
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify produces a stable URL slug.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "&", " and ")
	s = slugStrip.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func normalizeName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func normalizeEnum(v, fallback string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return fallback
	}
	return v
}

func ftsQuery(q string) string {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) == 0 {
			continue
		}
		parts = append(parts, `"`+strings.ReplaceAll(f, `"`, `""`)+`"*`)
	}
	if len(parts) == 0 {
		return `"__no_terms__"`
	}
	return strings.Join(parts, " AND ")
}

func containerEnum(v string) drink.Container {
	switch v {
	case "glass":
		return drink.ContainerGlass
	case "pet":
		return drink.ContainerPet
	case "fountain":
		return drink.ContainerFountain
	case "carton":
		return drink.ContainerCarton
	case "other":
		return drink.ContainerOther
	default:
		return drink.ContainerCan
	}
}

func sweetenerEnum(v string) drink.Sweetener {
	switch normalizeEnum(v, "") {
	case "sugar":
		return drink.SweetenerSugar
	case "hfcs":
		return drink.SweetenerHfcs
	case "aspartame":
		return drink.SweetenerAspartame
	case "sucralose":
		return drink.SweetenerSucralose
	case "acesulfame_k":
		return drink.SweetenerAcesulfameK
	case "saccharin":
		return drink.SweetenerSaccharin
	case "stevia":
		return drink.SweetenerStevia
	case "monk_fruit":
		return drink.SweetenerMonkFruit
	case "allulose":
		return drink.SweetenerAllulose
	case "erythritol":
		return drink.SweetenerErythritol
	case "mixed":
		return drink.SweetenerMixed
	case "none":
		return drink.SweetenerNone
	default:
		return drink.SweetenerUnknown
	}
}

func carbonationEnum(v string) drink.Carbonation {
	switch normalizeEnum(v, "") {
	case "still":
		return drink.CarbonationStill
	case "light":
		return drink.CarbonationLight
	case "standard":
		return drink.CarbonationStandard
	case "extra":
		return drink.CarbonationExtra
	default:
		return drink.CarbonationUnknown
	}
}

func acidEnum(v string) drink.Acid {
	switch normalizeEnum(v, "") {
	case "phosphoric":
		return drink.AcidPhosphoric
	case "citric":
		return drink.AcidCitric
	case "malic":
		return drink.AcidMalic
	case "tartaric":
		return drink.AcidTartaric
	case "none":
		return drink.AcidNone
	default:
		return drink.AcidUnknown
	}
}

// barcodeSource maps a loose string to the generated enum.
func barcodeSource(v string) drinkbarcode.Source {
	switch v {
	case string(drinkbarcode.SourceOff):
		return drinkbarcode.SourceOff
	case string(drinkbarcode.SourceScan):
		return drinkbarcode.SourceScan
	case string(drinkbarcode.SourceBrand):
		return drinkbarcode.SourceBrand
	default:
		return drinkbarcode.SourceUser
	}
}
