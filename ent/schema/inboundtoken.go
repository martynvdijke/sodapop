package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// InboundToken authorises machine write access (for example a Home Assistant
// automation). Only the SHA-256 hash of the token is stored.
type InboundToken struct{ ent.Schema }

func (InboundToken) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").NotEmpty(),
		field.Enum("kind").Values("home_assistant").Default("home_assistant"),
		field.String("label").Default(""),
		field.String("token_hash").NotEmpty().Unique().Immutable(),
		field.String("last_used_at").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
		field.String("revoked_at").Optional().Nillable(),
	}
}

func (InboundToken) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id")}
}
