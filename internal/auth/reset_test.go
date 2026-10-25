package auth

import (
	"context"
	"testing"
	"time"

	"sodapop/ent/passwordresettoken"
	"sodapop/internal/db"
)

func TestIssuePasswordResetUnknownEmail(t *testing.T) {
	s, database := newTestService(t)
	ctx := context.Background()

	if err := s.IssuePasswordReset(ctx, "nobody@example.test"); err != nil {
		t.Fatalf("unknown email should not error: %v", err)
	}
	if n := countResetTokens(t, s); n != 0 {
		t.Fatalf("reset tokens = %d, want 0", n)
	}
	outbox, err := database.Client.EmailOutbox.Query().Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outbox != 0 {
		t.Fatalf("outbox rows = %d, want 0", outbox)
	}
}

func TestIssuePasswordResetSkipsOIDCOnlyAccount(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	if _, err := s.Client.User.Create().
		SetHandle("oidcuser").
		SetEmail("oidc@example.test").
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.IssuePasswordReset(ctx, "oidc@example.test"); err != nil {
		t.Fatalf("issue reset: %v", err)
	}
	if n := countResetTokens(t, s); n != 0 {
		t.Fatalf("reset tokens = %d, want 0", n)
	}
}

func TestPasswordResetSingleUse(t *testing.T) {
	s, database := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "fizz", "fizz@example.test", "originalpass")
	if _, err := s.CreateSession(ctx, u.ID, "1.2.3.4", "test-agent"); err != nil {
		t.Fatal(err)
	}

	if err := s.IssuePasswordReset(ctx, "Fizz@Example.test"); err != nil {
		t.Fatalf("issue reset: %v", err)
	}
	if n := countResetTokens(t, s); n != 1 {
		t.Fatalf("reset tokens = %d, want 1", n)
	}
	token := resetTokenFromOutbox(t, database, u.Email)
	if !s.PasswordResetValid(ctx, token) {
		t.Fatal("fresh token should be valid")
	}

	if err := s.ConsumePasswordReset(ctx, token, "short"); err == nil {
		t.Fatal("weak password should be rejected")
	}
	if _, _, err := s.CreateAPIToken(ctx, u.ID, "phone", []string{"read"}); err != nil {
		t.Fatalf("create api token: %v", err)
	}
	if _, err := s.Client.InboundToken.Create().
		SetUserID(u.ID).
		SetLabel("Home Assistant").
		SetTokenHash(HashToken("inbound-secret")).
		Save(ctx); err != nil {
		t.Fatalf("create inbound token: %v", err)
	}
	if err := s.ConsumePasswordReset(ctx, token, "brandnewpass"); err != nil {
		t.Fatalf("consume reset: %v", err)
	}
	if got := countSessions(t, s, u.ID); got != 0 {
		t.Fatalf("sessions = %d, want all revoked", got)
	}
	apiTokens, err := s.Client.APIToken.Query().All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apiTokens) != 1 || apiTokens[0].RevokedAt == nil {
		t.Fatalf("api tokens = %+v, want one revoked", apiTokens)
	}
	inbound, err := s.Client.InboundToken.Query().All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbound) != 1 || inbound[0].RevokedAt == nil {
		t.Fatalf("inbound tokens = %+v, want one revoked", inbound)
	}
	refreshed, err := s.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.PasswordHash == nil || !CheckPassword(*refreshed.PasswordHash, "brandnewpass") {
		t.Fatal("new password was not stored")
	}
	if CheckPassword(*refreshed.PasswordHash, "originalpass") {
		t.Fatal("old password still works")
	}

	// A second use of the same token must fail.
	if err := s.ConsumePasswordReset(ctx, token, "anotherpass"); err == nil {
		t.Fatal("token should be single use")
	}
	if s.PasswordResetValid(ctx, token) {
		t.Fatal("used token should not be valid")
	}
}

func TestPasswordResetExpired(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "flat", "flat@example.test", "originalpass")

	token := RandomToken(32)
	past := time.Now().UTC().Add(-time.Minute)
	if _, err := s.Client.PasswordResetToken.Create().
		SetTokenHash(HashToken(token)).
		SetUserID(u.ID).
		SetCreatedAt(past.Add(-time.Hour).Format(time.RFC3339)).
		SetExpiresAt(past.Format(time.RFC3339)).
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	if s.PasswordResetValid(ctx, token) {
		t.Fatal("expired token should not be valid")
	}
	if err := s.ConsumePasswordReset(ctx, token, "brandnewpass"); err == nil {
		t.Fatal("expired token should be rejected")
	}
}

func TestPasswordResetUnknownToken(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	if s.PasswordResetValid(ctx, "not-a-token") {
		t.Fatal("unknown token should not be valid")
	}
	if err := s.ConsumePasswordReset(ctx, "not-a-token", "brandnewpass"); err == nil {
		t.Fatal("unknown token should be rejected")
	}
}

func TestIssuePasswordResetRetiresOlderTokens(t *testing.T) {
	s, database := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "cola", "cola@example.test", "originalpass")

	if err := s.IssuePasswordReset(ctx, u.Email); err != nil {
		t.Fatal(err)
	}
	first := resetTokenFromOutbox(t, database, u.Email)
	if err := s.IssuePasswordReset(ctx, u.Email); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Client.PasswordResetToken.Query().
		Where(passwordresettoken.UserID(u.ID), passwordresettoken.UsedAtIsNil()).
		All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("live tokens = %d, want 1", len(rows))
	}
	if s.PasswordResetValid(ctx, first) {
		t.Fatal("older token should be retired")
	}
}

func TestPasswordResetDeletedUserRejected(t *testing.T) {
	s, database := newTestService(t)
	ctx := context.Background()
	u := createUser(t, s, "gone", "gone@example.test", "originalpass")
	if err := s.IssuePasswordReset(ctx, u.Email); err != nil {
		t.Fatal(err)
	}
	token := resetTokenFromOutbox(t, database, u.Email)
	if _, err := s.Client.User.UpdateOneID(u.ID).
		SetStatus("deleted").
		SetDeletedAt(db.Now()).
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	if s.PasswordResetValid(ctx, token) {
		t.Fatal("deleted user token should not be valid")
	}
	if err := s.ConsumePasswordReset(ctx, token, "brandnewpass"); err == nil {
		t.Fatal("deleted user should be rejected")
	}
}
