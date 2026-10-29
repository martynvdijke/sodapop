// Package sharelinks stores the capability tokens behind public share cards.
// A token is an unguessable URL the owner can revoke; it never grants any other
// access to the account.
package sharelinks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned for unknown, revoked, or invisible links.
var ErrNotFound = errors.New("share link not found")

// ErrInvalidKind is returned when a caller asks for an unsupported card kind.
var ErrInvalidKind = errors.New("invalid share link kind")

// Kinds lists the card kinds a user may share.
var Kinds = []string{"passport", "streak"}

// Link is one share capability row.
type Link struct {
	ID        string
	UserID    string
	Kind      string
	Token     string
	CreatedAt string
	RevokedAt *string
}

// Service persists share links in SQLite.
type Service struct {
	SQL *sql.DB
}

// New returns a share link service over the given SQL handle.
func New(sqlDB *sql.DB) *Service {
	return &Service{SQL: sqlDB}
}

// maxActiveLinks caps how many unrevoked links one user may hold at once.
const maxActiveLinks = 10

// Create mints a new capability link for userID. The token is 32 random bytes
// rendered as hex (64 characters) and the row id is 16 random bytes of hex.
// After inserting, the oldest active links beyond maxActiveLinks are revoked in
// the same transaction so a user cannot accumulate an unbounded set of live
// capability URLs.
func (s *Service) Create(ctx context.Context, userID, kind string) (*Link, error) {
	if !ValidKind(kind) {
		return nil, ErrInvalidKind
	}
	id, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("share link id: %w", err)
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("share link token: %w", err)
	}
	createdAt := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin share link transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO share_links (id, user_id, kind, token, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, userID, kind, token, createdAt); err != nil {
		return nil, fmt.Errorf("insert share link: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM share_links
		WHERE user_id = ? AND revoked_at IS NULL
		ORDER BY created_at DESC, rowid DESC
		LIMIT -1 OFFSET ?`, userID, maxActiveLinks)
	if err != nil {
		return nil, fmt.Errorf("select share links beyond cap: %w", err)
	}
	excess := make([]string, 0, 4)
	for rows.Next() {
		var oldID string
		if err := rows.Scan(&oldID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan share link beyond cap: %w", err)
		}
		excess = append(excess, oldID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate share links beyond cap: %w", err)
	}
	rows.Close()

	revokedAt := time.Now().UTC().Format(time.RFC3339)
	for _, oldID := range excess {
		if _, err := tx.ExecContext(ctx, `
			UPDATE share_links SET revoked_at = ?
			WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
			revokedAt, oldID, userID); err != nil {
			return nil, fmt.Errorf("revoke share link beyond cap: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit share link: %w", err)
	}
	return &Link{ID: id, UserID: userID, Kind: kind, Token: token, CreatedAt: createdAt}, nil
}

// Resolve returns the active link for token. Revoked and unknown tokens are
// indistinguishable and both return ErrNotFound.
func (s *Service) Resolve(ctx context.Context, token string) (*Link, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	var link Link
	var revoked sql.NullString
	err := s.SQL.QueryRowContext(ctx, `
		SELECT id, user_id, kind, token, created_at, revoked_at
		FROM share_links
		WHERE token = ? AND revoked_at IS NULL`, token).
		Scan(&link.ID, &link.UserID, &link.Kind, &link.Token, &link.CreatedAt, &revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if revoked.Valid {
		return nil, ErrNotFound
	}
	return &link, nil
}

// ListForUser returns the user's newest active links. limit defaults to 20 and
// is capped at 50.
func (s *Service) ListForUser(ctx context.Context, userID string, limit int) ([]Link, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT id, user_id, kind, token, created_at
		FROM share_links
		WHERE user_id = ? AND revoked_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Link{}
	for rows.Next() {
		var link Link
		if err := rows.Scan(&link.ID, &link.UserID, &link.Kind, &link.Token, &link.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

// Revoke marks one of userID's links revoked. The user id is part of the
// predicate, so a foreign id can never be revoked.
func (s *Service) Revoke(ctx context.Context, userID, id string) (bool, error) {
	res, err := s.SQL.ExecContext(ctx, `
		UPDATE share_links
		SET revoked_at = ?
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ValidKind reports whether kind is a shareable card kind.
func ValidKind(kind string) bool {
	for _, k := range Kinds {
		if kind == k {
			return true
		}
	}
	return false
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
