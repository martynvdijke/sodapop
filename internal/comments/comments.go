// Package comments owns check-in comment reads and writes.
package comments

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"html"
	"html/template"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"sodapop/ent"
	"sodapop/ent/comment"
	"sodapop/ent/notification"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrInvalid     = errors.New("invalid comment")
	ErrRateLimited = errors.New("comment rate limit reached")
)

const (
	maxBodyRunes    = 1000
	rateLimitCount  = 30
	rateLimitSpan   = time.Hour
	maxMentions     = 5
	defaultPageSize = 50
	maxPageSize     = 100
)

// mentionPattern matches an @handle token. Handles are lowercase ASCII letters,
// digits and underscores, 3-24 characters long. Matching is case-insensitive so
// "@Alice" mentions alice. The leading group captures the optional boundary
// character so "foo@bar" is not treated as a mention.
var mentionPattern = regexp.MustCompile(`(?i)(^|[^a-z0-9_])@([a-z0-9_]{3,24})`)

func isHandleChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_':
		return true
	}
	return false
}

// Service owns check-in comments.
type Service struct {
	Client   *ent.Client
	SQL      *sql.DB
	Checkins *checkins.Service
	Social   *social.Service
}

func New(database *db.DB, checkinsSvc *checkins.Service) *Service {
	s := &Service{Client: database.Client, SQL: database.SQL, Checkins: checkinsSvc}
	// Reuse the check-ins social service when one is wired in, so mentions get
	// block-aware notifications without a second constructor argument.
	if checkinsSvc != nil {
		s.Social = checkinsSvc.Social
	}
	return s
}

func (s *Service) socialService() *social.Service {
	if s.Social != nil {
		return s.Social
	}
	if s.Checkins != nil {
		return s.Checkins.Social
	}
	return nil
}

// Comment is the read model a template renders. CanDelete and CanReport are set
// by the caller because they depend on the viewer's role, not the row.
type Comment struct {
	ID          string
	UserID      string
	Handle      string
	DisplayName string
	Body        string
	BodyHTML    template.HTML
	CreatedAt   string
	CanDelete   bool
	CanReport   bool
}

// MentionHandles returns the distinct handles mentioned in body, lowercased,
// in first-appearance order, capped at maxMentions.
func MentionHandles(body string) []string {
	matches := mentionPattern.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, loc := range matches {
		hs, he := loc[4], loc[5]
		// Reject the prefix of a longer handle such as @this_handle_is_too_long.
		if he < len(body) && isHandleChar(body[he]) {
			continue
		}
		handle := strings.ToLower(body[hs:he])
		if seen[handle] {
			continue
		}
		seen[handle] = true
		out = append(out, handle)
		if len(out) == maxMentions {
			break
		}
	}
	return out
}

// RenderBody escapes a comment body and turns valid @handle tokens into profile
// links. Raw HTML never survives: escaping happens before linkification.
func RenderBody(body string) template.HTML {
	escaped := html.EscapeString(body)
	matches := mentionPattern.FindAllStringSubmatchIndex(escaped, -1)
	if len(matches) == 0 {
		return template.HTML(escaped)
	}
	var b strings.Builder
	last := 0
	for _, loc := range matches {
		start, end := loc[0], loc[1]
		hs, he := loc[4], loc[5]
		if he < len(escaped) && isHandleChar(escaped[he]) {
			continue
		}
		handle := strings.ToLower(escaped[hs:he])
		b.WriteString(escaped[last:start])
		b.WriteString(escaped[start : hs-1]) // boundary character, if any
		b.WriteString(`<a href="/u/` + handle + `" class="mention">@`)
		b.WriteString(escaped[hs:he])
		b.WriteString(`</a>`)
		last = end
	}
	b.WriteString(escaped[last:])
	return template.HTML(b.String())
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

	// A block in either direction makes the check-in unwritable. It is reported
	// as missing, never as a forbidden action.
	if parent.UserID != userID && s.Checkins.Social != nil {
		blocked, err := s.Checkins.Social.AreBlocked(ctx, userID, parent.UserID)
		if err != nil {
			return nil, err
		}
		if blocked {
			return nil, ErrNotFound
		}
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
		// Prefer the social service so OnNotify/push fans out exactly once. Fall
		// back to a direct insert only when no social service is wired in, which
		// keeps bare test or constructor setups working.
		if soc := s.socialService(); soc != nil {
			_ = soc.Notify(ctx, parent.UserID, string(notification.KindComment), userID, parent.ID, "commented on your check-in")
		} else {
			_, _ = s.Client.Notification.Create().
				SetUserID(parent.UserID).
				SetKind(notification.KindComment).
				SetActorID(userID).
				SetSubjectID(parent.ID).
				SetText("commented on your check-in").
				Save(ctx)
		}
	}

	// Mention notifications are best effort: a failure here must never undo or
	// fail the comment itself.
	s.notifyMentions(ctx, userID, parent, body)
	return c, nil
}

// notifyMentions records a mention notification for each eligible @handle. It
// skips the commenter, the check-in owner (already notified as a comment),
// blocked pairs, and users who cannot see the parent check-in.
func (s *Service) notifyMentions(ctx context.Context, commenterID string, parent *checkins.Detail, body string) {
	soc := s.socialService()
	for _, handle := range MentionHandles(body) {
		mentioned, err := s.Client.User.Query().
			Where(
				user.HandleEqualFold(handle),
				user.DeletedAtIsNil(),
				user.StatusEQ(user.StatusActive),
			).
			Only(ctx)
		if err != nil {
			continue
		}
		if mentioned.ID == commenterID || mentioned.ID == parent.UserID {
			continue
		}
		if soc != nil {
			blocked, err := soc.AreBlocked(ctx, commenterID, mentioned.ID)
			if err != nil || blocked {
				continue
			}
		}
		// The mentioned user must be able to see the parent check-in.
		if _, err := s.Checkins.Get(ctx, mentioned.ID, parent.ID); err != nil {
			continue
		}
		if soc != nil {
			_ = soc.Notify(ctx, mentioned.ID, string(notification.KindMention), commenterID, parent.ID, "mentioned you in a comment")
			continue
		}
		_, _ = s.Client.Notification.Create().
			SetUserID(mentioned.ID).
			SetKind(notification.KindMention).
			SetActorID(commenterID).
			SetSubjectID(parent.ID).
			SetText("mentioned you in a comment").
			Save(ctx)
	}
}

// visibleCommentScope is the SQL predicate shared by the page and total
// queries. It takes the viewer ID twice, in that order.
const visibleCommentScope = `
	cm.deleted_at IS NULL
	AND u.deleted_at IS NULL AND u.status <> 'deleted'
	AND NOT EXISTS (
		SELECT 1 FROM blocks bl
		WHERE (bl.blocker_id = ? AND bl.blocked_id = cm.user_id)
		   OR (bl.blocked_id = ? AND bl.blocker_id = cm.user_id)
	)`

// List returns the newest page of a visible check-in's live comments
// oldest-first. An invisible or missing parent is reported as ErrNotFound,
// never as a forbidden response.
func (s *Service) List(ctx context.Context, viewerID, checkinID string) ([]Comment, error) {
	items, _, _, err := s.ListPage(ctx, viewerID, checkinID, "", defaultPageSize)
	return items, err
}

// ListPage returns one page of visible comments oldest-first. before is the ID
// of the oldest comment already shown; the page then contains strictly older
// comments. total counts every visible comment on the check-in for display.
func (s *Service) ListPage(ctx context.Context, viewerID, checkinID, before string, limit int) (items []Comment, total int, hasMore bool, err error) {
	if _, err := s.Checkins.Get(ctx, viewerID, checkinID); err != nil {
		if errors.Is(err, checkins.ErrNotFound) {
			return nil, 0, false, ErrNotFound
		}
		return nil, 0, false, err
	}

	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}

	if err := s.SQL.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM comments cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.checkin_id = ? AND `+visibleCommentScope,
		checkinID, viewerID, viewerID).Scan(&total); err != nil {
		return nil, 0, false, err
	}

	query := `
		SELECT cm.id, cm.user_id, u.handle, COALESCE(u.display_name,''), cm.body, cm.created_at
		FROM comments cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.checkin_id = ? AND ` + visibleCommentScope
	args := []any{checkinID, viewerID, viewerID}

	if before != "" {
		var beforeCreatedAt string
		err := s.SQL.QueryRowContext(ctx,
			`SELECT created_at FROM comments WHERE id = ? AND checkin_id = ? AND deleted_at IS NULL`,
			before, checkinID).Scan(&beforeCreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, false, ErrInvalid
		}
		if err != nil {
			return nil, 0, false, err
		}
		query += ` AND ((cm.created_at < ?) OR (cm.created_at = ? AND cm.id < ?))`
		args = append(args, beforeCreatedAt, beforeCreatedAt, before)
	}

	// Read one extra row to learn whether an older page exists. The query is
	// newest-first so LIMIT keeps the newest rows of the requested window.
	query += ` ORDER BY cm.created_at DESC, cm.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()

	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.UserID, &c.Handle, &c.DisplayName, &c.Body, &c.CreatedAt); err != nil {
			return nil, 0, false, err
		}
		c.BodyHTML = RenderBody(c.Body)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, err
	}

	if len(out) > limit {
		hasMore = true
		out = out[:limit]
	}
	// Callers render threads oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, total, hasMore, nil
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

// EditAsModerator replaces the body of any live comment. Moderation happens
// outside the ownership and visibility rules the author path enforces, so the
// body is re-validated here instead of trusting the form.
func (s *Service) EditAsModerator(ctx context.Context, commentID, body string) error {
	body = strings.TrimSpace(body)
	if n := utf8.RuneCountInString(body); n < 1 || n > maxBodyRunes {
		return ErrInvalid
	}
	c, err := s.Client.Comment.Query().
		Where(comment.ID(commentID), comment.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	return s.Client.Comment.UpdateOneID(c.ID).SetBody(body).Exec(ctx)
}

// newID mirrors the schema's random 16-byte hex identifier.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
