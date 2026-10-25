package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserIdentity links an external OIDC subject to a local user.
type UserIdentity struct {
	ent.Schema
}

func (UserIdentity) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id").Unique(),
		field.String("provider").NotEmpty(),
		field.String("subject").NotEmpty(),
		field.String("user_id").NotEmpty(),
		field.String("email").Optional().Nillable(),
		field.String("groups_json").Default("[]"),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

func (UserIdentity) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("identities").Field("user_id").Unique().Required(),
	}
}

func (UserIdentity) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider", "subject").Unique(),
	}
}

// Session is a database-backed browser session. Only the SHA-256 hash of the
// cookie value is stored.
type Session struct {
	ent.Schema
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("token_hash").NotEmpty().Unique().Immutable(),
		field.String("user_id").NotEmpty(),
		field.String("csrf_token").NotEmpty(),
		field.String("ip").Optional().Nillable(),
		field.String("user_agent").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
		field.String("last_seen_at").DefaultFunc(now),
		field.String("expires_at").NotEmpty(),
	}
}

func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("sessions").Field("user_id").Unique().Required(),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("expires_at"),
	}
}

// APIToken is a personal access token with a required scope set. Tokens are
// shown once and stored as SHA-256 hashes.
type APIToken struct {
	ent.Schema
}

func (APIToken) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").NotEmpty(),
		field.String("name").NotEmpty(),
		field.String("token_hash").NotEmpty().Unique(),
		field.String("scopes").Default("read"),
		field.String("created_at").DefaultFunc(now),
		field.String("last_used_at").Optional().Nillable(),
		field.String("revoked_at").Optional().Nillable(),
	}
}

func (APIToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("api_tokens").Field("user_id").Unique().Required(),
	}
}

// EmailVerificationToken is a single-use, hashed email verification token.
type EmailVerificationToken struct {
	ent.Schema
}

func (EmailVerificationToken) Fields() []ent.Field {
	return []ent.Field{
		field.String("token_hash").NotEmpty().Unique().Immutable(),
		field.String("user_id").NotEmpty(),
		field.String("email").NotEmpty(),
		field.String("created_at").DefaultFunc(now),
		field.String("expires_at").NotEmpty(),
		field.String("used_at").Optional().Nillable(),
	}
}

// PasswordResetToken is a single-use, hashed password reset token.
type PasswordResetToken struct {
	ent.Schema
}

func (PasswordResetToken) Fields() []ent.Field {
	return []ent.Field{
		field.String("token_hash").NotEmpty().Unique().Immutable(),
		field.String("user_id").NotEmpty(),
		field.String("created_at").DefaultFunc(now),
		field.String("expires_at").NotEmpty(),
		field.String("used_at").Optional().Nillable(),
	}
}

// EmailOutbox is the durable queue for transactional email.
type EmailOutbox struct {
	ent.Schema
}

func (EmailOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("to_email").NotEmpty(),
		field.String("template").NotEmpty(),
		field.String("payload_json").Default("{}"),
		field.Int("attempts").Default(0),
		field.String("next_attempt_at").DefaultFunc(now),
		field.String("sent_at").Optional().Nillable(),
		field.String("last_error").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
	}
}

// UserSetting holds per-user notification and theme preferences.
type UserSetting struct {
	ent.Schema
}

func (UserSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("user_id").NotEmpty().Unique(),
		field.Bool("email_friend_req").Default(true),
		field.Bool("email_weekly").Default(false),
		field.Bool("push_enabled").Default(false),
		field.Enum("theme").Values("system", "light", "dark").Default("system"),
		field.Bool("public_profile").Default(false),
		field.Enum("units").Values("metric", "imperial").Default("metric"),
		field.String("caffeine_bedtime").Default(""),
		field.String("updated_at").DefaultFunc(now),
	}
}

// LoginAttempt is a lightweight rate-limiting ledger, pruned hourly.
type LoginAttempt struct {
	ent.Schema
}

func (LoginAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("key").NotEmpty(),
		field.String("attempted_at").DefaultFunc(now),
	}
}

func (LoginAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("key", "attempted_at"),
	}
}

// AppSetting is a small key/value store for runtime feature switches.
type AppSetting struct {
	ent.Schema
}

func (AppSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").NotEmpty().Unique(),
		field.String("value").Default(""),
		field.String("updated_at").DefaultFunc(now),
	}
}
