package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Badge is a catalog badge definition.
type Badge struct {
	ent.Schema
}

func (Badge) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("slug").NotEmpty().Unique(),
		field.String("name").NotEmpty(),
		field.String("description").Default(""),
		field.Enum("kind").Values("core", "local", "special").Default("core"),
		field.String("rule").Default(""),
		field.String("created_at").DefaultFunc(now),
	}
}

// UserBadge is an awarded badge.
type UserBadge struct {
	ent.Schema
}

func (UserBadge) Fields() []ent.Field {
	return []ent.Field{
		field.String("user_id").NotEmpty(),
		field.Int("badge_id"),
		field.Int("level").Default(1),
		field.String("awarded_at").DefaultFunc(now),
	}
}

func (UserBadge) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "badge_id").Unique(),
	}
}

// List is a user-curated collection. The system wishlist has is_system=true.
type List struct {
	ent.Schema
}

func (List) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").NotEmpty(),
		field.String("name").NotEmpty(),
		field.String("slug").Default(""),
		field.String("description").Default(""),
		field.Enum("visibility").Values("public", "friends", "private").Default("public"),
		field.Bool("is_system").Default(false),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

func (List) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("lists").Field("user_id").Unique().Required(),
		edge.To("items", ListItem.Type),
	}
}

// ListItem is a drink saved to a list.
type ListItem struct {
	ent.Schema
}

func (ListItem) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("list_id").NotEmpty(),
		field.Int("drink_id"),
		field.String("note").Default(""),
		field.String("added_at").DefaultFunc(now),
	}
}

func (ListItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("list", List.Type).Ref("items").Field("list_id").Unique().Required(),
	}
}

func (ListItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("list_id", "drink_id").Unique(),
	}
}
