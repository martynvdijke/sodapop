package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Integration is a per-user outbound integration target. Secrets are stored
// encrypted where the integration needs a token.
type Integration struct {
	ent.Schema
}

func (Integration) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("user_id").NotEmpty(),
		field.Enum("kind").Values("home_assistant", "matrix", "trmnl").Default("home_assistant"),
		field.String("target").Default(""),
		field.String("secret").Default(""),
		field.Bool("enabled").Default(true),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

func (Integration) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "kind").Unique(),
	}
}

// PushSubscription is a browser Web Push registration.
type PushSubscription struct {
	ent.Schema
}

func (PushSubscription) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("user_id").NotEmpty(),
		field.String("endpoint").NotEmpty().Unique(),
		field.String("p256dh").NotEmpty(),
		field.String("auth").NotEmpty(),
		field.String("created_at").DefaultFunc(now),
	}
}

func (PushSubscription) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
	}
}
