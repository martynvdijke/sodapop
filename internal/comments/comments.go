// Package comments owns check-in comment reads and writes.
package comments

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"sodapop/ent"
	"sodapop/ent/comment"
	"sodapop/ent/notification"
	"sodapop/internal/checkins"
	"sodapop/internal/db"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrInvalid     = errors.New("invalid comment")
	ErrRateLimited = errors.New("comment rate limit reached")
)

const (
	maxBodyRunes   = 1000
	rateLimitCount = 30
	rateLimitSpan  = time.Hour
	// maxComments bounds a thread so one check-in cannot make the detail page
	// arbitrarily expensive. The newest comments are kept.
	maxComments = 200
)

// Service owns check-in comments.
type Service struct {
	Client   *ent.Client
	SQL      *sql.DB
	Checkins *checkins.Service
}

func New(database *db.DB, checkinsSvc *checkins.Service) *Service {
	return &Service{Client: database.Client, SQL: database.SQL, Checkins: checkinsSvc}
}

// Comment is the read model a template renders. CanDelete is set by the caller
// because it depends on the viewer's role, not the row.
type Comment struct {
	ID          string
	UserID      string
	Handle      string
	DisplayName string
	Body        string
	CreatedAt   string
	CanDelete   bool
}

// Initial returns the uppercase first rune of the display name, falling back to
// the handle, or "?" when both are empty.
func (c Comment) Initial() string {
	name := strings.TrimSpace(c.DisplayName)
	if name == "" {
		name = strings.TrimSpace(c.Handle)
	}
	if name == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(name)[0]))
}

// Create adds a comment to a visible check-in. The parent check-in owner is
// notified unless they are the commenter. At most 30 comments per user are
// accepted in any rolling hour.
func (s *Service) Create(ctx context.Context, userID, checkinID, body string) (*ent.Comment, error) {
	body = strings.TrimSpace(body)
	if n := utf8.RuneCountInString(body); n < 1 || n > maxBodyRunes {
		return nil, ErrInvalid
	}

	parent, err := s.Checkins.Get(ctx, userID, checkinID)
	if err != nil {
		if errors.Is(err, checkins.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	cutoff := time.Now().UTC().Add(-rateLimitSpan).Format(time.RFC3339)
	var recent int
	if err := s.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM comments WHERE user_id = ? AND created_at >= ?`,
		userID, cutoff).Scan(&recent); err != nil {
		return nil, err
	}
	if recent >= rateLimitCount {
		return nil, ErrRateLimited
	}

	// Insert through raw SQL so the limit stays a rune limit. Ent's generated
	// MaxLen validator counts bytes, which would reject 1,000 multi-byte runes.
	id, err := newID()
	if err != nil {
		return nil, err
	}
	createdAt := db.Now()
	if _, err := s.SQL.ExecContext(ctx,
		`INSERT INTO comments (id, checkin_id, user_id, body, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, parent.ID, userID, body, createdAt); err != nil {
		return nil, err
	}
	c := &ent.Comment{
		ID:        id,
		CheckinID: parent.ID,
		UserID:    userID,
		Body:      body,
		CreatedAt: createdAt,
	}

	if parent.UserID != userID {
		_, _ = s.Client.Notification.Create().
			SetUserID(parent.UserID).
			SetKind(notification.KindComment).
			SetActorID(userID).
			SetSubjectID(parent.ID).
			SetText("commented on your check-in").
			Save(ctx)
	}
	return c, nil
}

// List returns a visible check-in's live comments oldest-first, capped at the
// newest maxComments. An invisible or missing parent is reported as ErrNotFound,
// never as a forbidden response.
func (s *Service) List(ctx context.Context, viewerID, checkinID string) ([]Comment, error) {
	if _, err := s.Checkins.Get(ctx, viewerID, checkinID); err != nil {
		if errors.Is(err, checkins.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT cm.id, cm.user_id, u.handle, COALESCE(u.display_name,''), cm.body, cm.created_at
		FROM comments cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.checkin_id = ? AND cm.deleted_at IS NULL
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		ORDER BY cm.created_at DESC, cm.id DESC
		LIMIT ?`, checkinID, maxComments)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.UserID, &c.Handle, &c.DisplayName, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The query reads newest-first so LIMIT keeps the newest rows; callers
	// render threads oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// Delete soft-deletes a comment. Non-authors cannot tell a comment exists unless
// their parent check-in is visible. Admins may remove any comment.
func (s *Service) Delete(ctx context.Context, userID string, admin bool, commentID string) error {
	c, err := s.Client.Comment.Query().
		Where(comment.ID(commentID), comment.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}

	if !admin {
		if c.UserID != userID {
			return ErrNotFound
		}
		if _, err := s.Checkins.Get(ctx, userID, c.CheckinID); err != nil {
			if errors.Is(err, checkins.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
	}

	return s.Client.Comment.UpdateOneID(c.ID).SetDeletedAt(db.Now()).Exec(ctx)
}

// newID mirrors the schema's random 16-byte hex identifier.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
