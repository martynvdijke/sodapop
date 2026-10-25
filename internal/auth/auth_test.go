package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/emailoutbox"
	"sodapop/ent/session"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

// newTestService opens an isolated in-memory database with cheap bcrypt.
func newTestService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	// db.Init appends its own "?_pragma=..." suffix, so end the URI with "&".
	dsn := fmt.Sprintf("auth-%d?mode=memory&cache=shared&", time.Now().UnixNano())
	database, err := db.Init(dsn)
	if err != nil {
		t.Fatalf("init auth db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfg := &config.Config{
		BaseURL:    "https://sodapop.test",
		SessionKey: []byte("test-session-secret"),
		SessionTTL: time.Hour,
		BcryptCost: 4,
	}
	return New(cfg, database), database
}

// createUser inserts an active, email-verified local account.
func createUser(t *testing.T, s *Service, handle, email, password string) *ent.User {
	t.Helper()
	ctx := context.Background()
	hash, err := s.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u, err := s.Client.User.Create().
		SetHandle(handle).
		SetEmail(email).
		SetEmailVerifiedAt(db.Now()).
		SetPasswordHash(hash).
		SetDisplayName(handle).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

// itoaTest is a tiny strconv.Itoa alias shared by the test files.
func itoaTest(v int) string { return strconv.Itoa(v) }

// resetTokenFromOutbox reads the raw token that was emailed to the address.
func resetTokenFromOutbox(t *testing.T, database *db.DB, to string) string {
	t.Helper()
	row, err := database.Client.EmailOutbox.Query().
		Where(emailoutbox.ToEmail(to), emailoutbox.Template("password_reset")).
		Order(ent.Desc(emailoutbox.FieldID)).
		First(context.Background())
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	var p struct {
		Link string `json:"link"`
	}
	if err := json.Unmarshal([]byte(row.PayloadJSON), &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	_, token, ok := strings.Cut(p.Link, "token=")
	if !ok || token == "" {
		t.Fatalf("payload link %q has no token", p.Link)
	}
	return token
}

func countResetTokens(t *testing.T, s *Service) int {
	t.Helper()
	n, err := s.Client.PasswordResetToken.Query().Count(context.Background())
	if err != nil {
		t.Fatalf("count reset tokens: %v", err)
	}
	return n
}

func countSessions(t *testing.T, s *Service, userID string) int {
	t.Helper()
	n, err := s.Client.Session.Query().Where(session.UserID(userID)).Count(context.Background())
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}
