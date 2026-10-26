package venues

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"sodapop/ent"
	"sodapop/ent/venuemedia"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/media"
)

type mediaHarness struct {
	svc    *Service
	media  *media.Service
	db     *db.DB
	ctx    context.Context
	venue  *ent.Venue
	upload *ent.User
	other  *ent.User
}

func newMediaHarness(t *testing.T) *mediaHarness {
	t.Helper()
	ctx := context.Background()
	database, err := db.Init(filepath.Join(t.TempDir(), "venues.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	upload, err := database.Client.User.Create().
		SetHandle("uploader").SetEmail("uploader@example.com").Save(ctx)
	if err != nil {
		t.Fatalf("create uploader: %v", err)
	}
	other, err := database.Client.User.Create().
		SetHandle("other").SetEmail("other@example.com").Save(ctx)
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	v, err := database.Client.Venue.Create().
		SetName("Test Bar").SetSlug("test-bar").Save(ctx)
	if err != nil {
		t.Fatalf("create venue: %v", err)
	}
	mediaSvc := media.New(database, &config.Config{DataDir: t.TempDir()})
	return &mediaHarness{
		svc:    New(database),
		media:  mediaSvc,
		db:     database,
		ctx:    ctx,
		venue:  v,
		upload: upload,
		other:  other,
	}
}

func (h *mediaHarness) addRow(t *testing.T, venueID int, userID, kind string, deleted bool) *ent.VenueMedia {
	t.Helper()
	b := h.db.Client.VenueMedia.Create().
		SetVenueID(venueID).
		SetMediaID(fmt.Sprintf("media-%d-%s", len(userID)+venueID, userID)).
		SetKind(venuemedia.Kind(kind)).
		SetUploadedBy(userID)
	if deleted {
		b.SetDeletedAt(db.Now())
	}
	row, err := b.Save(h.ctx)
	if err != nil {
		t.Fatalf("insert venue media: %v", err)
	}
	return row
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 24; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 9), G: uint8(y * 13), B: 0x44, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestAddVenueMediaStoresAndLists(t *testing.T) {
	h := newMediaHarness(t)
	row, err := h.svc.AddVenueMedia(h.ctx, h.media, h.upload.ID, h.venue.ID, "menu", "  Drinks menu  ",
		bytes.NewReader(testPNG(t)), "menu.png", "image/png")
	if err != nil {
		t.Fatalf("add venue media: %v", err)
	}
	if row.Kind != venuemedia.KindMenu {
		t.Errorf("kind = %q, want menu", row.Kind)
	}
	if row.Label != "Drinks menu" {
		t.Errorf("label = %q, want trimmed", row.Label)
	}
	if row.MediaID == "" {
		t.Error("media id is empty")
	}

	views, err := h.svc.VenueMedia(h.ctx, h.venue.ID)
	if err != nil {
		t.Fatalf("list venue media: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("views = %d, want 1", len(views))
	}
	got := views[0]
	if got.ID != row.ID || got.MediaID != row.MediaID || got.Kind != "menu" ||
		got.Label != "Drinks menu" || got.UploadedBy != h.upload.ID ||
		got.UploaderHandle != "uploader" {
		t.Errorf("view = %+v, want row %+v with handle uploader", got, row)
	}
}

func TestAddVenueMediaDefaultsKindAndCapsLabel(t *testing.T) {
	h := newMediaHarness(t)
	row, err := h.svc.AddVenueMedia(h.ctx, h.media, h.upload.ID, h.venue.ID, "", "",
		bytes.NewReader(testPNG(t)), "photo.png", "image/png")
	if err != nil {
		t.Fatalf("add default kind: %v", err)
	}
	if row.Kind != venuemedia.KindPhoto {
		t.Errorf("kind = %q, want photo", row.Kind)
	}

	long := make([]rune, maxMediaLabel+1)
	for i := range long {
		long[i] = 'x'
	}
	_, err = h.svc.AddVenueMedia(h.ctx, h.media, h.upload.ID, h.venue.ID, "photo", string(long),
		bytes.NewReader(testPNG(t)), "photo.png", "image/png")
	if !errors.Is(err, ErrInvalidLabel) {
		t.Errorf("label error = %v, want ErrInvalidLabel", err)
	}
}

func TestAddVenueMediaRejectsInvalidKindBeforeStore(t *testing.T) {
	h := newMediaHarness(t)
	for _, kind := range []string{"video", "drink", "nope"} {
		_, err := h.svc.AddVenueMedia(h.ctx, nil, h.upload.ID, h.venue.ID, kind, "",
			bytes.NewReader(testPNG(t)), "x.png", "image/png")
		if !errors.Is(err, ErrInvalidKind) {
			t.Errorf("kind %q error = %v, want ErrInvalidKind", kind, err)
		}
	}
}

func TestAddVenueMediaRejectsNonImage(t *testing.T) {
	h := newMediaHarness(t)
	_, err := h.svc.AddVenueMedia(h.ctx, h.media, h.upload.ID, h.venue.ID, "photo", "",
		bytes.NewReader([]byte("this is definitely not an image")), "notes.txt", "text/plain")
	if !errors.Is(err, ErrInvalidMedia) {
		t.Fatalf("error = %v, want ErrInvalidMedia", err)
	}
}

func TestAddVenueMediaRejectsUnknownVenue(t *testing.T) {
	h := newMediaHarness(t)
	_, err := h.svc.AddVenueMedia(h.ctx, h.media, h.upload.ID, 999999, "photo", "",
		bytes.NewReader(testPNG(t)), "x.png", "image/png")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestAddVenueMediaPerUploaderCap(t *testing.T) {
	h := newMediaHarness(t)
	for i := 0; i < maxMediaPerUser; i++ {
		h.addRow(t, h.venue.ID, h.upload.ID, "photo", false)
	}
	// A nil media service proves the cap is rejected before any Store call.
	_, err := h.svc.AddVenueMedia(h.ctx, nil, h.upload.ID, h.venue.ID, "photo", "",
		bytes.NewReader(testPNG(t)), "x.png", "image/png")
	if !errors.Is(err, ErrMediaLimit) {
		t.Fatalf("error = %v, want ErrMediaLimit", err)
	}
}

func TestAddVenueMediaPerVenueCap(t *testing.T) {
	h := newMediaHarness(t)
	for i := 0; i < maxMediaPerVenue; i++ {
		h.addRow(t, h.venue.ID, fmt.Sprintf("user-%d", i), "photo", false)
	}
	_, err := h.svc.AddVenueMedia(h.ctx, nil, h.upload.ID, h.venue.ID, "menu", "",
		bytes.NewReader(testPNG(t)), "x.png", "image/png")
	if !errors.Is(err, ErrMediaLimit) {
		t.Fatalf("error = %v, want ErrMediaLimit", err)
	}
}

func TestAddVenueMediaIgnoresSoftDeletedRowsForCap(t *testing.T) {
	h := newMediaHarness(t)
	for i := 0; i < maxMediaPerUser-1; i++ {
		h.addRow(t, h.venue.ID, h.upload.ID, "photo", false)
	}
	for i := 0; i < 3; i++ {
		h.addRow(t, h.venue.ID, h.upload.ID, "photo", true)
	}
	row, err := h.svc.AddVenueMedia(h.ctx, h.media, h.upload.ID, h.venue.ID, "photo", "",
		bytes.NewReader(testPNG(t)), "x.png", "image/png")
	if err != nil {
		t.Fatalf("add with deleted rows: %v", err)
	}
	views, err := h.svc.VenueMedia(h.ctx, h.venue.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, v := range views {
		if v.ID == row.ID {
			return
		}
	}
	t.Fatalf("new row %s missing from active list", row.ID)
}

func TestVenueMediaUnknownVenue(t *testing.T) {
	h := newMediaHarness(t)
	if _, err := h.svc.VenueMedia(h.ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestRemoveVenueMediaPermissions(t *testing.T) {
	h := newMediaHarness(t)
	row := h.addRow(t, h.venue.ID, h.upload.ID, "photo", false)

	if err := h.svc.RemoveVenueMedia(h.ctx, h.other.ID, false, row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user error = %v, want ErrNotFound", err)
	}
	views, err := h.svc.VenueMedia(h.ctx, h.venue.ID)
	if err != nil {
		t.Fatalf("list after rejected remove: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("views after rejected remove = %d, want 1", len(views))
	}

	if err := h.svc.RemoveVenueMedia(h.ctx, h.upload.ID, false, row.ID); err != nil {
		t.Fatalf("uploader remove: %v", err)
	}
	views, err = h.svc.VenueMedia(h.ctx, h.venue.ID)
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("views after remove = %d, want 0", len(views))
	}

	adminRow := h.addRow(t, h.venue.ID, h.upload.ID, "menu", false)
	if err := h.svc.RemoveVenueMedia(h.ctx, h.other.ID, true, adminRow.ID); err != nil {
		t.Fatalf("admin remove: %v", err)
	}
	if err := h.svc.RemoveVenueMedia(h.ctx, h.upload.ID, false, adminRow.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove already deleted = %v, want ErrNotFound", err)
	}
	if err := h.svc.RemoveVenueMedia(h.ctx, h.upload.ID, false, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove missing = %v, want ErrNotFound", err)
	}
}
