package adminnotes

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sodapop/ent/adminnote"
	"sodapop/internal/db"
)

func newTestService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return New(database.Client), database
}

func mkUser(t *testing.T, database *db.DB, handle string) string {
	t.Helper()
	u, err := database.Client.User.Create().SetHandle(handle).SetEmail(handle + "@example.com").Save(context.Background())
	if err != nil {
		t.Fatalf("user %s: %v", handle, err)
	}
	return u.ID
}

func TestCreateListDelete(t *testing.T) {
	svc, database := newTestService(t)
	author := mkUser(t, database, "mod")
	other := mkUser(t, database, "other")
	ctx := context.Background()

	row, err := svc.Create(ctx, author, "system", 42, "  looking into it  ")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.Body != "looking into it" {
		t.Fatalf("body = %q, want trimmed text", row.Body)
	}
	if row.Entity != adminnote.EntitySystem || row.EntityID != 42 {
		t.Fatalf("row = %+v", row)
	}

	rows, err := svc.List(ctx, "system", 42)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("list = %+v", rows)
	}
	if otherRows, err := svc.List(ctx, "system", 43); err != nil || len(otherRows) != 0 {
		t.Fatalf("other entity rows = %v, %v", otherRows, err)
	}

	if err := svc.Delete(ctx, other, row.ID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin delete = %v, want ErrForbidden", err)
	}
	if err := svc.Delete(ctx, author, row.ID, false); err != nil {
		t.Fatalf("author delete: %v", err)
	}
	if err := svc.Delete(ctx, author, row.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing = %v, want ErrNotFound", err)
	}
}

func TestCreateValidation(t *testing.T) {
	svc, database := newTestService(t)
	author := mkUser(t, database, "mod")
	ctx := context.Background()

	cases := []struct {
		name     string
		entity   string
		entityID int
		body     string
	}{
		{"empty body", "system", 1, "   "},
		{"too long", "system", 1, strings.Repeat("a", MaxBodyRunes+1)},
		{"unknown entity", "report", 1, "hello"},
		{"zero entity id", "system", 0, "hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(ctx, author, tc.entity, tc.entityID, tc.body); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
	got, err := svc.Create(ctx, author, "system", 1, strings.Repeat("a", MaxBodyRunes))
	if err != nil || len([]rune(got.Body)) != MaxBodyRunes {
		t.Fatalf("max body: %v, %v", got, err)
	}
}
