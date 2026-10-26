package api

import (
	"bytes"
	"context"
	"image/jpeg"
	"io"
	"net/http"
	"strconv"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/internal/db"
)

// storeCheckinPhoto writes a real JPEG through the media service so the test
// exercises the same storage and permission paths as production uploads.
func storeCheckinPhoto(t *testing.T, h *harness, ownerID string) *ent.Media {
	t.Helper()
	row, err := h.server.Media.Store(context.Background(), ownerID, "checkin",
		bytes.NewReader(jpegBytes(t)), "photo.jpg", "image/jpeg")
	if err != nil {
		t.Fatalf("store media: %v", err)
	}
	return row
}

// mediaCheckin attaches a media row to a check-in with the given visibility.
func mediaCheckin(t *testing.T, h *harness, userID string, drinkID int, mediaID, visibility string, deleted bool) *ent.Checkin {
	t.Helper()
	create := h.server.Auth.Client.Checkin.Create().
		SetUserID(userID).
		SetDrinkID(drinkID).
		SetPhotoMediaID(mediaID).
		SetVisibility(checkin.Visibility(visibility))
	if deleted {
		create = create.SetDeletedAt(db.Now())
	}
	row, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	return row
}

func newAPIUser(t *testing.T, h *harness, handle string) string {
	t.Helper()
	u, err := h.server.Auth.Client.User.Create().
		SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(handle).
		SetEmailVerifiedAt(db.Now()).Save(context.Background())
	if err != nil {
		t.Fatalf("user %s: %v", handle, err)
	}
	return u.ID
}

func TestMediaOwnerFetchesPrivatePhoto(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Private Cola", "private-cola", nil)
	media := storeCheckinPhoto(t, h, h.alice)
	mediaCheckin(t, h, h.alice, drink.ID, media.ID, "private", false)

	rec := h.do(t, http.MethodGet, "/api/v1/media/"+media.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner full: got %d, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("content-type = %q, want image/jpeg", ct)
	}
	if cl := rec.Header().Get("Content-Length"); cl == "" || cl == "0" {
		t.Errorf("content-length = %q, want the stored file size", cl)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=86400" {
		t.Errorf("cache-control = %q", cc)
	}
	if _, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Errorf("body is not a JPEG: %v", err)
	}

	thumb := h.do(t, http.MethodGet, "/api/v1/media/"+media.ID+"/thumb", "", true)
	if thumb.Code != http.StatusOK {
		t.Fatalf("owner thumb: got %d, body %s", thumb.Code, thumb.Body.String())
	}
	if ct := thumb.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("thumb content-type = %q, want image/jpeg", ct)
	}
	if _, err := jpeg.Decode(bytes.NewReader(thumb.Body.Bytes())); err != nil {
		t.Errorf("thumb body is not a JPEG: %v", err)
	}

	// Private photos are not visible to another account at either size.
	carol := newAPIUser(t, h, "carol")
	carolToken := h.tokenFor(t, carol)
	for _, path := range []string{"/api/v1/media/" + media.ID, "/api/v1/media/" + media.ID + "/thumb"} {
		rec := h.doWith(t, carolToken, http.MethodGet, path, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", path, rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s 404 cache-control = %q, want no-store", path, cc)
		}
	}
}

func TestMediaFriendsOnlyVisibility(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	drink := newDrink(t, h, "Shared Cola", "shared-cola", nil)
	media := storeCheckinPhoto(t, h, h.alice)
	mediaCheckin(t, h, h.alice, drink.ID, media.ID, "friends", false)
	bobToken := h.tokenFor(t, h.bob)

	// Not yet friends: bob is a stranger and gets an indistinguishable 404.
	rec := h.doWith(t, bobToken, http.MethodGet, "/api/v1/media/"+media.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger: got %d, want 404", rec.Code)
	}

	f, err := h.server.Social.Request(ctx, h.alice, "bob")
	if err != nil || f == nil {
		t.Fatalf("friend request: %v", err)
	}
	if err := h.server.Social.Accept(ctx, h.bob, strconv.Itoa(f.ID)); err != nil {
		t.Fatalf("accept: %v", err)
	}
	rec = h.doWith(t, bobToken, http.MethodGet, "/api/v1/media/"+media.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("friend full: got %d, body %s", rec.Code, rec.Body.String())
	}
	rec = h.doWith(t, bobToken, http.MethodGet, "/api/v1/media/"+media.ID+"/thumb", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("friend thumb: got %d, body %s", rec.Code, rec.Body.String())
	}

	// Blocking drops the friendship and revokes the photo immediately.
	if err := h.server.Social.Block(ctx, h.alice, "bob"); err != nil {
		t.Fatalf("block: %v", err)
	}
	rec = h.doWith(t, bobToken, http.MethodGet, "/api/v1/media/"+media.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("blocked friend: got %d, want 404", rec.Code)
	}
	// The owner keeps access.
	rec = h.do(t, http.MethodGet, "/api/v1/media/"+media.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner after block: got %d", rec.Code)
	}
}

func TestMediaPublicPhotoVisibleToStranger(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Public Cola", "public-cola", nil)
	media := storeCheckinPhoto(t, h, h.alice)
	mediaCheckin(t, h, h.alice, drink.ID, media.ID, "public", false)
	carol := newAPIUser(t, h, "carol")

	rec := h.doWith(t, h.tokenFor(t, carol), http.MethodGet, "/api/v1/media/"+media.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public stranger: got %d, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("content-type = %q, want image/jpeg", ct)
	}
}

func TestMediaSoftDeletedCheckinNotFound(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Deleted Cola", "deleted-cola", nil)
	media := storeCheckinPhoto(t, h, h.alice)
	mediaCheckin(t, h, h.alice, drink.ID, media.ID, "public", true)

	carol := newAPIUser(t, h, "carol")
	rec := h.doWith(t, h.tokenFor(t, carol), http.MethodGet, "/api/v1/media/"+media.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("soft-deleted checkin: got %d, want 404", rec.Code)
	}
}

func TestMediaAnonymousAndUnknown(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Anon Cola", "anon-cola", nil)
	media := storeCheckinPhoto(t, h, h.alice)
	mediaCheckin(t, h, h.alice, drink.ID, media.ID, "public", false)

	for _, path := range []string{"/api/v1/media/" + media.ID, "/api/v1/media/" + media.ID + "/thumb"} {
		rec := h.do(t, http.MethodGet, path, "", false)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s anonymous: got %d, want 401", path, rec.Code)
		}
	}

	rec := h.do(t, http.MethodGet, "/api/v1/media/does-not-exist", "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: got %d, want 404", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("unknown id cache-control = %q, want no-store", cc)
	}
}

func TestMediaResponseIsBinary(t *testing.T) {
	h := newHarness(t)
	media := storeCheckinPhoto(t, h, h.alice)

	rec := h.do(t, http.MethodGet, "/api/v1/media/"+media.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner read: got %d", rec.Code)
	}
	raw, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xD8 {
		t.Fatalf("body does not start with a JPEG SOI marker: %x", raw[:2])
	}
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(len(raw)); got != want {
		t.Errorf("content-length = %q, want %q", got, want)
	}
}
