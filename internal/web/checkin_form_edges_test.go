package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent/checkin"
	"sodapop/ent/checkintag"
	"sodapop/ent/user"
)

func TestCheckinSubmitIgnoresSelfAndDuplicateFriendTags(t *testing.T) {
	h := newGalleryWebHarness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	member := h.verifiedUser(t, "self-tagger", user.RoleUser)
	friend := h.verifiedUser(t, "duplicate-tag", user.RoleUser)
	h.acceptFriendship(t, member, friend)
	drink := h.galleryDrink(t, "Edge Cola", "edge-cola")

	fields := url.Values{}
	fields.Set("drink_id", strconv.Itoa(drink.ID))
	fields.Set("visibility", "public")
	fields.Set("idempotency_key", "self-and-duplicate-tags-1")
	fields.Add("friend_ids", member.ID) // self: ignored
	fields.Add("friend_ids", friend.ID)
	fields.Add("friend_ids", friend.ID) // duplicate: deduped

	rec := httptest.NewRecorder()
	h.srv.checkinSubmit(rec, checkinMultipartValues(t, member, fields))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	row, err := h.db.Client.Checkin.Query().
		Where(checkin.IdempotencyKey("self-and-duplicate-tags-1")).
		Only(context.Background())
	if err != nil {
		t.Fatalf("check-in: %v", err)
	}
	tags, err := h.db.Client.CheckinTag.Query().
		Where(checkintag.CheckinID(row.ID)).
		All(context.Background())
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	if len(tags) != 1 || tags[0].UserID != friend.ID {
		t.Fatalf("tags = %+v, want exactly the friend %s", tags, friend.ID)
	}

	// The tag survives the detail round-trip.
	req := h.request(http.MethodGet, "/checkins/"+row.ID, member, url.Values{})
	req.SetPathValue("id", row.ID)
	detail := httptest.NewRecorder()
	h.srv.checkinDetailPage(detail, req)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", detail.Code, detail.Body.String())
	}
	if !strings.Contains(detail.Body.String(), `href="/u/duplicate-tag"`) {
		t.Fatalf("detail missing tagged friend: %s", detail.Body.String())
	}
}

func TestCheckinSubmitRejectsBadLocationStrings(t *testing.T) {
	cases := []struct {
		name string
		lat  string
		lng  string
	}{
		{"non-numeric lat", "north-ish", "-0.12"},
		{"non-numeric lng", "51.5", "west-ish"},
		{"latitude out of range", "91", "-0.12"},
		{"longitude out of range", "51.5", "181"},
		{"only one coordinate", "51.5", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newGalleryWebHarness(t)
			member := h.verifiedUser(t, "bad-location", user.RoleUser)
			drink := h.galleryDrink(t, "Location Cola", "location-cola")

			fields := url.Values{}
			fields.Set("drink_id", strconv.Itoa(drink.ID))
			fields.Set("visibility", "public")
			fields.Set("idempotency_key", "bad-location-"+tc.name)
			fields.Set("lat", tc.lat)
			fields.Set("lng", tc.lng)

			rec := httptest.NewRecorder()
			h.srv.checkinSubmit(rec, checkinMultipartValues(t, member, fields))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "that location could not be read") {
				t.Fatalf("missing friendly location error: %s", rec.Body.String())
			}
			if n, _ := h.db.Client.Checkin.Query().
				Where(checkin.IdempotencyKey("bad-location-" + tc.name)).Count(context.Background()); n != 0 {
				t.Fatalf("bad location wrote %d check-ins", n)
			}
		})
	}
}

func TestCheckinSubmitInlineVenueMissingOrInvalidName(t *testing.T) {
	t.Run("blank name is treated as no inline venue", func(t *testing.T) {
		h := newGalleryWebHarness(t)
		member := h.verifiedUser(t, "blank-venue", user.RoleUser)
		drink := h.galleryDrink(t, "Blank Venue Cola", "blank-venue-cola")

		fields := url.Values{}
		fields.Set("drink_id", strconv.Itoa(drink.ID))
		fields.Set("visibility", "public")
		fields.Set("idempotency_key", "blank-inline-venue-1")
		fields.Set("new_venue_name", "   ")

		rec := httptest.NewRecorder()
		h.srv.checkinSubmit(rec, checkinMultipartValues(t, member, fields))
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		row, err := h.db.Client.Checkin.Query().
			Where(checkin.IdempotencyKey("blank-inline-venue-1")).Only(context.Background())
		if err != nil {
			t.Fatalf("check-in: %v", err)
		}
		if row.VenueID != nil {
			t.Fatalf("blank venue name attached venue %d", *row.VenueID)
		}
	})

	t.Run("overlong name is rejected without writing the check-in", func(t *testing.T) {
		h := newGalleryWebHarness(t)
		member := h.verifiedUser(t, "long-venue", user.RoleUser)
		drink := h.galleryDrink(t, "Long Venue Cola", "long-venue-cola")

		fields := url.Values{}
		fields.Set("drink_id", strconv.Itoa(drink.ID))
		fields.Set("visibility", "public")
		fields.Set("idempotency_key", "invalid-inline-venue-1")
		fields.Set("new_venue_name", strings.Repeat("x", 121))

		rec := httptest.NewRecorder()
		h.srv.checkinSubmit(rec, checkinMultipartValues(t, member, fields))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "could not add that venue") {
			t.Fatalf("missing venue error: %s", rec.Body.String())
		}
		if n, _ := h.db.Client.Checkin.Query().
			Where(checkin.IdempotencyKey("invalid-inline-venue-1")).Count(context.Background()); n != 0 {
			t.Fatalf("invalid venue wrote %d check-ins", n)
		}
	})
}
