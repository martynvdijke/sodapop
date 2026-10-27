// Package saves owns private bookmarks of check-ins. A save is scoped to one
// user and never widens what that user can see: every read re-checks the
// underlying check-in's visibility.
package saves

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"sodapop/ent"
	"sodapop/ent/checkinsave"
	"sodapop/internal/checkins"
	"sodapop/internal/db"
)

var (
	// ErrNotFound means the check-in is missing or not visible to the viewer.
	ErrNotFound = errors.New("not found")
	// ErrInvalid means the input or cursor is malformed.
	ErrInvalid = errors.New("invalid save")
)

// Service owns check-in bookmarks.
type Service struct {
	Client   *ent.Client
	SQL      *sql.DB
	Checkins *checkins.Service
}

// New builds the bookmark service. Checkins enforces visibility and blocks.
func New(database *db.DB, checkinsSvc *checkins.Service) *Service {
	return &Service{Client: database.Client, SQL: database.SQL, Checkins: checkinsSvc}
}

// Save bookmarks a check-in the user is allowed to see. It is idempotent and
// self-saves are allowed.
func (s *Service) Save(ctx context.Context, userID, checkinID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(checkinID) == "" {
		return ErrInvalid
	}
	// Visibility and blocks are enforced here so a save can never outlive the
	// permission that allowed it.
	if s.Checkins != nil {
		if _, err := s.Checkins.Get(ctx, userID, checkinID); err != nil {
			if errors.Is(err, checkins.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
	}
	exists, err := s.Client.CheckinSave.Query().
		Where(checkinsave.UserID(userID), checkinsave.CheckinID(checkinID)).
		Exist(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.Client.CheckinSave.Create().
		SetCheckinID(checkinID).
		SetUserID(userID).
		Save(ctx)
	if err != nil {
		// A concurrent insert may win the unique index. Treat that as success;
		// any other failure is real.
		ok, qerr := s.Client.CheckinSave.Query().
			Where(checkinsave.UserID(userID), checkinsave.CheckinID(checkinID)).
			Exist(ctx)
		if qerr == nil && ok {
			return nil
		}
		return err
	}
	return nil
}

// Unsave removes a bookmark. Removing an absent save is not an error.
func (s *Service) Unsave(ctx context.Context, userID, checkinID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(checkinID) == "" {
		return ErrInvalid
	}
	_, err := s.Client.CheckinSave.Delete().
		Where(checkinsave.UserID(userID), checkinsave.CheckinID(checkinID)).
		Exec(ctx)
	return err
}

// IsSaved reports whether the user has bookmarked the check-in.
func (s *Service) IsSaved(ctx context.Context, userID, checkinID string) (bool, error) {
	if userID == "" || checkinID == "" {
		return false, nil
	}
	return s.Client.CheckinSave.Query().
		Where(checkinsave.UserID(userID), checkinsave.CheckinID(checkinID)).
		Exist(ctx)
}

// SavedIDs marks which of checkinIDs the user has saved. It is used to render
// feed items without one query per row.
func (s *Service) SavedIDs(ctx context.Context, userID string, checkinIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(checkinIDs))
	if userID == "" || len(checkinIDs) == 0 {
		return out, nil
	}
	rows, err := s.Client.CheckinSave.Query().
		Where(checkinsave.UserID(userID), checkinsave.CheckinIDIn(checkinIDs...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.CheckinID] = true
	}
	return out, nil
}

// ListIDs returns saved check-in IDs newest first, skipping rows the viewer can
// no longer see. The cursor advances over skipped rows so a page of invisible
// saves still makes progress instead of looping.
func (s *Service) ListIDs(ctx context.Context, userID string, limit int, before string) (ids []string, hasMore bool, nextBefore string, err error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	query := `SELECT id, checkin_id FROM checkin_saves WHERE user_id = ?`
	args := []any{userID}
	if before != "" {
		var beforeCreated string
		err := s.SQL.QueryRowContext(ctx,
			`SELECT created_at FROM checkin_saves WHERE id = ? AND user_id = ?`,
			before, userID).Scan(&beforeCreated)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, "", ErrInvalid
		}
		if err != nil {
			return nil, false, "", err
		}
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, beforeCreated, beforeCreated, before)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, "", err
	}
	defer rows.Close()

	type saveRow struct {
		id        string
		checkinID string
	}
	scanned := make([]saveRow, 0, limit+1)
	for rows.Next() {
		var row saveRow
		if err := rows.Scan(&row.id, &row.checkinID); err != nil {
			return nil, false, "", err
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, "", err
	}

	if len(scanned) > limit {
		hasMore = true
		scanned = scanned[:limit]
	}
	if len(scanned) == 0 {
		return []string{}, false, "", nil
	}
	// The cursor always points at the last save row scanned for this page, even
	// when every row in it has since become invisible.
	nextBefore = scanned[len(scanned)-1].id

	ids = make([]string, 0, len(scanned))
	for _, row := range scanned {
		if s.Checkins == nil {
			ids = append(ids, row.checkinID)
			continue
		}
		if _, err := s.Checkins.Get(ctx, userID, row.checkinID); err != nil {
			if errors.Is(err, checkins.ErrNotFound) {
				continue
			}
			return nil, false, "", err
		}
		ids = append(ids, row.checkinID)
	}
	return ids, hasMore, nextBefore, nil
}
