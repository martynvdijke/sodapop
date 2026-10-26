package auth

import (
	"context"
	"errors"
	"net/mail"
	"regexp"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/db"
)

var (
	// ErrInvalidHandle is returned when a handle does not match the public
	// registration rules.
	ErrInvalidHandle = errors.New("invalid handle")
	// ErrInvalidEmail is returned when an email address cannot be parsed.
	ErrInvalidEmail = errors.New("invalid email")
	// ErrInvalidPassword is returned when a password is shorter than the
	// registration minimum.
	ErrInvalidPassword = errors.New("invalid password")
)

var handlePattern = regexp.MustCompile(`^[a-z0-9_]{3,24}$`)

// ValidateHandle applies the same handle rules as local registration.
func ValidateHandle(handle string) error {
	if !handlePattern.MatchString(handle) {
		return ErrInvalidHandle
	}
	return nil
}

// ValidateEmail applies the same email rules as local registration.
func ValidateEmail(email string) error {
	if _, err := mail.ParseAddress(email); err != nil {
		return ErrInvalidEmail
	}
	return nil
}

// ValidatePassword applies the same password length rule as registration.
func ValidatePassword(password string) error {
	if len(password) < minPassword {
		return ErrInvalidPassword
	}
	return nil
}

// ValidateRegistration mirrors the local registration validation so bootstrap
// flows cannot create accounts that normal signup would reject.
func ValidateRegistration(handle, email, password string) error {
	if err := ValidateHandle(handle); err != nil {
		return err
	}
	if err := ValidateEmail(email); err != nil {
		return err
	}
	return ValidatePassword(password)
}

// AdminAccount is the input for CreateVerifiedAdmin.
type AdminAccount struct {
	Handle       string
	Email        string
	DisplayName  string
	PasswordHash string
}

// CreateVerifiedAdmin creates an active, verified admin. The caller is
// responsible for transaction boundaries and uniqueness conflicts.
func (s *Service) CreateVerifiedAdmin(ctx context.Context, client *ent.Client, account AdminAccount) (*ent.User, error) {
	if client == nil {
		client = s.Client
	}
	return client.User.Create().
		SetHandle(account.Handle).
		SetEmail(account.Email).
		SetDisplayName(account.DisplayName).
		SetPasswordHash(account.PasswordHash).
		SetEmailVerifiedAt(db.Now()).
		SetRole(user.RoleAdmin).
		SetStatus(user.StatusActive).
		Save(ctx)
}

// SignValue signs an arbitrary payload with the session cookie key.
func (s *Service) SignValue(payload []byte) string {
	return s.sign(payload)
}

// VerifyValue verifies and returns a payload produced by SignValue.
func (s *Service) VerifyValue(value string) ([]byte, error) {
	return s.verifySigned(value)
}
