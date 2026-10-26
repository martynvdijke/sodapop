package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Follow is a one-way follow.
type Follow struct {
	ent.Schema
}

func (Follow) Fields() []ent.Field {
	return []ent.Field{
		field.String("follower_id").NotEmpty(),
		field.String("followee_id").NotEmpty(),
		field.String("created_at").DefaultFunc(now),
	}
}

func (Follow) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("follower_id", "followee_id").Unique(),
	}
}

// Friendship is a mutual relationship. Rows are stored canonically with
// user_a < user_b.
type Friendship struct {
	ent.Schema
}

func (Friendship) Fields() []ent.Field {
	return []ent.Field{
		field.String("user_a").NotEmpty(),
		field.String("user_b").NotEmpty(),
		field.Enum("status").Values("pending", "accepted", "declined").Default("pending"),
		field.String("requested_by").NotEmpty(),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

func (Friendship) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_a", "user_b").Unique(),
	}
}

// Block prevents interactions and feed visibility between two users.
type Block struct {
	ent.Schema
}

func (Block) Fields() []ent.Field {
	return []ent.Field{
		field.String("blocker_id").NotEmpty(),
		field.String("blocked_id").NotEmpty(),
		field.String("created_at").DefaultFunc(now),
	}
}

func (Block) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("blocker_id", "blocked_id").Unique(),
	}
}

// Activity is the fan-out source for the friends feed.
type Activity struct {
	ent.Schema
}

func (Activity) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").NotEmpty(),
		field.Enum("kind").Values("checkin", "badge", "list", "venue", "friend").Default("checkin"),
		field.String("subject_id").Default(""),
		field.Enum("visibility").Values("public", "friends", "private").Default("public"),
		field.String("created_at").DefaultFunc(now),
	}
}

func (Activity) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "created_at"),
		index.Fields("created_at"),
	}
}

// Notification is an in-app notification for a user.
type Notification struct {
	ent.Schema
}

func (Notification) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").NotEmpty(),
		field.Enum("kind").Values("friend_request", "friend_accept", "toast", "comment", "badge", "system").Default("system"),
		field.String("actor_id").Optional().Nillable(),
		field.String("subject_id").Default(""),
		field.String("text").Default(""),
		field.String("read_at").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
	}
}

func (Notification) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "created_at"),
	}
}

// Comment is a flat, user-owned comment on a check-in. Threading is not
// modelled; deleted comments are soft-deleted via deleted_at.
type Comment struct {
	ent.Schema
}

func (Comment) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("checkin_id").NotEmpty(),
		field.String("user_id").NotEmpty(),
		field.String("body").NotEmpty().MaxLen(1000),
		field.String("created_at").DefaultFunc(now),
		field.String("deleted_at").Optional().Nillable(),
	}
}
