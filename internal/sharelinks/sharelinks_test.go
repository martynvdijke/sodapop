package sharelinks

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"sodapop/internal/db"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "sharelinks.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return New(database.SQL)
}

func TestCreateValidatesAndResolves(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "u1", "bogus"); !errors.Is(err, ErrInvalidKind) {
		t.Fatalf("invalid kind err = %v, want ErrInvalidKind", err)
	}

	link, err := svc.Create(ctx, "u1", "passport")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(link.ID) != 32 {
		t.Errorf("id length = %d, want 32", len(link.ID))
	}
	if len(link.Token) != 64 {
		t.Errorf("token length = %d, want 64", len(link.Token))
	}
	if link.Kind != "passport" || link.UserID != "u1" {
		t.Errorf("link = %+v", link)
	}

	got, err := svc.Resolve(ctx, link.Token)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != link.ID || got.Token != link.Token || got.Kind != "passport" {
		t.Errorf("resolved %+v, want %+v", got, link)
	}
	if got.RevokedAt != nil {
		t.Error("fresh link is already revoked")
	}

	for _, token := range []string{"", "unknown", "deadbeef"} {
		if _, err := svc.Resolve(ctx, token); !errors.Is(err, ErrNotFound) {
			t.Errorf("resolve(%q) err = %v, want ErrNotFound", token, err)
		}
	}
}

func TestRevokeIsScopedToOwner(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	link, err := svc.Create(ctx, "owner", "streak")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ok, err := svc.Revoke(ctx, "someone-else", link.ID)
	if err != nil {
		t.Fatalf("foreign revoke: %v", err)
	}
	if ok {
		t.Fatal("foreign revoke reported success")
	}
	if _, err := svc.Resolve(ctx, link.Token); err != nil {
		t.Fatalf("foreign revoke made the link disappear: %v", err)
	}

	ok, err = svc.Revoke(ctx, "owner", link.ID)
	if err != nil {
		t.Fatalf("owner revoke: %v", err)
	}
	if !ok {
		t.Fatal("owner revoke reported no change")
	}
	if _, err := svc.Resolve(ctx, link.Token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked resolve err = %v, want ErrNotFound", err)
	}

	ok, err = svc.Revoke(ctx, "owner", link.ID)
	if err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	if ok {
		t.Fatal("second revoke reported a change")
	}
}

func TestCreateCapsActiveLinks(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	links := make([]*Link, 0, maxActiveLinks+1)
	for i := 0; i < maxActiveLinks+1; i++ {
		link, err := svc.Create(ctx, "capped", "passport")
		if err != nil {
			t.Fatalf("create %d: %v", i+1, err)
		}
		links = append(links, link)
	}

	active, err := svc.ListForUser(ctx, "capped", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(active) != maxActiveLinks {
		t.Fatalf("active links = %d, want %d", len(active), maxActiveLinks)
	}
	activeIDs := make(map[string]bool, len(active))
	for _, link := range active {
		activeIDs[link.ID] = true
	}
	if activeIDs[links[0].ID] {
		t.Error("oldest link is still active")
	}
	if _, err := svc.Resolve(ctx, links[0].Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("oldest link resolve err = %v, want ErrNotFound", err)
	}
	for _, link := range links[1:] {
		if !activeIDs[link.ID] {
			t.Errorf("link %s was revoked before the cap", link.ID)
		}
	}
}

func TestListForUserOrderingAndCap(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 60; i++ {
		created := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		if _, err := svc.SQL.ExecContext(ctx, `
			INSERT INTO share_links (id, user_id, kind, token, created_at)
			VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("u1-%02d", i), "u1", "passport", fmt.Sprintf("token-u1-%02d", i), created); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	// A newer revoked row must not displace active rows.
	if _, err := svc.SQL.ExecContext(ctx, `
		INSERT INTO share_links (id, user_id, kind, token, created_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"u1-revoked", "u1", "streak", "token-revoked",
		base.Add(2*time.Hour).Format(time.RFC3339), base.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert revoked: %v", err)
	}
	// Another user's fresh row must never leak into this list.
	if _, err := svc.SQL.ExecContext(ctx, `
		INSERT INTO share_links (id, user_id, kind, token, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		"u2-newest", "u2", "passport", "token-u2", base.Add(3*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("insert other user: %v", err)
	}

	links, err := svc.ListForUser(ctx, "u1", 0)
	if err != nil {
		t.Fatalf("list default: %v", err)
	}
	if len(links) != 20 {
		t.Fatalf("default len = %d, want 20", len(links))
	}
	if links[0].ID != "u1-59" {
		t.Errorf("newest = %q, want u1-59", links[0].ID)
	}
	if links[len(links)-1].ID != "u1-40" {
		t.Errorf("oldest on first page = %q, want u1-40", links[len(links)-1].ID)
	}
	for _, link := range links {
		if link.UserID != "u1" {
			t.Errorf("leaked link for %q", link.UserID)
		}
		if link.ID == "u1-revoked" {
			t.Error("revoked link appeared in the list")
		}
	}

	capped, err := svc.ListForUser(ctx, "u1", 999)
	if err != nil {
		t.Fatalf("list capped: %v", err)
	}
	if len(capped) != 50 {
		t.Fatalf("capped len = %d, want 50", len(capped))
	}
	if capped[0].ID != "u1-59" {
		t.Errorf("capped newest = %q, want u1-59", capped[0].ID)
	}

	short, err := svc.ListForUser(ctx, "u1", 3)
	if err != nil {
		t.Fatalf("list short: %v", err)
	}
	if len(short) != 3 || short[0].ID != "u1-59" || short[2].ID != "u1-57" {
		t.Errorf("short list = %+v", short)
	}
}
