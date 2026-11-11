package comments_test

import (
	"errors"
	"strings"
	"testing"

	"sodapop/internal/comments"
)

func TestEditByAuthor(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")
	created, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "first thoughts")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := f.svc.Edit(f.ctx, f.friend.ID, created.ID, "  revised thoughts @alice  "); err != nil {
		t.Fatalf("edit: %v", err)
	}

	list, err := f.svc.List(f.ctx, f.owner.ID, c.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("comments = %d", len(list))
	}
	if list[0].Body != "revised thoughts @alice" {
		t.Fatalf("body = %q", list[0].Body)
	}
	if list[0].EditedAt == "" {
		t.Fatal("edited_at was not set")
	}
	if list[0].EditedLabel() == "" {
		t.Fatal("EditedLabel is empty after an edit")
	}
	if !strings.Contains(string(list[0].BodyHTML), `class="mention"`) {
		t.Fatalf("mention link lost after edit: %s", list[0].BodyHTML)
	}
}

func TestEditRefusals(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")

	t.Run("non-author is not found", func(t *testing.T) {
		created, err := f.svc.Create(f.ctx, f.owner.ID, c.ID, "owner only")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := f.svc.Edit(f.ctx, f.friend.ID, created.ID, "hijacked"); !errors.Is(err, comments.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("deleted is not editable", func(t *testing.T) {
		created, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "temporary")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := f.svc.Delete(f.ctx, f.friend.ID, false, created.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if err := f.svc.Edit(f.ctx, f.friend.ID, created.ID, "back"); !errors.Is(err, comments.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("empty body is invalid", func(t *testing.T) {
		created, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "something")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := f.svc.Edit(f.ctx, f.friend.ID, created.ID, "   "); !errors.Is(err, comments.ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
	})

	t.Run("over-long body is invalid", func(t *testing.T) {
		created, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "something else")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := f.svc.Edit(f.ctx, f.friend.ID, created.ID, strings.Repeat("x", 1001)); !errors.Is(err, comments.ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
	})

	t.Run("missing comment is not found", func(t *testing.T) {
		if err := f.svc.Edit(f.ctx, f.friend.ID, "does-not-exist", "hi"); !errors.Is(err, comments.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

// A 1,000-rune multi-byte body must survive the edit: the limit is runes, not
// bytes.
func TestEditBodyCapCountsRunes(t *testing.T) {
	f := newFixture(t)
	c := f.checkin(t, "public")
	created, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "short")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	body := strings.Repeat("é", 1000)
	if err := f.svc.Edit(f.ctx, f.friend.ID, created.ID, body); err != nil {
		t.Fatalf("edit: %v", err)
	}
	list, err := f.svc.List(f.ctx, f.owner.ID, c.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Body != body {
		t.Fatalf("body was not stored intact")
	}
}
