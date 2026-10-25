package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// User is a Sodapop account. Accounts can be created through Authelia OIDC or
// with a local password; local accounts stay read-only until their email is
// verified.
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("handle").NotEmpty().Unique(),
		field.String("email").NotEmpty().Unique(),
		field.String("email_verified_at").Optional().Nillable(),
		field.String("password_hash").Optional().Nillable().Sensitive(),
		field.String("display_name").Default(""),
		field.String("bio").Default(""),
		field.String("avatar_media_id").Optional().Nillable(),
		field.Enum("role").Values("user", "moderator", "admin").Default("user"),
		field.Enum("status").Values("active", "suspended", "deleted").Default("active"),
		field.Enum("default_visibility").Values("public", "friends", "private").Default("public"),
		field.Bool("analytics_opt_out").Default(false),
		field.String("country").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
		field.String("deleted_at").Optional().Nillable(),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("sessions", Session.Type),
		edge.To("api_tokens", APIToken.Type),
		edge.To("identities", UserIdentity.Type),
		edge.To("checkins", Checkin.Type),
		edge.To("lists", List.Type),
	}
}
