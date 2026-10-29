package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PushDevice is one native Android push registration. A token belongs to exactly
// one user; re-registering the same token refreshes ownership and last_seen_at.
type PushDevice struct {
	ent.Schema
}

func (PushDevice) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").NotEmpty(),
		field.String("token").NotEmpty().Unique(),
		field.Enum("platform").Values("android"),
		field.String("created_at").DefaultFunc(now),
		field.String("last_seen_at").DefaultFunc(now),
	}
}

func (PushDevice) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
	}
}
