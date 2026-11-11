package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SavedSearch is a member's standing catalog query. It is evaluated against
// drinks that enter the catalog or move into a limited/seasonal lifecycle, and
// a match produces one drop_alert notification. The filter fields mirror the
// browse-page filters so the web form and the API accept the same vocabulary;
// empty values mean "any".
type SavedSearch struct {
	ent.Schema
}

func (SavedSearch) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(newID).Immutable(),
		field.String("user_id").NotEmpty(),
		field.String("name").Default(""),
		field.String("query").Default(""),
		field.String("beverage_type").Default(""),
		field.String("style_slug").Default(""),
		field.String("brand_slug").Default(""),
		field.String("flavour_slug").Default(""),
		field.Bool("caffeine_free").Default(false),
		field.Bool("sugar_free").Default(false),
		field.Bool("notify_drops").Default(true),
		// last_notified_at debounces alerts so one drink in one season does not
		// notify the same member twice.
		field.String("last_notified_at").Optional().Nillable(),
		field.String("created_at").DefaultFunc(now),
		field.String("updated_at").DefaultFunc(now),
	}
}

func (SavedSearch) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "created_at"),
	}
}
