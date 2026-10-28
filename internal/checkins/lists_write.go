package checkins

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/ent/list"
	"sodapop/ent/listitem"
	"sodapop/internal/db"
)

var (
	// ErrListInvalidInput reports a malformed list or item payload.
	ErrListInvalidInput = errors.New("invalid list input")
	// ErrListSystem reports an attempt to rename, edit or delete a built-in
	// list such as the wishlist. Items may still be added and removed.
	ErrListSystem = errors.New("built-in lists cannot be modified")
)

// ListCreateInput is the owner-supplied payload for a new list.
type ListCreateInput struct {
	Name        string
	Description string
	Visibility  string
}

// ListUpdateInput carries only the fields a PATCH should change.
type ListUpdateInput struct {
	Name        *string
	Description *string
	Visibility  *string
}

// ListItemInput identifies one drink to save, either by numeric id or slug.
type ListItemInput struct {
	DrinkID int
	Slug    string
	Note    string
}

func normalizeVisibility(v string) (string, error) {
	switch v {
	case "":
		return "private", nil
	case "public", "friends", "private":
		return v, nil
	default:
		return "", fmt.Errorf("%w: visibility must be public, friends or private", ErrListInvalidInput)
	}
}

func validateListName(name string) error {
	if n := len([]rune(strings.TrimSpace(name))); n < 1 || n > 80 {
		return fmt.Errorf("%w: name must be 1-80 characters", ErrListInvalidInput)
	}
	return nil
}

func validateDescription(description string) error {
	if len([]rune(strings.TrimSpace(description))) > 500 {
		return fmt.Errorf("%w: description must be 500 characters or fewer", ErrListInvalidInput)
	}
	return nil
}

// CreateList creates an owner-scoped list and returns its summary.
func (s *Service) CreateList(ctx context.Context, userID string, in ListCreateInput) (*ListSummary, error) {
	name := strings.TrimSpace(in.Name)
	if err := validateListName(name); err != nil {
		return nil, err
	}
	description := strings.TrimSpace(in.Description)
	if err := validateDescription(description); err != nil {
		return nil, err
	}
	vis, err := normalizeVisibility(in.Visibility)
	if err != nil {
		return nil, err
	}
	row, err := s.Client.List.Create().
		SetUserID(userID).
		SetName(name).
		SetDescription(description).
		SetVisibility(list.Visibility(vis)).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return &ListSummary{
		ID: row.ID, Name: row.Name, Description: row.Description,
		Visibility: string(row.Visibility), IsSystem: row.IsSystem, ItemCount: 0,
	}, nil
}

// UpdateList applies a partial update to a list the caller owns. Missing,
// invisible and foreign lists all return ErrNotFound.
func (s *Service) UpdateList(ctx context.Context, userID, listID string, in ListUpdateInput) (*ListSummary, error) {
	l, err := s.ownedList(ctx, userID, listID)
	if err != nil {
		return nil, err
	}
	if l.IsSystem {
		return nil, ErrListSystem
	}
	update := l.Update().SetUpdatedAt(db.Now())
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if err := validateListName(name); err != nil {
			return nil, err
		}
		update = update.SetName(name)
	}
	if in.Description != nil {
		description := strings.TrimSpace(*in.Description)
		if err := validateDescription(description); err != nil {
			return nil, err
		}
		update = update.SetDescription(description)
	}
	if in.Visibility != nil {
		vis, err := normalizeVisibility(*in.Visibility)
		if err != nil {
			return nil, err
		}
		update = update.SetVisibility(list.Visibility(vis))
	}
	saved, err := update.Save(ctx)
	if err != nil {
		return nil, err
	}
	count, err := s.Client.ListItem.Query().Where(listitem.ListID(saved.ID)).Count(ctx)
	if err != nil {
		return nil, err
	}
	return &ListSummary{
		ID: saved.ID, Name: saved.Name, Description: saved.Description,
		Visibility: string(saved.Visibility), IsSystem: saved.IsSystem, ItemCount: count,
	}, nil
}

// DeleteList removes an owned, non-system list and its items in one
// transaction.
func (s *Service) DeleteList(ctx context.Context, userID, listID string) error {
	l, err := s.ownedList(ctx, userID, listID)
	if err != nil {
		return err
	}
	if l.IsSystem {
		return ErrListSystem
	}
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.ListItem.Delete().Where(listitem.ListID(l.ID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.List.DeleteOneID(l.ID).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// AddListItem saves a drink to an owned list. Adding a drink that is already
// present returns the existing item with created=false. System lists accept
// items even though they cannot be renamed or deleted.
func (s *Service) AddListItem(ctx context.Context, userID, listID string, in ListItemInput) (*ListItem, bool, error) {
	l, err := s.ownedList(ctx, userID, listID)
	if err != nil {
		return nil, false, err
	}
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > 200 {
		return nil, false, fmt.Errorf("%w: note must be 200 characters or fewer", ErrListInvalidInput)
	}
	d, err := s.resolveListDrink(ctx, in)
	if err != nil {
		return nil, false, err
	}

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	existing, err := tx.ListItem.Query().
		Where(listitem.ListID(l.ID), listitem.DrinkID(d.ID)).
		Only(ctx)
	if err == nil {
		_ = tx.Rollback()
		return listItemFrom(d, existing), false, nil
	}
	if !ent.IsNotFound(err) {
		_ = tx.Rollback()
		return nil, false, err
	}
	row, err := tx.ListItem.Create().
		SetListID(l.ID).
		SetDrinkID(d.ID).
		SetNote(note).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	if _, err := tx.List.UpdateOneID(l.ID).SetUpdatedAt(db.Now()).Save(ctx); err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return listItemFrom(d, row), true, nil
}

// RemoveListItem removes a drink from an owned list. Removing an absent drink
// is a no-op so the endpoint stays idempotent.
func (s *Service) RemoveListItem(ctx context.Context, userID, listID string, drinkID int) error {
	l, err := s.ownedList(ctx, userID, listID)
	if err != nil {
		return err
	}
	if drinkID <= 0 {
		return fmt.Errorf("%w: invalid drink id", ErrListInvalidInput)
	}
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.ListItem.Delete().
		Where(listitem.ListID(l.ID), listitem.DrinkID(drinkID)).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.List.UpdateOneID(l.ID).SetUpdatedAt(db.Now()).Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ownedList loads a list only when userID owns it. Any other case is reported
// as missing so an API caller cannot probe for another user's lists.
func (s *Service) ownedList(ctx context.Context, userID, listID string) (*ent.List, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return nil, ErrNotFound
	}
	l, err := s.Client.List.Query().
		Where(list.ID(listID), list.UserID(userID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return l, nil
}

func (s *Service) resolveListDrink(ctx context.Context, in ListItemInput) (*ent.Drink, error) {
	q := s.Client.Drink.Query().
		Where(drink.StatusEQ(drink.StatusActive)).
		WithBrand()
	switch {
	case in.DrinkID > 0:
		q = q.Where(drink.ID(in.DrinkID))
	case strings.TrimSpace(in.Slug) != "":
		q = q.Where(drink.Slug(strings.TrimSpace(in.Slug)))
	default:
		return nil, fmt.Errorf("%w: drink_id or slug is required", ErrListInvalidInput)
	}
	d, err := q.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return d, nil
}

func listItemFrom(d *ent.Drink, row *ent.ListItem) *ListItem {
	brand := ""
	if d.Edges.Brand != nil {
		brand = d.Edges.Brand.Name
	}
	return &ListItem{
		DrinkID: d.ID, Slug: d.Slug, Name: d.Name, Brand: brand,
		Note: row.Note, CreatedAt: row.AddedAt,
	}
}
