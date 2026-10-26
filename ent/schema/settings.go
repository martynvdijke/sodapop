package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// RuntimeSetting is an admin-editable runtime setting. Non-secret values live
// in Value; secret values live only in EncryptedValue so a database dump never
// contains plaintext credentials.
type RuntimeSetting struct {
	ent.Schema
}

func (RuntimeSetting) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("key").NotEmpty().Unique(),
		field.String("value").Default(""),
		field.String("encrypted_value").Default(""),
		field.Bool("is_secret").Default(false),
		field.String("updated_by").Optional().Nillable(),
		field.String("updated_at").DefaultFunc(now),
	}
}
