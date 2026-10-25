package auth

import (
	"context"
	"errors"
	"strconv"

	"sodapop/ent"
	"sodapop/ent/session"
)

// ErrNotFound is returned when a user-owned row does not exist for that user.
var ErrNotFound = errors.New("not found")

// ListSessions returns a user's live sessions, newest first.
func (s *Service) ListSessions(ctx context.Context, userID string) ([]*ent.Session, error) {
	return s.Client.Session.Query().
		Where(session.UserID(userID)).
		Order(ent.Desc(session.FieldCreatedAt), ent.Desc(session.FieldID)).
		All(ctx)
}

// RevokeSession deletes one session belonging to the user. Sessions owned by
// somebody else are reported as not found so callers cannot probe for IDs.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	id, ok := sessionIDInt(sessionID)
	if !ok {
		return ErrNotFound
	}
	n, err := s.Client.Session.Delete().
		Where(session.ID(id), session.UserID(userID)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAllSessions deletes every session for the user except exceptSessionID,
// which may be empty to delete all of them.
func (s *Service) RevokeAllSessions(ctx context.Context, userID, exceptSessionID string) error {
	q := s.Client.Session.Delete().Where(session.UserID(userID))
	if id, ok := sessionIDInt(exceptSessionID); ok {
		q = q.Where(session.IDNEQ(id))
	}
	_, err := q.Exec(ctx)
	return err
}

// sessionIDInt parses the numeric session primary key used in URLs.
func sessionIDInt(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
