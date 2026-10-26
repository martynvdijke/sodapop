package catalog

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/media"
)

type galleryHarness struct {
	svc    *Service
	media  *media.Service
	db     *db.DB
	ctx    context.Context
	drink  *ent.Drink
	user   *ent.User
	dir    string
	mediaD string
}

func newGalleryHarness(t *testing.T) *galleryHarness {
	t.Helper()
	database := openMemory(t)
	dir := t.TempDir()
	mediaSvc := media.New(database, &config.Config{DataDir: dir})
	svc := New(&config.Config{DataDir: dir}, database)
	ctx := context.Background()
	d, err := database.Client.Drink.Create().SetName("Cherry Cola").SetSlug("cherry-cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	u := mkUser(t, database.Client, "uploader")
	return &galleryHarness{svc: svc, media: mediaSvc, db: database, ctx: ctx,
		drink: d, user: u, dir: dir, mediaD: filepath.Join(dir, "media")}
}

func galleryPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 18, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 18; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 11), G: uint8(y * 7), B: 0x55, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func (h *galleryHarness) add(t *testing.T, attribution string) *ent.DrinkImage {
	t.Helper()
	row, err := h.svc.AddImage(h.ctx, h.media, h.user.ID, h.drink.ID,
		bytes.NewReader(galleryPNG(t)), "soda.png", "image/png", attribution)
	if err != nil {
		t.Fatalf("add image: %v", err)
	}
	return row
}

func TestCanModerateGalleryRoles(t *testing.T) {
	for _, tc := range []struct {
		role string
		want bool
	}{
		{"admin", true},
		{"moderator", true},
		{"user", false},
		{"", false},
	} {
		if got := CanModerateGallery(tc.role); got != tc.want {
			t.Errorf("CanModerateGallery(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
}

func TestAddImageStoresAndLists(t *testing.T) {
	h := newGalleryHarness(t)
	first := h.add(t, "  Photo by me  ")
	if first.DrinkID != h.drink.ID {
		t.Errorf("drink id = %d, want %d", first.DrinkID, h.drink.ID)
	}
	if first.IsPrimary {
		t.Error("new image must not be auto-promoted")
	}
	if first.Attribution != "Photo by me" {
		t.Errorf("attribution = %q, want trimmed", first.Attribution)
	}
	if !strings.HasPrefix(first.URL, "/media/") {
		t.Errorf("url = %q, want /media/{id}", first.URL)
	}
	stored, err := h.db.Client.Media.Get(h.ctx, strings.TrimPrefix(first.URL, "/media/"))
	if err != nil {
		t.Fatalf("media row: %v", err)
	}
	if stored.Kind != "drink" {
		t.Errorf("media kind = %q, want drink", stored.Kind)
	}

	second := h.add(t, "")
	if second.Attribution != h.user.Handle {
		t.Errorf("fallback attribution = %q, want %q", second.Attribution, h.user.Handle)
	}

	images, err := h.svc.Gallery(h.ctx, h.drink.ID)
	if err != nil {
		t.Fatalf("gallery: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("gallery len = %d, want 2", len(images))
	}
	// Newest first; created_at has second precision, so id breaks the tie.
	if images[0].ID != second.ID || images[1].ID != first.ID {
		t.Fatalf("gallery order = %d,%d; want %d,%d", images[0].ID, images[1].ID, second.ID, first.ID)
	}
	if images[0].MediaID == "" {
		t.Error("derived media id is empty")
	}
}

func TestAddImageCleansUpStoredMediaWhenAttachFails(t *testing.T) {
	h := newGalleryHarness(t)
	var mergeErr error
	merged := false
	reader := &mergeOnceReader{
		src: bytes.NewReader(galleryPNG(t)),
		onFirstRead: func() {
			merged = true
			_, mergeErr = h.db.Client.Drink.UpdateOneID(h.drink.ID).
				SetStatus(drink.StatusMerged).Save(h.ctx)
		},
	}

	_, err := h.svc.AddImage(h.ctx, h.media, h.user.ID, h.drink.ID,
		reader, "soda.png", "image/png", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddImage on merged drink = %v, want ErrNotFound", err)
	}
	if !merged || mergeErr != nil {
		t.Fatalf("merge hook ran=%v err=%v", merged, mergeErr)
	}
	if n, err := h.db.Client.Media.Query().Count(h.ctx); err != nil || n != 0 {
		t.Fatalf("media rows after failed attach = %d, %v; want 0", n, err)
	}
	if n, err := h.db.Client.DrinkImage.Query().Count(h.ctx); err != nil || n != 0 {
		t.Fatalf("image rows after failed attach = %d, %v; want 0", n, err)
	}
	files := 0
	_ = filepath.WalkDir(h.mediaD, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Fatalf("media directory still holds %d files after cleanup", files)
	}
}

// mergeOnceReader merges the drink on the first read, after AddImage's initial
// existence check but before the transaction re-check.
type mergeOnceReader struct {
	src         *bytes.Reader
	onFirstRead func()
	once        sync.Once
}

func (m *mergeOnceReader) Read(p []byte) (int, error) {
	m.once.Do(m.onFirstRead)
	return m.src.Read(p)
}

func TestAddImageTrimsAttributionToCap(t *testing.T) {
	h := newGalleryHarness(t)
	row := h.add(t, strings.Repeat("x", 200))
	if got := len([]rune(row.Attribution)); got != maxAttributionRunes {
		t.Fatalf("attribution runes = %d, want %d", got, maxAttributionRunes)
	}
}

func TestAddImageRejectsUnknownAndMergedDrinks(t *testing.T) {
	h := newGalleryHarness(t)
	if _, err := h.svc.AddImage(h.ctx, h.media, h.user.ID, 999999,
		bytes.NewReader(galleryPNG(t)), "soda.png", "image/png", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown drink err = %v, want ErrNotFound", err)
	}

	merged, err := h.db.Client.Drink.Create().
		SetName("Old Cola").SetSlug("old-cola").SetStatus(drink.StatusMerged).Save(h.ctx)
	if err != nil {
		t.Fatalf("merged drink: %v", err)
	}
	if _, err := h.svc.AddImage(h.ctx, h.media, h.user.ID, merged.ID,
		bytes.NewReader(galleryPNG(t)), "soda.png", "image/png", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("merged drink err = %v, want ErrNotFound", err)
	}
	if _, err := h.svc.Gallery(h.ctx, merged.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("merged gallery err = %v, want ErrNotFound", err)
	}
}

func TestAddImageRejectsNonImage(t *testing.T) {
	h := newGalleryHarness(t)
	_, err := h.svc.AddImage(h.ctx, h.media, h.user.ID, h.drink.ID,
		strings.NewReader("not an image"), "note.txt", "text/plain", "")
	if !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("err = %v, want ErrInvalidImage", err)
	}
}

func TestSetPrimaryIsAdminOnlyAndSingle(t *testing.T) {
	h := newGalleryHarness(t)
	a := h.add(t, "")
	b := h.add(t, "")

	if err := h.svc.SetPrimary(h.ctx, false, a.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin err = %v, want ErrForbidden", err)
	}
	if err := h.svc.SetPrimary(h.ctx, true, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown image err = %v, want ErrNotFound", err)
	}
	if err := h.svc.SetPrimary(h.ctx, true, a.ID); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	if err := h.svc.SetPrimary(h.ctx, true, b.ID); err != nil {
		t.Fatalf("switch primary: %v", err)
	}

	images, err := h.svc.Gallery(h.ctx, h.drink.ID)
	if err != nil {
		t.Fatalf("gallery: %v", err)
	}
	primaries := 0
	for _, img := range images {
		if img.IsPrimary {
			primaries++
			if img.ID != b.ID {
				t.Fatalf("primary = %d, want %d", img.ID, b.ID)
			}
		}
	}
	if primaries != 1 {
		t.Fatalf("primaries = %d, want 1", primaries)
	}

	if err := h.svc.ClearPrimary(h.ctx, false, h.drink.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin clear err = %v, want ErrForbidden", err)
	}
	if err := h.svc.ClearPrimary(h.ctx, true, h.drink.ID); err != nil {
		t.Fatalf("clear primary: %v", err)
	}
	images, _ = h.svc.Gallery(h.ctx, h.drink.ID)
	for _, img := range images {
		if img.IsPrimary {
			t.Fatal("primary was not cleared")
		}
	}
}

func TestSetPrimaryConcurrentInvariant(t *testing.T) {
	h := newGalleryHarness(t)
	rows := make([]*ent.DrinkImage, 0, 6)
	for i := 0; i < 6; i++ {
		rows = append(rows, h.add(t, ""))
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(rows))
	for _, row := range rows {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			errs <- h.svc.SetPrimary(context.Background(), true, id)
		}(row.ID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent set primary: %v", err)
		}
	}

	images, err := h.svc.Gallery(context.Background(), h.drink.ID)
	if err != nil {
		t.Fatalf("gallery: %v", err)
	}
	primaries := 0
	for _, img := range images {
		if img.IsPrimary {
			primaries++
		}
	}
	if primaries != 1 {
		t.Fatalf("primaries = %d, want exactly 1", primaries)
	}
}

func TestDeleteImageIsAdminOnlyAndRemovesMedia(t *testing.T) {
	h := newGalleryHarness(t)
	row := h.add(t, "")
	mediaID := strings.TrimPrefix(row.URL, "/media/")
	stored, err := h.db.Client.Media.Get(h.ctx, mediaID)
	if err != nil {
		t.Fatalf("media row: %v", err)
	}
	fullPath := filepath.Join(h.mediaD, filepath.FromSlash(stored.Path))
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("stored file: %v", err)
	}

	if err := h.svc.DeleteImage(h.ctx, false, row.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-admin err = %v, want ErrForbidden", err)
	}
	if _, err := h.db.Client.DrinkImage.Get(h.ctx, row.ID); err != nil {
		t.Fatalf("image removed by non-admin: %v", err)
	}

	if err := h.svc.DeleteImage(h.ctx, true, row.ID); err != nil {
		t.Fatalf("delete image: %v", err)
	}
	if _, err := h.db.Client.DrinkImage.Get(h.ctx, row.ID); !ent.IsNotFound(err) {
		t.Fatalf("image row still present: %v", err)
	}
	if _, err := h.db.Client.Media.Get(h.ctx, mediaID); !ent.IsNotFound(err) {
		t.Fatalf("media row still present: %v", err)
	}
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Fatalf("media file still present: %v", err)
	}
	if _, err := h.db.Client.Drink.Get(h.ctx, h.drink.ID); err != nil {
		t.Fatalf("drink was deleted: %v", err)
	}
	if err := h.svc.DeleteImage(h.ctx, true, row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
}
