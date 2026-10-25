package integrations

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"sodapop/ent"
	"sodapop/ent/inboundtoken"
	"sodapop/ent/user"
	"sodapop/internal/auth"
)

// ErrInvalidInboundToken is deliberately generic: callers must not be able to
// tell a revoked, unknown or malformed token apart.
var ErrInvalidInboundToken = errors.New("invalid inbound token")

// CreateInboundToken mints a new machine token for a user. The plaintext is
// returned exactly once; only its SHA-256 hash is stored. Creating a token
// revokes any earlier active token of the same kind for that user.
func (s *Service) CreateInboundToken(ctx context.Context, userID, label string) (string, *ent.InboundToken, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Home Assistant"
	}
	label = truncateRunes(label, 60)

	raw := auth.RandomToken(32)
	stamp := now()

	if _, err := s.Client.InboundToken.Update().
		Where(
			inboundtoken.UserID(userID),
			inboundtoken.KindEQ(inboundtoken.KindHomeAssistant),
			inboundtoken.RevokedAtIsNil(),
		).
		SetRevokedAt(stamp).
		Save(ctx); err != nil {
		return "", nil, err
	}

	tok, err := s.Client.InboundToken.Create().
		SetUserID(userID).
		SetKind(inboundtoken.KindHomeAssistant).
		SetLabel(label).
		SetTokenHash(auth.HashToken(raw)).
		SetCreatedAt(stamp).
		Save(ctx)
	if err != nil {
		return "", nil, err
	}
	return raw, tok, nil
}

// ListInboundTokens returns every token a user has created, newest first.
// Hashes are never exposed outside this package.
func (s *Service) ListInboundTokens(ctx context.Context, userID string) ([]*ent.InboundToken, error) {
	return s.Client.InboundToken.Query().
		Where(inboundtoken.UserID(userID)).
		Order(ent.Desc(inboundtoken.FieldCreatedAt)).
		All(ctx)
}

// RevokeInboundToken revokes one token owned by userID. A token belonging to
// somebody else is not found.
func (s *Service) RevokeInboundToken(ctx context.Context, userID, tokenID string) error {
	n, err := s.Client.InboundToken.Update().
		Where(
			inboundtoken.ID(tokenID),
			inboundtoken.UserID(userID),
			inboundtoken.RevokedAtIsNil(),
		).
		SetRevokedAt(now()).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalidInboundToken
	}
	return nil
}

// AuthenticateInbound resolves a plaintext token to its row and records the
// use. Revoked and unknown tokens both return ErrInvalidInboundToken.
func (s *Service) AuthenticateInbound(ctx context.Context, raw string) (*ent.InboundToken, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalidInboundToken
	}
	tok, err := s.Client.InboundToken.Query().
		Where(
			inboundtoken.TokenHash(auth.HashToken(raw)),
			inboundtoken.RevokedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrInvalidInboundToken
		}
		return nil, err
	}
	owner, err := s.Client.User.Get(ctx, tok.UserID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrInvalidInboundToken
		}
		return nil, err
	}
	if owner.DeletedAt != nil || owner.Status != user.StatusActive {
		return nil, ErrInvalidInboundToken
	}
	stamp := now()
	updated, err := tok.Update().SetLastUsedAt(stamp).Save(ctx)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func truncateRunes(v string, max int) string {
	if utf8.RuneCountInString(v) <= max {
		return v
	}
	runes := []rune(v)
	return string(runes[:max])
}
