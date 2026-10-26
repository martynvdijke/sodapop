package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sodapop/ent/checkin"
	"sodapop/ent/friendship"
	"sodapop/ent/media"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

func newTestService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	// db.Init appends its own "?_pragma=..." suffix, so end the URI with "&"
	// to keep the shared-cache in-memory parameters intact.
	dsn := fmt.Sprintf("media-%d?mode=memory&cache=shared&", time.Now().UnixNano())
	database, err := db.Init(dsn)
	if err != nil {
		t.Fatalf("init media db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return New(database, &config.Config{DataDir: t.TempDir()}), database
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x * 7) % 256),
				G: uint8((y * 11) % 256),
				B: 0x33,
				A: 0xFF,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func assertStored(t *testing.T, dir, rel string) {
	t.Helper()
	if strings.TrimSpace(rel) == "" {
		t.Fatal("stored path is empty")
	}
	if filepath.IsAbs(rel) {
		t.Fatalf("stored path %q must be relative to the media root", rel)
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil {
		t.Fatalf("stat %s: %v", full, err)
	}
	if info.Size() == 0 {
		t.Fatalf("stored file %s is empty", full)
	}
}

func TestStoreImage(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	pngData := testPNG(t, 40, 20)
	row, err := svc.Store(ctx, "owner-1", "checkin", bytes.NewReader(pngData), "My Photo.PNG", "image/png")
	if err != nil {
		t.Fatalf("store image: %v", err)
	}
	if row.Mime != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg", row.Mime)
	}
	if row.Kind != media.KindCheckin {
		t.Errorf("kind = %q, want checkin", row.Kind)
	}
	if row.Width <= 0 || row.Height <= 0 {
		t.Errorf("dimensions = %dx%d, want > 0", row.Width, row.Height)
	}
	if row.Bytes <= 0 {
		t.Errorf("bytes = %d, want > 0", row.Bytes)
	}
	if row.ThumbPath == "" {
		t.Fatal("thumb_path is empty")
	}
	if row.PosterPath != "" {
		t.Errorf("poster_path = %q, want empty for photos", row.PosterPath)
	}
	if row.UserID == nil || *row.UserID != "owner-1" {
		t.Errorf("user_id = %v, want owner-1", row.UserID)
	}

	assertStored(t, svc.Dir, row.Path)
	assertStored(t, svc.Dir, row.ThumbPath)
	if !strings.HasSuffix(row.Path, ".jpg") || !strings.HasSuffix(row.ThumbPath, ".thumb.jpg") {
		t.Errorf("paths = %q / %q, want .jpg and .thumb.jpg", row.Path, row.ThumbPath)
	}

	rc, opened, err := svc.Open(ctx, row.ID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()
	if opened.ID != row.ID {
		t.Errorf("opened id = %q, want %q", opened.ID, row.ID)
	}
	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode stored jpeg: %v", err)
	}
	if decoded.Bounds().Dx() != 40 || decoded.Bounds().Dy() != 20 {
		t.Errorf("stored dimensions = %dx%d, want 40x20", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}

	trc, _, err := svc.OpenThumb(ctx, row.ID)
	if err != nil {
		t.Fatalf("open thumb: %v", err)
	}
	defer trc.Close()
	if _, err := jpeg.Decode(trc); err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
}

func TestStoreImageResizesLongestEdge(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	row, err := svc.Store(ctx, "owner-1", "avatar", bytes.NewReader(testPNG(t, 1800, 900)), "big.png", "image/png")
	if err != nil {
		t.Fatalf("store image: %v", err)
	}
	if row.Kind != media.KindAvatar {
		t.Errorf("kind = %q, want avatar", row.Kind)
	}
	if row.Width != maxPhotoEdge || row.Height != 800 {
		t.Errorf("dimensions = %dx%d, want 1600x800", row.Width, row.Height)
	}

	rc, _, err := svc.OpenThumb(ctx, row.ID)
	if err != nil {
		t.Fatalf("open thumb: %v", err)
	}
	defer rc.Close()
	cfg, err := jpeg.DecodeConfig(rc)
	if err != nil {
		t.Fatalf("decode thumb config: %v", err)
	}
	if cfg.Width != thumbEdge || cfg.Height != 240 {
		t.Errorf("thumb dimensions = %dx%d, want 480x240", cfg.Width, cfg.Height)
	}
}

func TestStoreRejectsUnsupported(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.Store(ctx, "owner-1", "checkin", strings.NewReader("this is definitely not an image or a video"), "notes.txt", "text/plain")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}

	heic := append([]byte{0, 0, 0, 0x18}, []byte("ftypheic")...)
	heic = append(heic, make([]byte, 64)...)
	_, err = svc.Store(ctx, "owner-1", "checkin", bytes.NewReader(heic), "photo.heic", "image/heic")
	if !errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("heic err = %v, want ErrUnsupportedImage", err)
	}
}

func TestStoreVideo(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Minimal ISO base media header: ftyp box with an mp4 compatible brand.
	video := append([]byte{0x00, 0x00, 0x00, 0x18}, []byte("ftypmp42")...)
	video = append(video, []byte{0x00, 0x00, 0x00, 0x00, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}...)
	video = append(video, bytes.Repeat([]byte{0x00}, 2048)...)

	row, err := svc.Store(ctx, "owner-1", "checkin", bytes.NewReader(video), "clip.mp4", "video/mp4")
	if err != nil {
		t.Fatalf("store video: %v", err)
	}
	if row.Kind != media.KindVideo {
		t.Errorf("kind = %q, want video", row.Kind)
	}
	if row.Mime != "video/mp4" {
		t.Errorf("mime = %q, want video/mp4", row.Mime)
	}
	if row.Bytes != len(video) {
		t.Errorf("bytes = %d, want %d", row.Bytes, len(video))
	}
	if row.ThumbPath != "" || row.PosterPath != "" {
		t.Errorf("video thumb/poster = %q / %q, want empty", row.ThumbPath, row.PosterPath)
	}
	if !strings.HasSuffix(row.Path, ".mp4") {
		t.Errorf("path = %q, want .mp4 suffix", row.Path)
	}
	assertStored(t, svc.Dir, row.Path)

	rc, _, err := svc.Open(ctx, row.ID)
	if err != nil {
		t.Fatalf("open video: %v", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read video: %v", err)
	}
	if !bytes.Equal(raw, video) {
		t.Error("stored video bytes differ from upload")
	}
}

func TestCanView(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()

	owner, err := database.Client.User.Create().SetHandle("owner").SetEmail("owner@example.com").Save(ctx)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	viewer, err := database.Client.User.Create().SetHandle("viewer").SetEmail("viewer@example.com").Save(ctx)
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	friend, err := database.Client.User.Create().SetHandle("friend").SetEmail("friend@example.com").Save(ctx)
	if err != nil {
		t.Fatalf("create friend: %v", err)
	}
	drink, err := database.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("create drink: %v", err)
	}

	row, err := svc.Store(ctx, owner.ID, "checkin", bytes.NewReader(testPNG(t, 16, 16)), "photo.png", "image/png")
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Owner and catalog media are visible; an unrelated user is not.
	if ok, err := svc.CanView(ctx, owner.ID, row.ID); err != nil || !ok {
		t.Fatalf("owner CanView = %v, %v; want true, nil", ok, err)
	}
	if ok, err := svc.CanView(ctx, viewer.ID, row.ID); err != nil || ok {
		t.Fatalf("viewer CanView = %v, %v; want false, nil", ok, err)
	}

	// A public check-in exposes the photo to everyone.
	check, err := database.Client.Checkin.Create().
		SetUserID(owner.ID).
		SetDrinkID(drink.ID).
		SetPhotoMediaID(row.ID).
		SetVisibility(checkin.VisibilityPublic).
		Save(ctx)
	if err != nil {
		t.Fatalf("create public checkin: %v", err)
	}
	if ok, err := svc.CanView(ctx, viewer.ID, row.ID); err != nil || !ok {
		t.Fatalf("public checkin CanView = %v, %v; want true, nil", ok, err)
	}
	if ok, err := svc.CanView(ctx, "", row.ID); err != nil || !ok {
		t.Fatalf("anonymous public CanView = %v, %v; want true, nil", ok, err)
	}

	// Friends-only check-ins need an accepted friendship.
	if _, err := check.Update().SetVisibility(checkin.VisibilityFriends).Save(ctx); err != nil {
		t.Fatalf("make friends-only: %v", err)
	}
	if ok, err := svc.CanView(ctx, viewer.ID, row.ID); err != nil || ok {
		t.Fatalf("friends-only stranger CanView = %v, %v; want false, nil", ok, err)
	}

	a, b := orderIDs(owner.ID, friend.ID)
	if _, err := database.Client.Friendship.Create().
		SetUserA(a).SetUserB(b).
		SetStatus(friendship.StatusAccepted).
		SetRequestedBy(owner.ID).
		Save(ctx); err != nil {
		t.Fatalf("create friendship: %v", err)
	}
	if ok, err := svc.CanView(ctx, friend.ID, row.ID); err != nil || !ok {
		t.Fatalf("friend CanView = %v, %v; want true, nil", ok, err)
	}

	// Soft-deleted check-ins stop granting visibility.
	if _, err := check.Update().SetDeletedAt(db.Now()).Save(ctx); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if ok, err := svc.CanView(ctx, friend.ID, row.ID); err != nil || ok {
		t.Fatalf("deleted checkin CanView = %v, %v; want false, nil", ok, err)
	}
}

func TestCanViewVenueMediaFollowsActiveRow(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()

	owner, err := database.Client.User.Create().SetHandle("uploader").SetEmail("uploader@example.com").Save(ctx)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	venue, err := database.Client.Venue.Create().SetName("Corner Shop").SetSlug("corner-shop").Save(ctx)
	if err != nil {
		t.Fatalf("create venue: %v", err)
	}

	row, err := svc.Store(ctx, owner.ID, "venue", bytes.NewReader(testPNG(t, 16, 16)), "storefront.png", "image/png")
	if err != nil {
		t.Fatalf("store venue image: %v", err)
	}
	if row.Kind != media.KindVenue {
		t.Fatalf("kind = %q, want venue", row.Kind)
	}
	vm, err := database.Client.VenueMedia.Create().
		SetVenueID(venue.ID).
		SetMediaID(row.ID).
		SetUploadedBy(owner.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create venue media: %v", err)
	}

	// An active venue_media row is public to anonymous viewers.
	if ok, err := svc.CanView(ctx, "", row.ID); err != nil || !ok {
		t.Fatalf("active venue media CanView = %v, %v; want true, nil", ok, err)
	}

	// Removing the row revokes anonymous access even though the media row and
	// file are deliberately kept.
	if _, err := vm.Update().SetDeletedAt(db.Now()).Save(ctx); err != nil {
		t.Fatalf("soft delete venue media: %v", err)
	}
	if ok, err := svc.CanView(ctx, "", row.ID); err != nil || ok {
		t.Fatalf("removed venue media CanView = %v, %v; want false, nil", ok, err)
	}

	// A venue-kind media row with no venue_media row (an orphan) is not public.
	orphan, err := svc.Store(ctx, owner.ID, "venue", bytes.NewReader(testPNG(t, 16, 16)), "orphan.png", "image/png")
	if err != nil {
		t.Fatalf("store orphan: %v", err)
	}
	if ok, err := svc.CanView(ctx, "", orphan.ID); err != nil || ok {
		t.Fatalf("orphan venue media CanView = %v, %v; want false, nil", ok, err)
	}
}

func TestOpenRejectsTraversal(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()

	row, err := database.Client.Media.Create().
		SetPath("../escape.jpg").
		SetMime("image/jpeg").
		Save(ctx)
	if err != nil {
		t.Fatalf("create media: %v", err)
	}
	if _, _, err := svc.Open(ctx, row.ID); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v, want ErrInvalidPath", err)
	}
	if _, err := svc.resolve("../escape.jpg"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("resolve err = %v, want ErrInvalidPath", err)
	}
	if _, err := svc.resolve("/etc/passwd"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("absolute resolve err = %v, want ErrInvalidPath", err)
	}
}

func orderIDs(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}
