package api

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"sodapop/ent/checkintag"
	"sodapop/ent/media"
	"sodapop/ent/notification"
)

func TestCheckinMultipartPhotoTagsLocationAndIdempotentRetry(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	drink := newDrink(t, h, "Combo Cola", "combo-cola", nil)
	friend := apiFriend(t, h, "combo-friend")
	fields := map[string]string{
		"drink_id":        strconv.Itoa(drink.ID),
		"lat":             "51.500",
		"lng":             "-0.120",
		"friend_ids":      friend.ID,
		"idempotency_key": "photo-tags-location-1",
	}

	first := doCheckinUpload(t, h, h.token, checkinUpload{
		fields: fields, fileField: "image", filename: "combo.jpg", data: jpegBytes(t),
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	created := dataMap(t, first)
	id, _ := created["id"].(string)
	photoID, _ := created["photo_media_id"].(string)
	if id == "" || photoID == "" {
		t.Fatalf("missing id/photo: %s", first.Body.String())
	}
	if created["lat"] != 51.5 || created["lng"] != -0.12 {
		t.Fatalf("coords = %v,%v", created["lat"], created["lng"])
	}
	if tags, _ := created["tagged_friends"].([]any); len(tags) != 1 {
		t.Fatalf("tagged_friends = %v", created["tagged_friends"])
	}

	stored, err := h.server.DB.Client.Media.Query().Where(media.ID(photoID)).Only(ctx)
	if err != nil {
		t.Fatalf("media row: %v", err)
	}
	if stored.UserID == nil || *stored.UserID != h.alice || stored.Kind != media.KindCheckin {
		t.Fatalf("media row = %+v, want check-in media owned by alice", stored)
	}

	// A retry with a different upload returns the original row and image, and
	// must neither duplicate tags nor leave the retry upload orphaned.
	second := doCheckinUpload(t, h, h.token, checkinUpload{
		fields: fields, fileField: "image", filename: "retry.jpg", data: jpegBytes(t),
	})
	if second.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", second.Code, second.Body.String())
	}
	replayed := dataMap(t, second)
	if replayed["id"] != id {
		t.Fatalf("retry id = %v, want %s", replayed["id"], id)
	}
	if replayed["photo_media_id"] != photoID {
		t.Fatalf("retry photo = %v, want original %s", replayed["photo_media_id"], photoID)
	}
	if tags, _ := replayed["tagged_friends"].([]any); len(tags) != 1 {
		t.Fatalf("retry tagged_friends = %v", replayed["tagged_friends"])
	}

	if n, err := h.server.DB.Client.CheckinTag.Query().Where(checkintag.CheckinID(id)).Count(ctx); err != nil || n != 1 {
		t.Fatalf("tags = %d err=%v, want 1", n, err)
	}
	if n, err := h.server.DB.Client.Media.Query().Count(ctx); err != nil || n != 1 {
		t.Fatalf("media rows = %d err=%v, want only the original", n, err)
	}
	if n, err := h.server.DB.Client.Notification.Query().
		Where(notification.KindEQ(notification.KindMention), notification.SubjectID(id)).Count(ctx); err != nil || n != 1 {
		t.Fatalf("mention notifications = %d err=%v, want 1", n, err)
	}

	// The detail read exposes the same combined fields.
	rec := h.do(t, http.MethodGet, "/api/v1/checkins/"+id, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body.String())
	}
	detail := dataMap(t, rec)
	if detail["lat"] != 51.5 || detail["lng"] != -0.12 {
		t.Fatalf("detail coords = %v,%v", detail["lat"], detail["lng"])
	}
	if tags, _ := detail["tagged_friends"].([]any); len(tags) != 1 {
		t.Fatalf("detail tagged_friends = %v", detail["tagged_friends"])
	}
	photo, _ := detail["photo"].(map[string]any)
	if photo == nil || photo["id"] != photoID {
		t.Fatalf("detail photo = %v, want %s", detail["photo"], photoID)
	}
}
