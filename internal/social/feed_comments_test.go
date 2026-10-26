package social_test

import (
	"context"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/comment"
	"sodapop/internal/db"
	"sodapop/internal/social"
)

func TestFeedIncludesLiveCommentCount(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "feed-comments.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()

	mkUser := func(handle string) *ent.User {
		u, err := database.Client.User.Create().
			SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(handle).Save(ctx)
		if err != nil {
			t.Fatalf("user %s: %v", handle, err)
		}
		return u
	}
	alice := mkUser("alice")
	bob := mkUser("bob")
	drink, err := database.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	c, err := database.Client.Checkin.Create().
		SetUserID(alice.ID).SetDrinkID(drink.ID).SetVisibility(checkin.VisibilityPublic).Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if _, err := database.Client.Comment.Create().
		SetCheckinID(c.ID).SetUserID(bob.ID).SetBody("one").Save(ctx); err != nil {
		t.Fatalf("comment one: %v", err)
	}
	if _, err := database.Client.Comment.Create().
		SetCheckinID(c.ID).SetUserID(alice.ID).SetBody("two").Save(ctx); err != nil {
		t.Fatalf("comment two: %v", err)
	}
	deleted, err := database.Client.Comment.Create().
		SetCheckinID(c.ID).SetUserID(bob.ID).SetBody("gone").Save(ctx)
	if err != nil {
		t.Fatalf("comment deleted: %v", err)
	}
	if err := database.Client.Comment.UpdateOneID(deleted.ID).SetDeletedAt(db.Now()).Exec(ctx); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	svc := social.New(database)
	for _, feed := range []string{"friends", "following"} {
		var items []social.FeedItem
		if feed == "following" {
			items, err = svc.FeedFollowing(ctx, alice.ID, 20, "")
		} else {
			items, err = svc.Feed(ctx, alice.ID, 20, "")
		}
		if err != nil {
			t.Fatalf("%s feed: %v", feed, err)
		}
		var found bool
		for _, item := range items {
			if item.ID == c.ID {
				found = true
				if item.Comments != 2 {
					t.Fatalf("%s feed comment count = %d, want 2", feed, item.Comments)
				}
			}
		}
		if !found {
			t.Fatalf("%s feed missing check-in %s", feed, c.ID)
		}
	}

	// A deleted comment row is tracked separately from the count.
	n, err := database.Client.Comment.Query().Where(comment.DeletedAtNotNil()).Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("deleted comments = %d err %v, want 1", n, err)
	}
}
