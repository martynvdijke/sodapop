package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Report is a user-submitted moderation report.
type Report struct {
	ent.Schema
}

func (Report) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Enum("entity").Values("drink", "brand", "venue", "checkin", "user").Default("drink"),
		field.Int("entity_id"),
		field.String("user_id").Optional().Nillable(),
		field.String("reason").Default("other"),
		field.String("details").Default(""),
		field.Enum("status").Values("open", "resolved", "dismissed").Default("open"),
		field.String("created_at").DefaultFunc(now),
		field.String("resolved_at").Optional().Nillable(),
	}
}

func (Report) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at"),
	}
}

// AdminNote is an internal audit note.
type AdminNote struct {
	ent.Schema
}

func (AdminNote) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("author_id").NotEmpty(),
		field.Enum("entity").Values("drink", "brand", "venue", "checkin", "user", "system").Default("system"),
		field.Int("entity_id").Default(0),
		field.String("body").NotEmpty(),
		field.String("created_at").DefaultFunc(now),
	}
}

// AIEndpoint is an admin-configured OpenAI-compatible provider.
type AIEndpoint struct {
	ent.Schema
}

func (AIEndpoint) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("name").NotEmpty().Unique(),
		field.Enum("type").Values("text", "image", "vision").Default("text"),
		field.String("base_url").NotEmpty(),
		field.String("encrypted_api_key").Default(""),
		field.String("model").NotEmpty(),
		field.String("tags").Default("[]"),
		field.Bool("enabled").Default(true),
		field.Float("temperature").Optional().Nillable(),
		field.Int("max_tokens").Optional().Nillable(),
		field.String("image_size").Default(""),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

// AIDraftSession stores a multi-turn drafting conversation.
type AIDraftSession struct {
	ent.Schema
}

func (AIDraftSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").NotEmpty(),
		field.Enum("kind").Values("drink", "tasting_note", "venue").Default("drink"),
		field.String("messages_json").Default("[]"),
		field.String("draft_json").Default("{}"),
		field.Enum("status").Values("chatting", "ready", "committed").Default("chatting"),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

// Media is an uploaded image row, referenced by check-ins, avatars and drinks.
type Media struct {
	ent.Schema
}

func (Media) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").Optional().Nillable(),
		field.Enum("kind").Values("checkin", "avatar", "drink", "venue", "brand", "video").Default("checkin"),
		field.String("path").NotEmpty(),
		field.String("thumb_path").Default(""),
		field.String("poster_path").Default(""),
		field.String("mime").NotEmpty(),
		field.Int("duration_seconds").Default(0),
		field.Int("width").Default(0),
		field.Int("height").Default(0),
		field.Int("bytes").Default(0),
		field.String("created_at").DefaultFunc(now),
	}
}
