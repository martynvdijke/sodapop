package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Checkin is one drink log. Ratings are stored as integer quarter-stars
// (0..20) so 4.25 is stored as 17.
type Checkin struct {
	ent.Schema
}

func (Checkin) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").NotEmpty(),
		field.Int("drink_id"),
		field.Int("venue_id").Optional().Nillable(),
		field.Int("rating").Optional().Nillable().Range(0, 20),
		field.Enum("serving").Values("can", "bottle", "glass", "fountain", "other").Default("can"),
		field.String("note").Default(""),
		field.String("photo_media_id").Optional().Nillable(),
		field.Enum("visibility").Values("public", "friends", "private").Default("public"),
		field.String("idempotency_key").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
		field.String("deleted_at").Optional().Nillable(),
	}
}

func (Checkin) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("checkins").Field("user_id").Unique().Required(),
		edge.From("drink", Drink.Type).Ref("checkins").Field("drink_id").Unique().Required(),
		edge.From("venue", Venue.Type).Ref("checkins").Field("venue_id").Unique(),
	}
}

func (Checkin) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "created_at"),
		index.Fields("drink_id"),
		index.Fields("idempotency_key").Unique(),
	}
}

// CheckinLike is a "toast" on a check-in.
type CheckinLike struct {
	ent.Schema
}

func (CheckinLike) Fields() []ent.Field {
	return []ent.Field{
		field.String("checkin_id").NotEmpty(),
		field.String("user_id").NotEmpty(),
		field.String("created_at").DefaultFunc(now),
	}
}

func (CheckinLike) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("checkin_id", "user_id").Unique(),
	}
}
