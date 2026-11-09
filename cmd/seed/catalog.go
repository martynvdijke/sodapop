package main

import (
	"context"
	"fmt"

	"sodapop/ent"
	brandpkg "sodapop/ent/brand"
	drinkpkg "sodapop/ent/drink"
	"sodapop/ent/style"
	"sodapop/internal/db"
)

// starterBrand is a Sodapop-authored producer row. Brands are keyed by slug so
// seeding is idempotent and coexists with the SEED_DEMO rows.
type starterBrand struct {
	Name    string
	Slug    string
	Country string
}

// starterDrink is a Sodapop-authored catalog row. Nutrition pointers are nil
// when we are not confident of the value, so the schema default is left alone
// rather than inventing a number. Barcodes are deliberately absent: Open Food
// Facts data is never blended into authored rows.
type starterDrink struct {
	Name         string
	Slug         string
	BrandSlug    string
	StyleSlug    string
	Container    string
	VolumeMl     int
	Sugar        *float64
	Caffeine     *int
	Calories     *int
	BeverageType string
	ABVPercent   float64
}

func f(v float64) *float64 { return &v }
func i(v int) *int         { return &v }

var starterBrands = []starterBrand{
	{"Coca-Cola", "coca-cola", "US"},
	{"PepsiCo", "pepsi", "US"},
	{"Keurig Dr Pepper", "keurig-dr-pepper", "US"},
	{"Schweppes", "schweppes", "GB"},
	{"Fever-Tree", "fever-tree", "GB"},
	{"Fentimans", "fentimans", "GB"},
	{"Bundaberg", "bundaberg", "AU"},
	{"San Pellegrino", "san-pellegrino", "IT"},
	{"Perrier", "perrier", "FR"},
	{"LaCroix", "lacroix", "US"},
	{"Topo Chico", "topo-chico", "MX"},
	{"Jarritos", "jarritos", "MX"},
	{"Red Bull", "red-bull", "AT"},
	{"Monster", "monster", "US"},
	{"Celsius", "celsius", "US"},
	{"Arizona", "arizona", "US"},
	{"Lipton", "lipton", "GB"},
	{"Pure Leaf", "pure-leaf", "US"},
	{"GT's", "gts", "US"},
	{"Olipop", "olipop", "US"},
	{"Poppi", "poppi", "US"},
	{"Starbucks", "starbucks", "US"},
	{"Nescafé", "nescafe", "CH"},
	{"Athletic Brewing", "athletic-brewing", "US"},
	{"Lyre's", "lyres", "AU"},
	{"Seedlip", "seedlip", "GB"},
}

// starterDrinks is a curated cross-category catalog that makes a fresh
// instance useful without SEED_DEMO. Every StyleSlug already exists in
// families (see main.go), so no style names are invented here.
var starterDrinks = []starterDrink{
	// Cola
	{"Coca-Cola", "coca-cola-330-glass", "coca-cola", "regular-cola", "glass", 330, f(10.6), i(9), i(42), "", 0},
	{"Coca-Cola Zero Sugar", "coca-cola-zero-sugar-330", "coca-cola", "diet-cola", "can", 330, f(0), i(9), i(0), "", 0},
	{"Pepsi", "pepsi-330", "pepsi", "regular-cola", "can", 330, f(10.9), i(10), i(43), "", 0},
	{"Pepsi Max", "pepsi-max-330", "pepsi", "diet-cola", "can", 330, f(0), i(10), i(0), "", 0},
	{"Olipop Vintage Cola", "olipop-vintage-cola-355", "olipop", "flavoured-cola", "can", 355, nil, nil, nil, "", 0},
	{"Poppi Classic Cola", "poppi-classic-cola-355", "poppi", "flavoured-cola", "can", 355, nil, nil, nil, "", 0},

	// Lemon-lime
	{"Sprite", "sprite-330", "coca-cola", "classic-lemon-lime", "can", 330, f(9.0), nil, i(38), "", 0},
	{"7UP", "7up-355", "keurig-dr-pepper", "classic-lemon-lime", "can", 355, f(10.7), nil, i(39), "", 0},

	// Pepper-style
	{"Dr Pepper", "dr-pepper-355", "keurig-dr-pepper", "pepper-soda", "can", 355, f(10.6), i(10), i(42), "", 0},

	// Root beer
	{"A&W Root Beer", "aw-root-beer-355", "keurig-dr-pepper", "creamy-root-beer", "can", 355, f(12.7), nil, i(48), "", 0},
	{"Barq's Root Beer", "barqs-root-beer-355", "coca-cola", "creamy-root-beer", "can", 355, f(11.8), i(6), i(45), "", 0},

	// Ginger beer
	{"Fever-Tree Refreshingly Light Ginger Beer", "fever-tree-light-ginger-beer-200", "fever-tree", "brewed-ginger-beer", "glass", 200, f(3.8), nil, i(15), "", 0},
	{"Bundaberg Ginger Beer", "bundaberg-ginger-beer-375", "bundaberg", "brewed-ginger-beer", "glass", 375, f(9.6), nil, i(38), "", 0},

	// Ginger ale
	{"Schweppes Ginger Ale", "schweppes-ginger-ale-330", "schweppes", "dry-ginger-ale", "can", 330, f(8.9), nil, i(36), "", 0},

	// Orange
	{"San Pellegrino Aranciata", "san-pellegrino-aranciata-330", "san-pellegrino", "orange-soda", "can", 330, f(8.0), nil, i(34), "", 0},
	{"Jarritos Mandarin", "jarritos-mandarin-370", "jarritos", "orange-soda", "glass", 370, f(11.9), nil, i(49), "", 0},
	{"Fanta Orange", "fanta-orange-355", "coca-cola", "orange-soda", "can", 355, f(12.3), nil, i(49), "", 0},

	// Sparkling water
	{"San Pellegrino Sparkling Natural Mineral Water", "san-pellegrino-mineral-water-500", "san-pellegrino", "mineral-water", "pet", 500, f(0), nil, i(0), "", 0},
	{"Perrier Sparkling Natural Mineral Water", "perrier-330", "perrier", "mineral-water", "glass", 330, f(0), nil, i(0), "", 0},
	{"LaCroix LimonCello", "lacroix-limoncello-355", "lacroix", "flavoured-sparkling-water", "can", 355, f(0), nil, i(0), "", 0},
	{"Topo Chico Mineral Water", "topo-chico-mineral-water-355", "topo-chico", "mineral-water", "glass", 355, f(0), nil, i(0), "", 0},

	// Energy
	{"Red Bull Energy Drink", "red-bull-250", "red-bull", "energy-drink", "can", 250, f(11.0), i(32), i(45), "", 0},
	{"Monster Energy", "monster-energy-500", "monster", "energy-drink", "can", 500, f(10.8), i(32), i(45), "", 0},
	{"Celsius Sparkling Orange", "celsius-sparkling-orange-355", "celsius", "energy-drink", "can", 355, f(0), i(56), i(3), "", 0},

	// Iced tea
	{"Arizona Green Tea with Ginseng and Honey", "arizona-green-tea-680", "arizona", "iced-tea", "can", 680, f(7.0), nil, i(29), "", 0},
	{"Lipton Iced Tea Lemon", "lipton-iced-tea-lemon-500", "lipton", "iced-tea", "pet", 500, f(6.0), nil, i(24), "", 0},
	{"Pure Leaf Unsweetened Black Tea", "pure-leaf-unsweetened-black-tea-547", "pure-leaf", "iced-tea", "glass", 547, f(0), nil, i(0), "", 0},

	// Kombucha
	{"GT's Kombucha Gingerade", "gts-kombucha-gingerade-480", "gts", "kombucha", "glass", 480, f(2.5), nil, i(13), "", 0},

	// Lemonade
	{"Fentimans Victorian Lemonade", "fentimans-victorian-lemonade-275", "fentimans", "lemonade", "glass", 275, f(9.5), nil, i(40), "", 0},

	// Tea
	{"Lipton Green Tea", "lipton-green-tea-500", "lipton", "green-tea", "glass", 500, f(0), i(3), i(1), "tea", 0},
	{"Pure Leaf Black Tea", "pure-leaf-black-tea-547", "pure-leaf", "black-tea", "glass", 547, f(0), nil, i(0), "tea", 0},
	{"Starbucks Matcha Latte", "starbucks-matcha-latte-355", "starbucks", "matcha", "can", 355, f(7.5), nil, i(30), "tea", 0},

	// Coffee
	{"Starbucks Espresso Roast", "starbucks-espresso-roast-250", "starbucks", "espresso", "glass", 250, f(0), i(3), i(1), "coffee", 0},
	{"Nescafé Cold Brew", "nescafe-cold-brew-250", "nescafe", "cold-brew", "can", 250, f(0), i(2), i(0), "coffee", 0},

	// Non-alcoholic
	{"Athletic Brewing Run Wild IPA", "athletic-run-wild-ipa-355", "athletic-brewing", "na-beer", "can", 355, nil, nil, nil, "na_beer", 0},
	{"Lyre's Dry London Spirit", "lyres-dry-london-spirit-700", "lyres", "na-spirits", "glass", 700, nil, nil, nil, "na_spirit", 0},
}

// seedStarterCatalog ensures the curated brands and drinks exist. It is
// idempotent: brands are matched by slug and drinks by slug, and existing rows
// are never updated. Gated by SEED_CATALOG=1 so search and E2E stay
// deterministic by default.
func seedStarterCatalog(ctx context.Context, database *db.DB) error {
	// Every starter drink references a style from the shared taxonomy, so make
	// sure it exists even when the catalog is seeded on its own.
	if err := seedStyleTaxonomy(ctx, database); err != nil {
		return fmt.Errorf("seed style taxonomy: %w", err)
	}

	brandIDs := make(map[string]int, len(starterBrands))
	for _, b := range starterBrands {
		row, err := database.Client.Brand.Query().Where(brandpkg.Slug(b.Slug)).Only(ctx)
		if err != nil {
			if !ent.IsNotFound(err) {
				return fmt.Errorf("lookup brand %s: %w", b.Slug, err)
			}
			row, err = database.Client.Brand.Create().
				SetName(b.Name).SetSlug(b.Slug).SetCountry(b.Country).
				SetVerified(true).Save(ctx)
			if err != nil {
				return fmt.Errorf("create brand %s: %w", b.Slug, err)
			}
		}
		brandIDs[b.Slug] = row.ID
	}

	styleIDs := make(map[string]int, len(starterDrinks))
	for _, d := range starterDrinks {
		styleID, ok := styleIDs[d.StyleSlug]
		if !ok {
			row, err := database.Client.Style.Query().Where(style.Slug(d.StyleSlug)).Only(ctx)
			if err != nil {
				return fmt.Errorf("lookup style %s for %s: %w", d.StyleSlug, d.Slug, err)
			}
			styleID = row.ID
			styleIDs[d.StyleSlug] = styleID
		}

		exists, err := database.Client.Drink.Query().Where(drinkpkg.Slug(d.Slug)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("lookup drink %s: %w", d.Slug, err)
		}
		if exists {
			continue
		}

		beverageType := drinkpkg.BeverageTypeSoda
		if d.BeverageType != "" {
			beverageType = drinkpkg.BeverageType(d.BeverageType)
		}
		create := database.Client.Drink.Create().
			SetName(d.Name).
			SetSlug(d.Slug).
			SetBrandID(brandIDs[d.BrandSlug]).
			SetStyleID(styleID).
			SetContainer(drinkpkg.Container(d.Container)).
			SetBeverageType(beverageType).
			SetAbvPercent(d.ABVPercent).
			SetVolumeMl(d.VolumeMl).
			SetStatus(drinkpkg.StatusActive)
		if d.Sugar != nil {
			create.SetSugarGPer100ml(*d.Sugar)
		}
		if d.Caffeine != nil {
			create.SetCaffeineMgPer100ml(*d.Caffeine)
		}
		if d.Calories != nil {
			create.SetCaloriesKcalPer100ml(*d.Calories)
		}
		if _, err := create.Save(ctx); err != nil {
			return fmt.Errorf("create drink %s: %w", d.Slug, err)
		}
	}
	return nil
}
