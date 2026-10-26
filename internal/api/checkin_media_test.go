package api

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"sodapop/ent/checkin"
)

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("jpeg: %v", err)
	}
	return buf.Bytes()
}

type checkinUpload struct {
	fields    map[string]string
	fileField string
	filename  string
	data      []byte
}

func doCheckinUpload(t *testing.T, h *harness, token string, up checkinUpload) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for key, value := range up.fields {
		if err := mw.WriteField(key, value); err != nil {
			t.Fatalf("field %s: %v", key, err)
		}
	}
	if up.data != nil {
		fw, err := mw.CreateFormFile(up.fileField, up.filename)
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		if _, err := fw.Write(up.data); err != nil {
			t.Fatalf("write image: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/checkins", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func TestCreateCheckinJSONUnchanged(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "JSON Cola", "json-cola", nil)

	rec := h.do(t, http.MethodPost, "/api/v1/checkins",
		`{"drink_id":`+strconv.Itoa(drink.ID)+`,"rating":4.25,"serving":"can","note":"crisp"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("json create: %d %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	if data["created"] != true || data["rating"].(float64) != 17 {
		t.Fatalf("json data = %+v", data)
	}
	if _, ok := data["photo_media_id"]; ok {
		t.Fatalf("json response unexpectedly has photo_media_id: %+v", data)
	}
}

func TestCreateCheckinMultipartWithPhoto(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Photo Cola", "photo-cola", nil)

	rec := doCheckinUpload(t, h, h.token, checkinUpload{
		fields: map[string]string{
			"drink_id": strconv.Itoa(drink.ID),
			"rating":   "4.25",
			"serving":  "bottle",
			"note":     "with a photo",
		},
		fileField: "image",
		filename:  "cola.jpg",
		data:      jpegBytes(t),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("multipart create: %d %s", rec.Code, rec.Body.String())
	}
	data := dataMap(t, rec)
	photoID, _ := data["photo_media_id"].(string)
	if photoID == "" {
		t.Fatalf("missing photo_media_id: %s", rec.Body.String())
	}
	if data["rating"].(float64) != 17 {
		t.Fatalf("rating = %v, want 17", data["rating"])
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/checkins/"+data["id"].(string) {
		t.Fatalf("Location = %q", loc)
	}

	// The detail read exposes the stored photo.
	rec = h.do(t, http.MethodGet, "/api/v1/checkins/"+data["id"].(string), "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body.String())
	}
	detail := dataMap(t, rec)
	photo, _ := detail["photo"].(map[string]any)
	if photo == nil || photo["id"] != photoID || photo["mime"] != "image/jpeg" {
		t.Fatalf("detail photo = %+v, want id %s", photo, photoID)
	}
}

func TestCreateCheckinMultipartPhotoAliasAndMissingImage(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Alias Cola", "alias-cola", nil)

	rec := doCheckinUpload(t, h, h.token, checkinUpload{
		fields:    map[string]string{"drink_id": strconv.Itoa(drink.ID), "visibility": "public"},
		fileField: "photo",
		filename:  "alias.jpg",
		data:      jpegBytes(t),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("photo alias: %d %s", rec.Code, rec.Body.String())
	}
	if dataMap(t, rec)["photo_media_id"] == nil {
		t.Fatalf("photo alias did not store media: %s", rec.Body.String())
	}

	rec = doCheckinUpload(t, h, h.token, checkinUpload{
		fields: map[string]string{"drink_id": strconv.Itoa(drink.ID), "note": "text only"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("missing image: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := dataMap(t, rec)["photo_media_id"]; ok {
		t.Fatalf("text-only check-in has photo_media_id: %s", rec.Body.String())
	}
}

func TestCreateCheckinMultipartRejectsBadAndOversizedImages(t *testing.T) {
	h := newHarness(t)
	drink := newDrink(t, h, "Bad Photo Cola", "bad-photo-cola", nil)
	fields := map[string]string{"drink_id": strconv.Itoa(drink.ID)}

	rec := doCheckinUpload(t, h, h.token, checkinUpload{
		fields: fields, fileField: "image", filename: "note.txt", data: []byte("this is not an image"),
	})
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("bad mime: %d %s", rec.Code, rec.Body.String())
	}
	if got := h.server.DB.Client.Checkin.Query().CountX(context.Background()); got != 0 {
		t.Fatalf("bad upload created %d check-ins", got)
	}

	// A valid image header past the 12 MiB photo cap reports 413.
	big := append(jpegBytes(t), make([]byte, 13<<20)...)
	rec = doCheckinUpload(t, h, h.token, checkinUpload{
		fields: fields, fileField: "image", filename: "big.jpg", data: big,
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized image: %d %s", rec.Code, rec.Body.String())
	}
	if got := h.server.DB.Client.Media.Query().CountX(context.Background()); got != 0 {
		t.Fatalf("oversized upload left %d media rows", got)
	}
	if got := h.server.DB.Client.Checkin.Query().CountX(context.Background()); got != 0 {
		t.Fatalf("oversized upload created %d check-ins", got)
	}
}

func TestCreateCheckinMultipartIdempotencyDoesNotDoubleCreate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	drink := newDrink(t, h, "Idem Cola", "idem-cola", nil)
	fields := map[string]string{
		"drink_id":        strconv.Itoa(drink.ID),
		"idempotency_key": "checkin-key-1",
	}

	first := doCheckinUpload(t, h, h.token, checkinUpload{
		fields: fields, fileField: "image", filename: "first.jpg", data: jpegBytes(t),
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	firstPhoto := dataMap(t, first)["photo_media_id"]

	second := doCheckinUpload(t, h, h.token, checkinUpload{
		fields: fields, fileField: "image", filename: "second.jpg", data: jpegBytes(t),
	})
	if second.Code != http.StatusOK {
		t.Fatalf("replay: %d %s", second.Code, second.Body.String())
	}
	replay := dataMap(t, second)
	if replay["created"] != false || replay["id"] != dataMap(t, first)["id"] {
		t.Fatalf("replay data = %+v", replay)
	}
	if replay["photo_media_id"] != firstPhoto {
		t.Fatalf("replay photo = %v, want original %v", replay["photo_media_id"], firstPhoto)
	}

	if got := h.server.DB.Client.Checkin.Query().
		Where(checkin.IdempotencyKey("checkin-key-1")).CountX(ctx); got != 1 {
		t.Fatalf("idempotency key has %d check-ins, want 1", got)
	}
	// The replay upload is discarded instead of becoming an orphan.
	if got := h.server.DB.Client.Media.Query().CountX(ctx); got != 1 {
		t.Fatalf("media rows = %d, want 1", got)
	}
}
