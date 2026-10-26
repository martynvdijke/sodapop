package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Venue is a place where soda is bought or drunk, including hotels and vending
// machines.
type Venue struct {
	ent.Schema
}

func (Venue) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("name").NotEmpty(),
		field.String("slug").NotEmpty().Unique(),
		field.Enum("kind").Values("bar", "restaurant", "cafe", "shop", "hotel", "festival", "vending", "other").Default("other"),
		field.String("address").Default(""),
		field.String("city").Default(""),
		field.String("region").Default(""),
		field.String("country").Default(""),
		field.Float("lat").Default(0),
		field.Float("lng").Default(0),
		field.String("website").Optional().Nillable(),
		field.String("created_by").Optional().Nillable(),
		field.Int("merged_into_id").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

func (Venue) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("names", VenueName.Type),
		edge.To("drinks", VenueDrink.Type),
		edge.To("checkins", Checkin.Type),
	}
}

// VenueName records aliases and historical names used for deduplication.
type VenueName struct {
	ent.Schema
}

func (VenueName) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Int("venue_id"),
		field.String("name").NotEmpty(),
	}
}

func (VenueName) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("venue", Venue.Type).Ref("names").Field("venue_id").Unique().Required(),
	}
}

// VenueDrink marks a drink as seen at a venue ("on offer").
type VenueDrink struct {
	ent.Schema
}

func (VenueDrink) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Int("venue_id"),
		field.Int("drink_id"),
		field.String("last_seen_at").DefaultFunc(now),
		field.Bool("on_offer").Default(false),
	}
}

func (VenueDrink) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("venue", Venue.Type).Ref("drinks").Field("venue_id").Unique().Required(),
	}
}

// VenueSave is a user's saved venue.
type VenueSave struct {
	ent.Schema
}

func (VenueSave) Fields() []ent.Field {
	return []ent.Field{
		field.String("user_id").NotEmpty(),
		field.Int("venue_id"),
		field.String("created_at").DefaultFunc(now),
	}
}

func (VenueSave) Indexes() []ent.Index {
	return []ent.Index{}
}

// VenueMedia is a user-contributed photo or menu for a venue.
type VenueMedia struct {
	ent.Schema
}

func (VenueMedia) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.Int("venue_id"),
		field.String("media_id").NotEmpty(),
		field.Enum("kind").Values("photo", "menu").Default("photo"),
		field.String("label").Default(""),
		field.String("uploaded_by").NotEmpty(),
		field.String("created_at").DefaultFunc(now),
		field.String("deleted_at").Optional().Nillable(),
	}
}
