package web

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"sodapop/ent"
	"sodapop/ent/report"
	"sodapop/ent/user"
	"sodapop/internal/media"
)

func newGalleryWebHarness(t *testing.T) *v12Harness {
	t.Helper()
	h := newV12Harness(t)
	h.srv.Cfg.DataDir = t.TempDir()
	h.srv.Media = media.New(h.db, h.srv.Cfg)
	return h
}

func (h *v12Harness) galleryDrink(t *testing.T, name, slug string) *ent.Drink {
	t.Helper()
	d, err := h.db.Client.Drink.Create().SetName(name).SetSlug(slug).Save(context.Background())
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	return d
}

func galleryImagePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 20, 14))
	for y := 0; y < 14; y++ {
		for x := 0; x < 20; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 9), G: uint8(y * 5), B: 0x66, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func (h *v12Harness) galleryMultipart(t *testing.T, target string, u *ent.User, fields map[string]string, filename string, data []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if fields == nil {
		fields = map[string]string{}
	}
	if _, ok := fields["csrf_token"]; !ok {
		fields["csrf_token"] = "test-csrf"
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	if filename != "" {
		fw, err := mw.CreateFormFile("image", filename)
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	ctx := context.WithValue(req.Context(), userKey{}, u)
	ctx = context.WithValue(ctx, sessionKey{}, &ent.Session{CsrfToken: "test-csrf"})
	return req.WithContext(ctx)
}

func (h *v12Harness) uploadGalleryImage(t *testing.T, u *ent.User, slug, filename string, data []byte, attribution string) *httptest.ResponseRecorder {
	t.Helper()
	fields := map[string]string{"attribution": attribution}
	req := h.galleryMultipart(t, "/drinks/"+slug+"/images", u, fields, filename, data)
	req.SetPathValue("slug", slug)
	rec := httptest.NewRecorder()
	h.srv.drinkImageUpload(rec, req)
	return rec
}

func TestGalleryUploadAndValidation(t *testing.T) {
	h := newGalleryWebHarness(t)
	uploader := h.verifiedUser(t, "uploader", user.RoleUser)
	h.galleryDrink(t, "Cherry Cola", "cherry-cola")

	rec := h.uploadGalleryImage(t, uploader, "cherry-cola", "soda.png", galleryImagePNG(t), "  Cellar shot  ")
	if rec.Code != http.StatusFound {
		t.Fatalf("upload status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/drinks/cherry-cola#gallery" {
		t.Fatalf("redirect = %q", loc)
	}
	row, err := h.db.Client.DrinkImage.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("image row: %v", err)
	}
	if row.Attribution != "Cellar shot" || row.IsPrimary {
		t.Fatalf("image row = %+v", row)
	}

	rec = h.uploadGalleryImage(t, uploader, "cherry-cola", "note.txt", []byte("not an image"), "")
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-image status = %d, want 415: %s", rec.Code, rec.Body.String())
	}
	rec = h.uploadGalleryImage(t, uploader, "missing", "soda.png", galleryImagePNG(t), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown drink status = %d, want 404", rec.Code)
	}
	rec = h.uploadGalleryImage(t, uploader, "cherry-cola", "", nil, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing file status = %d, want 400", rec.Code)
	}
}

func TestGalleryUploadTooLarge(t *testing.T) {
	h := newGalleryWebHarness(t)
	uploader := h.verifiedUser(t, "uploader", user.RoleUser)
	h.galleryDrink(t, "Cherry Cola", "cherry-cola")

	oversized := bytes.Repeat([]byte{0}, (25<<20)+8192)
	rec := h.uploadGalleryImage(t, uploader, "cherry-cola", "huge.png", oversized, "")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d, want 413: %s", rec.Code, rec.Body.String())
	}
}

func (h *v12Harness) galleryRow(t *testing.T, drinkID int, primary bool) *ent.DrinkImage {
	t.Helper()
	row, err := h.db.Client.DrinkImage.Create().
		SetDrinkID(drinkID).SetURL("/media/0123456789abcdef0123456789abcdef").
		SetIsPrimary(primary).Save(context.Background())
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	return row
}

func (h *v12Harness) postGalleryAction(t *testing.T, u *ent.User, handler http.HandlerFunc, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := h.request(http.MethodPost, "/drinks/images/"+id+"/action", u, h.form())
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestGallerySetPrimaryIsModeratorOnly(t *testing.T) {
	h := newGalleryWebHarness(t)
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	moderator := h.verifiedUser(t, "moderator", user.RoleModerator)
	member := h.verifiedUser(t, "member", user.RoleUser)
	d := h.galleryDrink(t, "Cherry Cola", "cherry-cola")
	row := h.galleryRow(t, d.ID, false)

	rec := h.postGalleryAction(t, member, h.srv.drinkImageSetPrimary, strconv.Itoa(row.ID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member primary status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	got, _ := h.db.Client.DrinkImage.Get(context.Background(), row.ID)
	if got.IsPrimary {
		t.Fatal("member promoted an image")
	}

	rec = h.postGalleryAction(t, moderator, h.srv.drinkImageSetPrimary, strconv.Itoa(row.ID))
	if rec.Code != http.StatusFound {
		t.Fatalf("moderator primary status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	got, _ = h.db.Client.DrinkImage.Get(context.Background(), row.ID)
	if !got.IsPrimary {
		t.Fatal("moderator promotion did not stick")
	}

	rec = h.postGalleryAction(t, admin, h.srv.drinkImageSetPrimary, strconv.Itoa(row.ID))
	if rec.Code != http.StatusFound {
		t.Fatalf("admin primary status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/drinks/cherry-cola#gallery" {
		t.Fatalf("redirect = %q", loc)
	}
	got, _ = h.db.Client.DrinkImage.Get(context.Background(), row.ID)
	if !got.IsPrimary {
		t.Fatal("admin promotion did not stick")
	}

	rec = h.postGalleryAction(t, admin, h.srv.drinkImageSetPrimary, "999999")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown image status = %d, want 404", rec.Code)
	}
}

func TestGalleryDeleteIsModeratorOnly(t *testing.T) {
	h := newGalleryWebHarness(t)
	moderator := h.verifiedUser(t, "moderator", user.RoleModerator)
	member := h.verifiedUser(t, "member", user.RoleUser)
	d := h.galleryDrink(t, "Cherry Cola", "cherry-cola")
	row := h.galleryRow(t, d.ID, false)

	rec := h.postGalleryAction(t, member, h.srv.drinkImageDelete, strconv.Itoa(row.ID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member delete status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if _, err := h.db.Client.DrinkImage.Get(context.Background(), row.ID); err != nil {
		t.Fatalf("member removed image: %v", err)
	}

	rec = h.postGalleryAction(t, moderator, h.srv.drinkImageDelete, strconv.Itoa(row.ID))
	if rec.Code != http.StatusFound {
		t.Fatalf("moderator delete status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if _, err := h.db.Client.DrinkImage.Get(context.Background(), row.ID); err == nil {
		t.Fatal("moderator delete left the image row")
	}
	if _, err := h.db.Client.Drink.Get(context.Background(), d.ID); err != nil {
		t.Fatalf("drink was deleted: %v", err)
	}
}

func TestGalleryPageData(t *testing.T) {
	h := newGalleryWebHarness(t)
	verified := h.verifiedUser(t, "verified", user.RoleAdmin)
	unverified := h.user(t, "unverified")
	d := h.galleryDrink(t, "Cherry Cola", "cherry-cola")
	h.galleryRow(t, d.ID, true)

	g := h.srv.GalleryPageData(context.Background(), d.ID, verified)
	if len(g.Images) != 1 || !g.Images[0].IsPrimary {
		t.Fatalf("gallery = %+v", g.Images)
	}
	if !g.CanUpload || !g.CanModerate || g.GalleryError != "" {
		t.Fatalf("admin gallery data = %+v", g)
	}
	g = h.srv.GalleryPageData(context.Background(), d.ID, h.verifiedUser(t, "mod", user.RoleModerator))
	if !g.CanUpload || !g.CanModerate {
		t.Fatalf("moderator gallery data = %+v", g)
	}
	g = h.srv.GalleryPageData(context.Background(), d.ID, unverified)
	if g.CanUpload || g.CanModerate {
		t.Fatalf("unverified permissions = %+v", g)
	}
	g = h.srv.GalleryPageData(context.Background(), 999999, verified)
	if g.GalleryError == "" || len(g.Images) != 0 {
		t.Fatalf("missing drink gallery = %+v", g)
	}
}

func TestReportSubmitDrinkImage(t *testing.T) {
	h := newGalleryWebHarness(t)
	reporter := h.verifiedUser(t, "reporter", user.RoleUser)
	d := h.galleryDrink(t, "Cherry Cola", "cherry-cola")
	row := h.galleryRow(t, d.ID, false)

	rec := h.submitReport(t, reporter, url.Values{
		"entity": {"drink_image"}, "entity_ref": {strconv.Itoa(row.ID)}, "reason": {"inappropriate"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("report status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	saved, err := h.db.Client.Report.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("report row: %v", err)
	}
	if saved.Entity != report.EntityDrinkImage || saved.EntityRef != strconv.Itoa(row.ID) || saved.EntityID != 0 {
		t.Fatalf("report = %+v", saved)
	}

	before := h.reportCount(t)
	rec = h.submitReport(t, reporter, url.Values{
		"entity": {"drink_image"}, "entity_ref": {"999999"}, "reason": {"other"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown image status = %d, want 404", rec.Code)
	}
	rec = h.submitReport(t, reporter, url.Values{
		"entity": {"drink_image"}, "entity_ref": {"not-a-number"}, "reason": {"other"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad ref status = %d, want 400", rec.Code)
	}
	if after := h.reportCount(t); after != before {
		t.Fatalf("reports created = %d, want 0", after-before)
	}
}

func TestAdminReportRemoveDrinkImageKeepsDrink(t *testing.T) {
	h := newGalleryWebHarness(t)
	admin := h.verifiedUser(t, "admin", user.RoleAdmin)
	reporter := h.verifiedUser(t, "reporter", user.RoleUser)
	d := h.galleryDrink(t, "Cherry Cola", "cherry-cola")
	row := h.galleryRow(t, d.ID, false)
	rep := h.newReport(t, reporter, report.EntityDrinkImage, strconv.Itoa(row.ID))

	rec := h.adminRemoveRequest(t, admin, rep.ID)
	if rec.Code != http.StatusFound {
		t.Fatalf("remove status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	if _, err := h.db.Client.DrinkImage.Get(context.Background(), row.ID); err == nil {
		t.Fatal("image row still present")
	}
	if _, err := h.db.Client.Drink.Get(context.Background(), d.ID); err != nil {
		t.Fatalf("drink was deleted: %v", err)
	}
	saved, err := h.db.Client.Report.Get(context.Background(), rep.ID)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if saved.Status != report.StatusResolved {
		t.Fatalf("report status = %q, want resolved", saved.Status)
	}
}
