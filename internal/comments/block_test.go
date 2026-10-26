package comments_test

import (
	"errors"
	"testing"

	"sodapop/internal/comments"
	"sodapop/internal/social"
)

func TestBlockedCheckinOwnerHidesCommentsList(t *testing.T) {
	f := newFixture(t)
	soc := social.New(f.db)
	c := f.checkin(t, "public")

	if err := soc.Block(f.ctx, f.stranger.ID, f.owner.Handle); err != nil {
		t.Fatalf("block stranger->owner: %v", err)
	}
	if _, err := f.svc.List(f.ctx, f.stranger.ID, c.ID); !errors.Is(err, comments.ErrNotFound) {
		t.Fatalf("blocked comments list = %v, want ErrNotFound", err)
	}

	// The other direction is just as hidden.
	if err := soc.Unblock(f.ctx, f.stranger.ID, f.owner.Handle); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if err := soc.Block(f.ctx, f.owner.ID, f.stranger.Handle); err != nil {
		t.Fatalf("block owner->stranger: %v", err)
	}
	if _, err := f.svc.List(f.ctx, f.stranger.ID, c.ID); !errors.Is(err, comments.ErrNotFound) {
		t.Fatalf("reverse blocked comments list = %v, want ErrNotFound", err)
	}
}

func TestBlockedCommentCreateAndList(t *testing.T) {
	f := newFixture(t)
	soc := social.New(f.db)

	c := f.checkin(t, "public")

	// A block in either direction makes the check-in unwritable.
	if err := soc.Block(f.ctx, f.owner.ID, f.stranger.Handle); err != nil {
		t.Fatalf("block owner->stranger: %v", err)
	}
	if _, err := f.svc.Create(f.ctx, f.stranger.ID, c.ID, "hello"); !errors.Is(err, comments.ErrNotFound) {
		t.Fatalf("blocked create = %v, want ErrNotFound", err)
	}
	if err := soc.Unblock(f.ctx, f.owner.ID, f.stranger.Handle); err != nil {
		t.Fatalf("unblock owner->stranger: %v", err)
	}

	// Content survives a block; only reads are filtered.
	if _, err := f.svc.Create(f.ctx, f.friend.ID, c.ID, "from friend"); err != nil {
		t.Fatalf("friend comment: %v", err)
	}
	if _, err := f.svc.Create(f.ctx, f.owner.ID, c.ID, "from owner"); err != nil {
		t.Fatalf("owner comment: %v", err)
	}
	if err := soc.Block(f.ctx, f.stranger.ID, f.friend.Handle); err != nil {
		t.Fatalf("block stranger->friend: %v", err)
	}

	list, err := f.svc.List(f.ctx, f.stranger.ID, c.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Body != "from owner" {
		t.Fatalf("blocked list = %+v, want only owner's comment", list)
	}

	// Unblocking restores visibility.
	if err := soc.Unblock(f.ctx, f.stranger.ID, f.friend.Handle); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	list, err = f.svc.List(f.ctx, f.stranger.ID, c.ID)
	if err != nil {
		t.Fatalf("list after unblock: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list after unblock = %d, want 2", len(list))
	}
}
