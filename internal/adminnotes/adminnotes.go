// Package adminnotes stores short internal moderation notes. Notes are
// immutable except for deletion: there is deliberately no soft-delete column,
// so an admin note cannot be silently edited after the fact.
package adminnotes

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"sodapop/ent"
	"sodapop/ent/adminnote"
)

const (
	// MaxBodyRunes is the user-facing body limit for one note.
	MaxBodyRunes = 2000
	// MaxList is a hard cap on the notes returned for one entity.
	MaxList = 200
)

var (
	ErrInvalid   = errors.New("invalid admin note")
	ErrNotFound  = errors.New("admin note not found")
	ErrForbidden = errors.New("admin note forbidden")
)

var entities = map[string]adminnote.Entity{
	"drink":   adminnote.EntityDrink,
	"brand":   adminnote.EntityBrand,
	"venue":   adminnote.EntityVenue,
	"checkin": adminnote.EntityCheckin,
	"user":    adminnote.EntityUser,
	"system":  adminnote.EntitySystem,
}

// Service persists admin notes against an existing entity row.
type Service struct {
	Client *ent.Client
}

func New(client *ent.Client) *Service {
	return &Service{Client: client}
}

// Create validates and stores a note. Callers are responsible for checking
// that authorID belongs to an admin or moderator before calling.
func (s *Service) Create(ctx context.Context, authorID, entity string, entityID int, body string) (*ent.AdminNote, error) {
	authorID = strings.TrimSpace(authorID)
	kind, ok := entities[entity]
	if authorID == "" || !ok || entityID <= 0 {
		return nil, ErrInvalid
	}
	body = strings.TrimSpace(body)
	n := utf8.RuneCountInString(body)
	if n == 0 || n > MaxBodyRunes {
		return nil, ErrInvalid
	}
	row, err := s.Client.AdminNote.Create().
		SetAuthorID(authorID).
		SetEntity(kind).
		SetEntityID(entityID).
		SetBody(body).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return row, nil
}

// List returns an entity's notes oldest-first. It returns ErrInvalid for an
// unknown entity so a caller cannot accidentally dump every note.
func (s *Service) List(ctx context.Context, entity string, entityID int) ([]*ent.AdminNote, error) {
	kind, ok := entities[entity]
	if !ok || entityID <= 0 {
		return nil, ErrInvalid
	}
	return s.Client.AdminNote.Query().
		Where(adminnote.EntityEQ(kind), adminnote.EntityIDEQ(entityID)).
		Order(adminnote.ByCreatedAt(), adminnote.ByID()).
		Limit(MaxList).
		All(ctx)
}

// Delete removes a note. Admins may delete any note; non-admins may only
// delete their own. There is no soft delete for internal notes.
func (s *Service) Delete(ctx context.Context, authorID string, id int, admin bool) error {
	row, err := s.Client.AdminNote.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if !admin && row.AuthorID != authorID {
		return ErrForbidden
	}
	if err := s.Client.AdminNote.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
