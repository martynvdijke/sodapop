// Command seed populates the soda taxonomy and reusable flavour tags.
// It is idempotent: running it repeatedly updates nothing and duplicates nothing.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"

	brandpkg "sodapop/ent/brand"
	drinkpkg "sodapop/ent/drink"
	"sodapop/ent/flavour"
	"sodapop/ent/style"
	"sodapop/ent/user"
	"sodapop/internal/catalog"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

type family struct {
	Name     string
	Slug     string
	Children []string
}

var families = []family{
	{"Cola", "cola", []string{"Regular Cola", "Diet Cola", "Flavoured Cola"}},
	{"Pepper-style", "pepper-style", []string{"Pepper Soda", "Spiced Pepper"}},
	{"Lemon-lime", "lemon-lime", []string{"Classic Lemon-lime", "Lemon", "Lime"}},
	{"Orange", "orange", []string{"Orange Soda", "Blood Orange"}},
	{"Grape", "grape", []string{"Grape Soda", "Concord Grape"}},
	{"Cherry & berry", "cherry-berry", []string{"Cherry Cola", "Black Cherry", "Raspberry", "Blueberry"}},
	{"Tropical", "tropical", []string{"Pineapple", "Pineapple-coconut", "Mango", "Passionfruit"}},
	{"Root beer", "root-beer", []string{"Creamy Root Beer", "Sarsaparilla", "Birch Beer"}},
	{"Cream soda", "cream-soda", []string{"Vanilla Cream", "French Vanilla"}},
	{"Ginger ale", "ginger-ale", []string{"Golden Ginger Ale", "Dry Ginger Ale"}},
	{"Ginger beer", "ginger-beer", []string{"Brewed Ginger Beer", "Spicy Ginger Beer"}},
	{"Fruit punch", "fruit-punch", []string{"Punch", "Citrus Punch"}},
	{"Botanical", "botanical", []string{"Herbal", "Gentian", "Spruce"}},
	{"Tonic & mixers", "mixers", []string{"Tonic Water", "Club Soda", "Bitter Lemon", "Lemonade Mixer"}},
	{"Sparkling water", "sparkling-water", []string{"Seltzer", "Flavoured Sparkling Water", "Mineral Water"}},
	{"Still drinks", "still", []string{"Iced Tea", "Lemonade", "Juice Drink"}},
	{"Functional", "functional", []string{"Energy Drink", "Sports Drink", "Enhanced Water"}},
	{"Fermented", "fermented", []string{"Kombucha", "Water Kefir"}},
}

var flavours = []struct{ Name, Slug, Category string }{
	{"Citrus", "citrus", "fruit"},
	{"Lemon", "lemon", "fruit"},
	{"Lime", "lime", "fruit"},
	{"Orange", "orange", "fruit"},
	{"Grapefruit", "grapefruit", "fruit"},
	{"Cherry", "cherry", "fruit"},
	{"Berry", "berry", "fruit"},
	{"Grape", "grape", "fruit"},
	{"Pineapple", "pineapple", "fruit"},
	{"Tropical", "tropical", "fruit"},
	{"Vanilla", "vanilla", "sweet"},
	{"Caramel", "caramel", "sweet"},
	{"Honey", "honey", "sweet"},
	{"Spice", "spice", "spice"},
	{"Ginger", "ginger", "spice"},
	{"Cinnamon", "cinnamon", "spice"},
	{"Herbal", "herbal", "botanical"},
	{"Floral", "floral", "botanical"},
	{"Mint", "mint", "botanical"},
	{"Cola", "cola", "classic"},
	{"Cream", "cream", "classic"},
	{"Sour", "sour", "acid"},
	{"Bitter", "bitter", "acid"},
	{"Sweet", "sweet", "sweet"},
	{"Salty", "salty", "mineral"},
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	database, err := db.Init(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()

	for _, f := range families {
		parent, err := ensureStyle(ctx, database, f.Name, f.Slug, nil)
		if err != nil {
			log.Fatal(err)
		}
		for _, child := range f.Children {
			slug := slugify(child)
			if _, err := ensureStyle(ctx, database, child, slug, &parent.ID); err != nil {
				log.Fatal(err)
			}
		}
	}

	for _, f := range flavours {
		exists, err := database.Client.Flavour.Query().Where(
			flavour.Slug(f.Slug),
		).Exist(ctx)
		if err != nil {
			log.Fatal(err)
		}
		if exists {
			continue
		}
		if _, err := database.Client.Flavour.Create().
			SetName(f.Name).SetSlug(f.Slug).SetCategory(f.Category).Save(ctx); err != nil {
			log.Fatal(err)
		}
	}

	if os.Getenv("SEED_DEMO") == "1" {
		if err := seedDemo(ctx, database); err != nil {
			log.Fatal(err)
		}
		if err := seedDemoAdmin(ctx, database); err != nil {
			log.Fatal(err)
		}
	}

	count, _ := database.Client.Style.Query().Count(ctx)
	flavourCount, _ := database.Client.Flavour.Query().Count(ctx)
	fmt.Fprintf(os.Stdout, "seeded %d styles and %d flavours\n", count, flavourCount)
}

func ensureStyle(ctx context.Context, database *db.DB, name, slug string, parentID *int) (*styleRow, error) {
	existing, err := database.Client.Style.Query().Where(style.Slug(slug)).Only(ctx)
	if err == nil {
		return &styleRow{ID: existing.ID}, nil
	}
	create := database.Client.Style.Create().SetName(name).SetSlug(slug)
	if parentID != nil {
		create.SetParentID(*parentID)
	}
	row, err := create.Save(ctx)
	if err != nil {
		return nil, err
	}
	return &styleRow{ID: row.ID}, nil
}

type styleRow struct{ ID int }

func slugify(s string) string {
	out := make([]rune, 0, len(s))
	dash := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
			dash = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out = append(out, r)
			dash = false
		default:
			if !dash && len(out) > 0 {
				out = append(out, '-')
				dash = true
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// seedDemoAdmin adds a known administrator so E2E tests can exercise the
// moderator-only surface (drink lifecycle changes). Demo only — SEED_DEMO=1.
func seedDemoAdmin(ctx context.Context, database *db.DB) error {
	const handle = "e2e_admin"
	exists, err := database.Client.User.Query().Where(user.Handle(handle)).Exist(ctx)
	if err != nil || exists {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-horse-battery"), 12)
	if err != nil {
		return err
	}
	_, err = database.Client.User.Create().
		SetHandle(handle).
		SetEmail("e2e_admin@example.com").
		SetEmailVerifiedAt(db.Now()).
		SetPasswordHash(string(hash)).
		SetDisplayName("E2E admin").
		SetRole(user.RoleAdmin).
		Save(ctx)
	return err
}

// seedDemo adds a small, barcode-complete catalog used by E2E tests and local
// development. It is opt-in via SEED_DEMO=1.
func seedDemo(ctx context.Context, database *db.DB) error {
	brand, err := database.Client.Brand.Query().Where(brandpkg.Slug("coca-cola")).Only(ctx)
	if err != nil {
		brand, err = database.Client.Brand.Create().
			SetName("Coca-Cola").SetSlug("coca-cola").SetCountry("US").SetVerified(true).Save(ctx)
		if err != nil {
			return err
		}
	}
	fentimans, err := database.Client.Brand.Query().Where(brandpkg.Slug("fentimans")).Only(ctx)
	if err != nil {
		fentimans, err = database.Client.Brand.Create().
			SetName("Fentimans").SetSlug("fentimans").SetCountry("GB").SetVerified(true).Save(ctx)
		if err != nil {
			return err
		}
	}
	colaStyle, err := database.Client.Style.Query().Where(style.Slug("cola")).Only(ctx)
	if err != nil {
		return err
	}
	gingerStyle, err := database.Client.Style.Query().Where(style.Slug("ginger-beer")).Only(ctx)
	if err != nil {
		return err
	}

	demo := []struct {
		Name, Slug, GTIN string
		Brand            *brandRow
		StyleID          int
		Volume           int
		Container        string
		Sugar            float64
		Caffeine         int
	}{
		{"Coca-Cola", "coca-cola-355", "5449000000996", &brandRow{brand.ID}, colaStyle.ID, 355, "can", 10.6, 9},
		{"Fentimans Curiosity Cola", "fentimans-curiosity-cola-275", "5060337500128", &brandRow{fentimans.ID}, colaStyle.ID, 275, "glass", 9.8, 0},
		{"Fever-Tree Ginger Beer", "fever-tree-ginger-beer-200", "5027323000017", &brandRow{fentimans.ID}, gingerStyle.ID, 200, "glass", 8.4, 0},
	}
	for _, d := range demo {
		exists, err := database.Client.Drink.Query().Where(drinkpkg.Slug(d.Slug)).Exist(ctx)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		row, err := database.Client.Drink.Create().
			SetName(d.Name).SetSlug(d.Slug).SetBrandID(d.Brand.ID).SetStyleID(d.StyleID).
			SetVolumeMl(d.Volume).SetContainer(drinkpkg.Container(d.Container)).
			SetSugarGPer100ml(d.Sugar).SetCaffeineMgPer100ml(d.Caffeine).
			SetStatus("active").Save(ctx)
		if err != nil {
			return err
		}
		normalized, err := catalog.NormalizeGTIN(d.GTIN)
		if err != nil {
			return fmt.Errorf("seed barcode %s: %w", d.GTIN, err)
		}
		if _, err := database.Client.DrinkBarcode.Create().
			SetDrinkID(row.ID).SetGtin14(normalized).SetRawCode(d.GTIN).
			SetSource("brand").Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

type brandRow struct{ ID int }
