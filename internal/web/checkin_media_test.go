package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/media"
	"sodapop/ent/user"
)

// checkinMediaMultipart builds the web check-in form as a browser would when a
// photo is attached. The field name is part of the template contract.
func checkinMediaMultipart(t *testing.T, u *ent.User, fields map[string]string, filename string, data []byte) *http.Request {
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
		fw, err := mw.CreateFormFile("media", filename)
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
	req := httptest.NewRequest(http.MethodPost, "/checkins", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	ctx := context.WithValue(req.Context(), userKey{}, u)
	ctx = context.WithValue(ctx, sessionKey{}, &ent.Session{CsrfToken: "test-csrf"})
	return req.WithContext(ctx)
}

func TestCheckinSubmitStoresPhoto(t *testing.T) {
	h := newGalleryWebHarness(t)
	member := h.verifiedUser(t, "snapper", user.RoleUser)
	drink := h.galleryDrink(t, "Cherry Cola", "cherry-cola")

	req := checkinMediaMultipart(t, member, map[string]string{
		"drink_id":        strconv.Itoa(drink.ID),
		"serving":         "can",
		"visibility":      "friends",
		"idempotency_key": "photo-checkin-1",
	}, "cherry-cola.png", galleryImagePNG(t))
	rec := httptest.NewRecorder()
	h.srv.checkinSubmit(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("check-in with photo status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err := h.db.Client.Checkin.Query().
		Where(checkin.IdempotencyKey("photo-checkin-1")).
		Only(context.Background())
	if err != nil {
		t.Fatalf("check-in row: %v", err)
	}
	if row.PhotoMediaID == nil || *row.PhotoMediaID == "" {
		t.Fatal("check-in did not reference the uploaded photo")
	}
	stored, err := h.db.Client.Media.Query().
		Where(media.ID(*row.PhotoMediaID)).
		Only(context.Background())
	if err != nil {
		t.Fatalf("media row: %v", err)
	}
	if stored.Kind != media.KindCheckin || stored.UserID == nil || *stored.UserID != member.ID {
		t.Fatalf("media row = %+v, want check-in photo owned by %s", stored, member.ID)
	}
	if stored.Path == "" || stored.ThumbPath == "" {
		t.Fatalf("media files were not written: %+v", stored)
	}
}

// A check-in without a file must keep working; attaching a photo is optional.
func TestCheckinSubmitWithoutPhoto(t *testing.T) {
	h := newGalleryWebHarness(t)
	member := h.verifiedUser(t, "plain", user.RoleUser)
	drink := h.galleryDrink(t, "Root Beer", "root-beer")

	rec := httptest.NewRecorder()
	h.srv.checkinSubmit(rec, checkinMediaMultipart(t, member, map[string]string{
		"drink_id":        strconv.Itoa(drink.ID),
		"serving":         "can",
		"visibility":      "public",
		"idempotency_key": "no-photo-checkin-1",
	}, "", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("text-only check-in status = %d, want 302: %s", rec.Code, rec.Body.String())
	}
	row, err := h.db.Client.Checkin.Query().
		Where(checkin.IdempotencyKey("no-photo-checkin-1")).
		Only(context.Background())
	if err != nil {
		t.Fatalf("check-in row: %v", err)
	}
	if row.PhotoMediaID != nil {
		t.Fatalf("text-only check-in got photo %q", *row.PhotoMediaID)
	}
}
