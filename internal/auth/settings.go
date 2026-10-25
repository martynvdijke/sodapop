package auth

import (
	"context"

	"sodapop/ent"
	"sodapop/ent/usersetting"
)

// GetOrCreateSettings returns the per-user settings row, creating defaults on first use.
func (s *Service) GetOrCreateSettings(ctx context.Context, userID string) (*ent.UserSetting, error) {
	row, err := s.Client.UserSetting.Query().
		Where(usersetting.UserID(userID)).
		Only(ctx)
	if err == nil {
		return row, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}
	row, err = s.Client.UserSetting.Create().SetUserID(userID).Save(ctx)
	if err == nil {
		return row, nil
	}
	// Tolerate a concurrent create winning the unique constraint.
	if ent.IsConstraintError(err) {
		return s.Client.UserSetting.Query().
			Where(usersetting.UserID(userID)).
			Only(ctx)
	}
	return nil, err
}
