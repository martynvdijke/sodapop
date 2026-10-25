package integrations

import (
	"errors"
	"testing"

	"sodapop/ent/inboundtoken"
	"sodapop/internal/auth"
)

func TestCreateInboundTokenStoresHashOnly(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	raw, tok, err := svc.CreateInboundToken(ctx, u.ID, "")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if raw == "" {
		t.Fatal("raw token is empty")
	}
	if tok.TokenHash == raw {
		t.Fatal("stored token hash equals the plaintext token")
	}
	if tok.TokenHash != auth.HashToken(raw) {
		t.Fatalf("stored hash = %q, want %q", tok.TokenHash, auth.HashToken(raw))
	}
	if tok.Label != "Home Assistant" {
		t.Fatalf("label = %q, want default", tok.Label)
	}
	if tok.Kind != inboundtoken.KindHomeAssistant {
		t.Fatalf("kind = %q", tok.Kind)
	}

	rows, err := database.Client.InboundToken.Query().All(ctx)
	if err != nil {
		t.Fatalf("query tokens: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("stored %d tokens, want 1", len(rows))
	}
	for _, row := range rows {
		if row.TokenHash == raw {
			t.Fatal("database contains the plaintext token")
		}
	}
}

func TestAuthenticateInboundIsRepeatable(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	raw, tok, err := svc.CreateInboundToken(ctx, u.ID, "patio")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	for i := 0; i < 3; i++ {
		got, err := svc.AuthenticateInbound(ctx, raw)
		if err != nil {
			t.Fatalf("authenticate #%d: %v", i, err)
		}
		if got.ID != tok.ID {
			t.Fatalf("authenticate #%d returned %q, want %q", i, got.ID, tok.ID)
		}
		if got.LastUsedAt == nil {
			t.Fatalf("authenticate #%d did not record last_used_at", i)
		}
	}

	if _, err := svc.AuthenticateInbound(ctx, "not-a-real-token"); !errors.Is(err, ErrInvalidInboundToken) {
		t.Fatalf("bad token error = %v, want ErrInvalidInboundToken", err)
	}
	if _, err := svc.AuthenticateInbound(ctx, ""); !errors.Is(err, ErrInvalidInboundToken) {
		t.Fatalf("empty token error = %v, want ErrInvalidInboundToken", err)
	}
}

func TestRevokedInboundTokenIsRejected(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	raw, tok, err := svc.CreateInboundToken(ctx, u.ID, "patio")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if err := svc.RevokeInboundToken(ctx, u.ID, tok.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.AuthenticateInbound(ctx, raw); !errors.Is(err, ErrInvalidInboundToken) {
		t.Fatalf("authenticate revoked token = %v, want ErrInvalidInboundToken", err)
	}

	// Another user cannot revoke it either.
	other := createUser(t, database, "bob")
	if err := svc.RevokeInboundToken(ctx, other.ID, tok.ID); !errors.Is(err, ErrInvalidInboundToken) {
		t.Fatalf("cross-user revoke = %v, want ErrInvalidInboundToken", err)
	}
}

func TestRecreateInboundTokenRevokesPrevious(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	firstRaw, first, err := svc.CreateInboundToken(ctx, u.ID, "first")
	if err != nil {
		t.Fatalf("create first token: %v", err)
	}
	secondRaw, second, err := svc.CreateInboundToken(ctx, u.ID, "second")
	if err != nil {
		t.Fatalf("create second token: %v", err)
	}

	if firstRaw == secondRaw {
		t.Fatal("re-created token reused the previous plaintext")
	}
	if _, err := svc.AuthenticateInbound(ctx, firstRaw); !errors.Is(err, ErrInvalidInboundToken) {
		t.Fatalf("old token still valid: %v", err)
	}
	got, err := svc.AuthenticateInbound(ctx, secondRaw)
	if err != nil {
		t.Fatalf("new token invalid: %v", err)
	}
	if got.ID != second.ID {
		t.Fatalf("authenticated %q, want %q", got.ID, second.ID)
	}

	refreshed, err := database.Client.InboundToken.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("reload first: %v", err)
	}
	if refreshed.RevokedAt == nil {
		t.Fatal("previous token was not revoked")
	}
	if refreshed.TokenHash != first.TokenHash {
		t.Fatal("token hash was rewritten")
	}

	rows, err := svc.ListInboundTokens(ctx, u.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("list returned %d rows, want 2", len(rows))
	}
}

func TestInboundTokenLabelIsCapped(t *testing.T) {
	svc, database, ctx := newTestService(t)
	u := createUser(t, database, "ada")

	long := ""
	for i := 0; i < 80; i++ {
		long += "x"
	}
	_, tok, err := svc.CreateInboundToken(ctx, u.ID, long)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if len(tok.Label) != 60 {
		t.Fatalf("label length = %d, want 60", len(tok.Label))
	}
}
