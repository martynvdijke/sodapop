package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Brand is the producer of a drink (Coca-Cola, Boylan, Fever-Tree, ...).
type Brand struct {
	ent.Schema
}

func (Brand) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("name").NotEmpty(),
		field.String("slug").NotEmpty().Unique(),
		field.String("country").Optional().Nillable(),
		field.String("website").Optional().Nillable(),
		field.String("description").Default(""),
		field.String("logo_media_id").Optional().Nillable(),
		field.Enum("source").Values("local", "off", "user").Default("local"),
		field.Bool("verified").Default(false),
		field.String("created_by").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

func (Brand) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("drinks", Drink.Type),
	}
}

// Style is a two-level soda taxonomy row (family or substyle).
type Style struct {
	ent.Schema
}

func (Style) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Int("parent_id").Optional().Nillable(),
		field.String("name").NotEmpty().Unique(),
		field.String("slug").NotEmpty().Unique(),
		field.String("description").Default(""),
	}
}

func (Style) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("drinks", Drink.Type),
	}
}

// Flavour is a reusable tasting tag such as citrus or vanilla.
type Flavour struct {
	ent.Schema
}

func (Flavour) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("name").NotEmpty().Unique(),
		field.String("slug").NotEmpty().Unique(),
		field.String("category").Default("general"),
	}
}

func (Flavour) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("drink_flavours", DrinkFlavour.Type),
	}
}

// Drink is the shared catalog row for a specific packaged soda.
type Drink struct {
	ent.Schema
}

func (Drink) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Int("brand_id").Optional().Nillable(),
		field.Int("style_id").Optional().Nillable(),
		field.String("name").NotEmpty(),
		field.String("slug").NotEmpty().Unique(),
		field.String("description").Default(""),
		field.String("image_url").Optional().Nillable(),
		field.Enum("container").Values("can", "glass", "pet", "fountain", "carton", "other").Default("can"),
		field.Int("volume_ml").Default(355),
		field.Int("caffeine_mg_per_100ml").Default(0),
		field.Float("sugar_g_per_100ml").Default(0),
		field.Int("calories_kcal_per_100ml").Default(0),
		field.Enum("sweetener").Values("sugar", "hfcs", "aspartame", "sucralose", "acesulfame_k", "saccharin", "stevia", "monk_fruit", "allulose", "erythritol", "mixed", "none", "unknown").Default("unknown"),
		field.Enum("carbonation").Values("still", "light", "standard", "extra", "unknown").Default("unknown"),
		field.Enum("acid").Values("phosphoric", "citric", "malic", "tartaric", "none", "unknown").Default("unknown"),
		field.Enum("kind").Values("packaged", "mix").Default("packaged"),
		field.String("recipe_json").Default("{}"),
		field.Enum("source").Values("local", "off", "user").Default("local"),
		field.Enum("status").Values("provisional", "active", "merged", "limited", "seasonal", "discontinued").Default("active"),
		field.Bool("published").Default(false),
		field.String("discontinued_at").Optional().Nillable(),
		field.String("limited_until").Optional().Nillable(),
		field.String("created_by").Optional().Nillable(),
		field.Int("merged_into_id").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

func (Drink) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("brand", Brand.Type).Ref("drinks").Field("brand_id").Unique(),
		edge.From("style", Style.Type).Ref("drinks").Field("style_id").Unique(),
		edge.To("barcodes", DrinkBarcode.Type),
		edge.To("alias_rows", DrinkAlias.Type),
		edge.To("flavour_rows", DrinkFlavour.Type),
		edge.To("checkins", Checkin.Type),
		edge.To("images", DrinkImage.Type),
	}
}

func (Drink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name"),
		index.Fields("brand_id"),
		index.Fields("style_id"),
	}
}

// DrinkBarcode maps a package barcode to a drink. GTIN-14 is the canonical key.
type DrinkBarcode struct {
	ent.Schema
}

func (DrinkBarcode) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Int("drink_id"),
		field.String("gtin14").NotEmpty().Unique(),
		field.String("raw_code").Default(""),
		field.String("symbology").Default(""),
		field.Enum("source").Values("scan", "off", "user", "brand").Default("user"),
		field.String("created_at").DefaultFunc(now),
	}
}

func (DrinkBarcode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("drink", Drink.Type).Ref("barcodes").Field("drink_id").Unique().Required(),
	}
}

// DrinkFlavour stores a flavour intensity from 0 (absent) to 5 (dominant).
type DrinkFlavour struct {
	ent.Schema
}

func (DrinkFlavour) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Int("drink_id"),
		field.Int("flavour_id"),
		field.Int("intensity").Default(3).Range(0, 5),
	}
}

func (DrinkFlavour) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("drink", Drink.Type).Ref("flavour_rows").Field("drink_id").Unique().Required(),
		edge.From("flavour", Flavour.Type).Ref("drink_flavours").Field("flavour_id").Unique().Required(),
	}
}

func (DrinkFlavour) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("drink_id", "flavour_id").Unique(),
	}
}

// DrinkAlias is an alternative searchable name for a drink.
type DrinkAlias struct {
	ent.Schema
}

func (DrinkAlias) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Int("drink_id"),
		field.String("alias").NotEmpty(),
	}
}

func (DrinkAlias) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("drink", Drink.Type).Ref("alias_rows").Field("drink_id").Unique().Required(),
	}
}

// DrinkImage is a catalog image with one primary image per drink.
type DrinkImage struct {
	ent.Schema
}

func (DrinkImage) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Int("drink_id"),
		field.String("url").NotEmpty(),
		field.Bool("is_primary").Default(false),
		field.String("attribution").Default(""),
		field.String("created_at").DefaultFunc(now),
	}
}

func (DrinkImage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("drink", Drink.Type).Ref("images").Field("drink_id").Unique().Required(),
	}
}

// OffProduct isolates Open Food Facts data. It is never joined into public
// catalog reads except through an explicit, attributed import.
type OffProduct struct {
	ent.Schema
}

func (OffProduct) Fields() []ent.Field {
	return []ent.Field{
		field.String("gtin14").NotEmpty().Unique(),
		field.String("payload_json").Default("{}"),
		field.String("product_name").Default(""),
		field.String("brands").Default(""),
		field.String("quantity").Default(""),
		field.String("nutriments_json").Default("{}"),
		field.String("image_front_url").Optional().Nillable(),
		field.String("license").Default("ODbL-1.0"),
		field.String("fetched_at").DefaultFunc(now),
	}
}

// CatalogRevision audits edits to shared catalog rows.
type CatalogRevision struct {
	ent.Schema
}

func (CatalogRevision) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Enum("entity").Values("drink", "brand", "style", "venue").Default("drink"),
		field.Int("entity_id"),
		field.String("user_id").Optional().Nillable(),
		field.String("action").Default("update"),
		field.String("diff_json").Default("{}"),
		field.String("created_at").DefaultFunc(now),
	}
}
