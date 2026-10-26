package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// DeliveryOutbox is the durable queue for outbound integration deliveries.
// Target holds an integration id, token or subscription id, never a secret.
type DeliveryOutbox struct {
	ent.Schema
}

func (DeliveryOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("user_id").Default(""),
		field.Enum("kind").Values("webhook", "matrix", "trmnl", "push"),
		field.String("target").Default(""),
		field.String("payload_json").Default(""),
		field.Int("attempts").Default(0),
		field.String("next_attempt_at").NotEmpty(),
		field.String("last_error").Default(""),
		field.String("delivered_at").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
	}
}

func (DeliveryOutbox) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "delivery_outbox"},
	}
}

// SchedulerState tracks the last run and lease of a periodic scheduler.
type SchedulerState struct {
	ent.Schema
}

func (SchedulerState) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("name").NotEmpty().Unique(),
		field.String("last_run_at").Optional().Nillable(),
		field.String("locked_until").Optional().Nillable(),
		field.String("lock_owner").Default(""),
		field.String("updated_at").DefaultFunc(now),
	}
}

func (SchedulerState) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "scheduler_state"},
	}
}
