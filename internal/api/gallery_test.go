package api

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/user"
)

func newGalleryAPIHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	mux := http.NewServeMux()
	h.server.Register(mux)
	h.mux = mux
	return h
}

func apiGalleryPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 13), G: uint8(y * 3), B: 0x44, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func (h *harness) apiMultipart(t *testing.T, method, path, attribution, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if attribution != "" {
		_ = mw.WriteField("attribution", attribution)
	}
	if filename != "" {
		fw, err := mw.CreateFormFile("image", filename)
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		_, _ = fw.Write(data)
	}
	_ = mw.Close()
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.token)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func (h *harness) galleryDrink(t *testing.T, slug string) *ent.Drink {
	t.Helper()
	d, err := h.server.DB.Client.Drink.Create().SetName("Gallery Cola").SetSlug(slug).Save(context.Background())
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	return d
}

func TestGalleryAPIUploadAndList(t *testing.T) {
	h := newGalleryAPIHarness(t)
	h.galleryDrink(t, "gallery-cola")

	rec := h.do(t, http.MethodGet, "/api/v1/drinks/gallery-cola/images", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "total") {
		t.Fatalf("list leaks a total: %s", rec.Body.String())
	}

	rec = h.apiMultipart(t, http.MethodPost, "/api/v1/drinks/gallery-cola/images", "  Shelf shot  ", "soda.png", apiGalleryPNG(t))
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"attribution":"Shelf shot"`) {
		t.Fatalf("upload body = %s", rec.Body.String())
	}
	n, err := h.server.DB.Client.DrinkImage.Query().Count(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("image count = %d, %v", n, err)
	}

	rec = h.do(t, http.MethodGet, "/api/v1/drinks/missing/images", "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown drink status = %d, want 404", rec.Code)
	}
	rec = h.apiMultipart(t, http.MethodPost, "/api/v1/drinks/gallery-cola/images", "", "note.txt", []byte("not an image"))
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-image status = %d, want 415: %s", rec.Code, rec.Body.String())
	}
	rec = h.apiMultipart(t, http.MethodPost, "/api/v1/drinks/gallery-cola/images", "", "huge.png", bytes.Repeat([]byte{0}, (25<<20)+8192))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d, want 413: %s", rec.Code, rec.Body.String())
	}
}

func TestGalleryAPIPrimaryAndDeleteAllowModerators(t *testing.T) {
	h := newGalleryAPIHarness(t)
	d := h.galleryDrink(t, "gallery-cola")
	row, err := h.server.DB.Client.DrinkImage.Create().
		SetDrinkID(d.ID).SetURL("/media/0123456789abcdef0123456789abcdef").Save(context.Background())
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	path := "/api/v1/drinks/images/" + strconv.Itoa(row.ID)

	rec := h.do(t, http.MethodPost, path+"/primary", "", true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member primary status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodDelete, path, "", true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member delete status = %d, want 403: %s", rec.Code, rec.Body.String())
	}

	if _, err := h.server.DB.Client.User.UpdateOneID(h.alice).
		SetRole(user.RoleModerator).Save(context.Background()); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}
	rec = h.do(t, http.MethodPost, path+"/primary", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("moderator primary status = %d: %s", rec.Code, rec.Body.String())
	}
	got, err := h.server.DB.Client.DrinkImage.Get(context.Background(), row.ID)
	if err != nil || !got.IsPrimary {
		t.Fatalf("image primary = %+v, %v", got, err)
	}

	rec = h.do(t, http.MethodDelete, path, "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("moderator delete status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := h.server.DB.Client.DrinkImage.Get(context.Background(), row.ID); !ent.IsNotFound(err) {
		t.Fatalf("image still present: %v", err)
	}
	if _, err := h.server.DB.Client.Drink.Get(context.Background(), d.ID); err != nil {
		t.Fatalf("drink was deleted: %v", err)
	}
	rec = h.do(t, http.MethodDelete, "/api/v1/drinks/images/999999", "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown image status = %d, want 404", rec.Code)
	}
}
