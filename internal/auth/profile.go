package auth

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"sodapop/ent"
	"sodapop/ent/session"
	"sodapop/internal/db"
)

var (
	// ErrInvalidDisplayName is returned when a display name is empty or too long.
	ErrInvalidDisplayName = errors.New("display name must be between 1 and 60 characters")
	// ErrInvalidBio is returned when a bio exceeds the length cap.
	ErrInvalidBio = errors.New("bio must be 300 characters or fewer")
	// ErrWeakPassword is returned when a new password is too short.
	ErrWeakPassword = errors.New("password must be at least 8 characters")
	// ErrNoPassword is returned when a password change is attempted on an
	// account that signs in through OIDC only.
	ErrNoPassword = errors.New("this account does not use a password")
	// ErrInvalidMedia is returned when an avatar does not belong to the user or
	// is not an avatar upload.
	ErrInvalidMedia = errors.New("invalid avatar")
)

const (
	maxDisplayName = 60
	maxBio         = 300
	minPassword    = 10
)

// UpdateProfile stores the editable profile fields after trimming whitespace.
func (s *Service) UpdateProfile(ctx context.Context, userID, displayName, bio string) error {
	displayName = strings.TrimSpace(displayName)
	bio = strings.TrimSpace(bio)
	if n := utf8.RuneCountInString(displayName); n < 1 || n > maxDisplayName {
		return ErrInvalidDisplayName
	}
	if utf8.RuneCountInString(bio) > maxBio {
		return ErrInvalidBio
	}
	return s.Client.User.UpdateOneID(userID).
		SetDisplayName(displayName).
		SetBio(bio).
		SetUpdatedAt(db.Now()).
		Exec(ctx)
}

// SetAvatar points the user's profile at one of their own avatar uploads.
func (s *Service) SetAvatar(ctx context.Context, userID, mediaID string) error {
	if mediaID == "" {
		return ErrInvalidMedia
	}
	row, err := s.Client.Media.Get(ctx, mediaID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrInvalidMedia
		}
		return err
	}
	if row.Kind != "avatar" || row.UserID == nil || *row.UserID != userID {
		return ErrInvalidMedia
	}
	return s.Client.User.UpdateOneID(userID).
		SetAvatarMediaID(mediaID).
		SetUpdatedAt(db.Now()).
		Exec(ctx)
}

// ChangePassword verifies the current password, stores a new one, and revokes
// every other browser session. The session identified by currentSessionID is
// kept so the caller stays signed in; pass "" to revoke all of them.
func (s *Service) ChangePassword(ctx context.Context, userID, current, next, currentSessionID string) error {
	if utf8.RuneCountInString(next) < minPassword {
		return ErrWeakPassword
	}
	u, err := s.Client.User.Get(ctx, userID)
	if err != nil {
		return err
	}
	if u.PasswordHash == nil || *u.PasswordHash == "" {
		return ErrNoPassword
	}
	if !CheckPassword(*u.PasswordHash, current) {
		return ErrInvalidCredentials
	}
	hash, err := s.HashPassword(next)
	if err != nil {
		return err
	}
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.User.UpdateOneID(userID).
		SetPasswordHash(hash).
		SetUpdatedAt(db.Now()).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	q := tx.Session.Delete().Where(session.UserID(userID))
	if id, ok := sessionIDInt(currentSessionID); ok {
		q = q.Where(session.IDNEQ(id))
	}
	if _, err := q.Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// HasPassword reports whether the user can sign in with a local password.
func HasPassword(u *ent.User) bool {
	return u != nil && u.PasswordHash != nil && *u.PasswordHash != ""
}
