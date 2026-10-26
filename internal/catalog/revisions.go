package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"sodapop/ent"
	"sodapop/ent/catalogrevision"
	"sodapop/ent/drink"
	"sodapop/internal/db"
)

// MaxRevisionDiffBytes bounds the stored JSON diff. Catalog rows are text
// light, so an oversized diff always means something unexpected (or hostile)
// and is summarised rather than persisted verbatim.
const MaxRevisionDiffBytes = 8 * 1024

// ErrRevisionEntity is returned when a caller names an entity type the
// CatalogRevision enum cannot represent.
var ErrRevisionEntity = errors.New("unsupported catalog revision entity")

var revisionEntities = map[string]catalogrevision.Entity{
	"drink": catalogrevision.EntityDrink,
	"brand": catalogrevision.EntityBrand,
	"style": catalogrevision.EntityStyle,
	"venue": catalogrevision.EntityVenue,
}

// RecordRevision writes an audit row for a shared catalog mutation. It is a
// no-op when nothing changed. Callers that already hold an Ent transaction
// should use recordRevisionTx so the mutation and audit row commit together.
func (s *Service) RecordRevision(ctx context.Context, entity string, entityID int, userID *string, action string, before, after any) error {
	return recordRevision(ctx, func() *ent.CatalogRevisionCreate {
		return s.Client.CatalogRevision.Create()
	}, entity, entityID, userID, action, before, after)
}

func recordRevisionTx(ctx context.Context, tx *ent.Tx, entity string, entityID int, userID *string, action string, before, after any) error {
	return recordRevision(ctx, func() *ent.CatalogRevisionCreate {
		return tx.CatalogRevision.Create()
	}, entity, entityID, userID, action, before, after)
}

func recordRevision(ctx context.Context, create func() *ent.CatalogRevisionCreate, entity string, entityID int, userID *string, action string, before, after any) error {
	kind, ok := revisionEntities[entity]
	if !ok {
		return ErrRevisionEntity
	}
	if entityID <= 0 {
		return errors.New("catalog revision entity id must be positive")
	}
	diff, err := revisionDiff(before, after)
	if err != nil {
		return err
	}
	if string(diff) == "{}" {
		return nil
	}
	b := create().
		SetEntity(kind).
		SetEntityID(entityID).
		SetAction(truncateRevisionText(action, 80)).
		SetDiffJSON(string(diff))
	if userID != nil && *userID != "" {
		b.SetUserID(*userID)
	}
	_, err = b.Save(ctx)
	return err
}

// revisionDiff computes a field-level diff of two JSON-compatible values.
// Unchanged fields are omitted; a field that appears on only one side is
// tagged with before/after. Secret-looking keys are dropped before anything
// is stored.
func revisionDiff(before, after any) ([]byte, error) {
	beforeMap, err := revisionObject(before)
	if err != nil {
		return nil, err
	}
	afterMap, err := revisionObject(after)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(beforeMap)+len(afterMap))
	seen := map[string]bool{}
	for k := range beforeMap {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range afterMap {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	diff := map[string]any{}
	for _, k := range keys {
		bv, bok := beforeMap[k]
		av, aok := afterMap[k]
		if bok && aok && reflect.DeepEqual(bv, av) {
			continue
		}
		entry := map[string]any{}
		if bok {
			entry["before"] = bv
		}
		if aok {
			entry["after"] = av
		}
		diff[k] = entry
	}

	data, err := json.Marshal(diff)
	if err != nil {
		return nil, err
	}
	if len(data) <= MaxRevisionDiffBytes {
		return data, nil
	}
	return boundedRevisionSummary(diff, len(data)), nil
}

func revisionObject(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		// Non-object values (strings, numbers, slices) are wrapped so the diff
		// shape stays predictable.
		var scalar any
		if err := json.Unmarshal(raw, &scalar); err != nil {
			return nil, err
		}
		out = map[string]any{"value": scalar}
	}
	return sanitizeRevisionMap(out), nil
}

var revisionSecretKeys = []string{
	"password", "passwd", "secret", "token", "api_key", "apikey",
	"authorization", "cookie", "hash", "private_key",
}

// sanitizeRevisionMap strips secret-looking fields recursively so an audit row
// never becomes a credential store.
func sanitizeRevisionMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if revisionKeyIsSecret(k) {
			continue
		}
		out[k] = sanitizeRevisionValue(v)
	}
	return out
}

func sanitizeRevisionValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return sanitizeRevisionMap(val)
	case []any:
		out := make([]any, len(val))
		for i := range val {
			out[i] = sanitizeRevisionValue(val[i])
		}
		return out
	default:
		return v
	}
}

func revisionKeyIsSecret(key string) bool {
	lower := strings.ToLower(key)
	for _, needle := range revisionSecretKeys {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// boundedRevisionSummary replaces an oversized diff with a stable description
// of what changed. It never grows past MaxRevisionDiffBytes.
func boundedRevisionSummary(diff map[string]any, originalBytes int) []byte {
	keys := make([]string, 0, len(diff))
	for k := range diff {
		keys = append(keys, truncateRevisionText(k, 64))
	}
	sort.Strings(keys)
	if len(keys) > 40 {
		keys = keys[:40]
	}
	summary := map[string]any{
		"truncated":     true,
		"original_size": originalBytes,
		"fields":        keys,
	}
	for {
		out, err := json.Marshal(summary)
		if err != nil || len(out) <= MaxRevisionDiffBytes || len(keys) == 0 {
			if err != nil {
				return []byte(`{"truncated":true}`)
			}
			return out
		}
		keys = keys[:len(keys)/2]
		summary["fields"] = keys
	}
}

func truncateRevisionText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ActivateDrink marks a provisional catalog row active and records who did it
// in the same transaction. It replaces the plain status update used by the
// admin queue so activation cannot succeed without its audit trail.
func (s *Service) ActivateDrink(ctx context.Context, userID *string, drinkID int) (*ent.Drink, error) {
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	d, err := tx.Drink.Get(ctx, drinkID)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	before := map[string]any{"status": string(d.Status)}
	d, err = tx.Drink.UpdateOneID(drinkID).SetStatus(drink.StatusActive).Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := recordRevisionTx(ctx, tx, "drink", drinkID, userID, "activate", before, map[string]any{"status": string(d.Status)}); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d, nil
}

// SetDrinkStatus moves a drink between the admin lifecycle statuses and records
// who changed it in the same transaction. It mirrors ActivateDrink so a status
// change can never commit without its audit row. A request that leaves status
// and limited_until untouched is a no-op and writes no revision.
func (s *Service) SetDrinkStatus(ctx context.Context, userID *string, drinkID int, status string, limitedUntil string) error {
	if drinkID <= 0 {
		return ErrNotFound
	}
	next := drink.Status(strings.TrimSpace(status))
	switch next {
	case drink.StatusActive, drink.StatusLimited, drink.StatusSeasonal, drink.StatusDiscontinued:
	default:
		return errors.New("invalid drink status")
	}
	until := strings.TrimSpace(limitedUntil)
	if next != drink.StatusLimited && next != drink.StatusSeasonal {
		until = ""
	}

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	d, err := tx.Drink.Get(ctx, drinkID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	currentUntil := ""
	if d.LimitedUntil != nil {
		currentUntil = *d.LimitedUntil
	}
	if d.Status == next && currentUntil == until {
		_ = tx.Rollback()
		return nil
	}

	before := map[string]any{
		"status":        string(d.Status),
		"limited_until": currentUntil,
	}
	stamp := db.Now()
	update := tx.Drink.UpdateOneID(drinkID).
		SetStatus(next).
		SetUpdatedAt(stamp)
	if next == drink.StatusDiscontinued {
		update.SetDiscontinuedAt(stamp).ClearLimitedUntil()
	} else {
		update.ClearDiscontinuedAt()
		if until != "" {
			update.SetLimitedUntil(until)
		} else {
			update.ClearLimitedUntil()
		}
	}
	updated, err := update.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	afterUntil := ""
	if updated.LimitedUntil != nil {
		afterUntil = *updated.LimitedUntil
	}
	after := map[string]any{
		"status":        string(updated.Status),
		"limited_until": afterUntil,
	}
	if err := recordRevisionTx(ctx, tx, "drink", drinkID, userID, "status", before, after); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
