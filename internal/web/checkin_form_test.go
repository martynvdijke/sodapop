package web

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/checkintag"
	"sodapop/ent/friendship"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
	"sodapop/internal/social"
)

// checkinMultipartValues builds the composer form with repeated fields, which
// the friend picker needs. The existing checkinMediaMultipart helper only
// carries one value per name; this one keeps every friend_ids entry.
func checkinMultipartValues(t *testing.T, u *ent.User, fields url.Values) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if fields.Get("csrf_token") == "" {
		fields.Set("csrf_token", "test-csrf")
	}
	for key, values := range fields {
		for _, value := range values {
			if err := mw.WriteField(key, value); err != nil {
				t.Fatalf("write field %s: %v", key, err)
			}
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

func (h *v12Harness) acceptFriendship(t *testing.T, a, b *ent.User) {
	t.Helper()
	x, y := a.ID, b.ID
	if x > y {
		x, y = y, x
	}
	if _, err := h.db.Client.Friendship.Create().
		SetUserA(x).SetUserB(y).SetStatus(friendship.StatusAccepted).
		SetRequestedBy(a.ID).Save(context.Background()); err != nil {
		t.Fatalf("friendship: %v", err)
	}
}

func TestCheckinPageRendersFriendsVenueSearchAndLocation(t *testing.T) {
	h := newGalleryWebHarness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	member := h.verifiedUser(t, "composer", user.RoleUser)
	friend := h.verifiedUser(t, "tagme", user.RoleUser)
	h.acceptFriendship(t, member, friend)
	h.galleryDrink(t, "Cherry Cola", "cherry-cola")

	req := h.request(http.MethodGet, "/checkins/new?slug=cherry-cola", member, url.Values{})
	rec := httptest.NewRecorder()
	h.srv.checkinPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="friend_ids"`,
		`value="` + friend.ID + `"`,
		`name="venue_q"`,
		`data-venue-select`,
		`name="lat"`,
		`name="lng"`,
		`data-locate`,
		`name="new_venue_name"`,
		`capture="environment"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("composer missing %q", want)
		}
	}
}

func TestCheckinPageVenueSearchIsServerRendered(t *testing.T) {
	h := newGalleryWebHarness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	member := h.verifiedUser(t, "searcher", user.RoleUser)
	h.galleryDrink(t, "Cola", "cola")
	ctx := context.Background()
	if _, err := h.db.Client.Venue.Create().
		SetName("Near Cafe").SetSlug("near-cafe").Save(ctx); err != nil {
		t.Fatalf("near venue: %v", err)
	}
	if _, err := h.db.Client.Venue.Create().
		SetName("Far Cafe").SetSlug("far-cafe").Save(ctx); err != nil {
		t.Fatalf("far venue: %v", err)
	}

	req := h.request(http.MethodGet, "/checkins/new?slug=cola&venue_q=Near", member, url.Values{})
	rec := httptest.NewRecorder()
	h.srv.checkinPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Near Cafe") {
		t.Fatalf("server-side venue search missing the match: %s", body)
	}
	if strings.Contains(body, "Far Cafe") {
		t.Fatalf("server-side venue search returned a non-match: %s", body)
	}
}

func TestCheckinSubmitStoresLocationAndTaggedFriends(t *testing.T) {
	h := newGalleryWebHarness(t)
	member := h.verifiedUser(t, "tagger", user.RoleUser)
	first := h.verifiedUser(t, "first", user.RoleUser)
	second := h.verifiedUser(t, "second", user.RoleUser)
	h.acceptFriendship(t, member, first)
	h.acceptFriendship(t, member, second)
	drink := h.galleryDrink(t, "Ginger Beer", "ginger-beer")

	fields := url.Values{}
	fields.Set("drink_id", strconv.Itoa(drink.ID))
	fields.Set("serving", "can")
	fields.Set("visibility", "public")
	fields.Set("idempotency_key", "tagged-checkin-1")
	fields.Set("lat", "51.500")
	fields.Set("lng", "-0.120")
	fields.Add("friend_ids", first.ID)
	fields.Add("friend_ids", second.ID)

	rec := httptest.NewRecorder()
	h.srv.checkinSubmit(rec, checkinMultipartValues(t, member, fields))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	row, err := h.db.Client.Checkin.Query().
		Where(checkin.IdempotencyKey("tagged-checkin-1")).
		Only(context.Background())
	if err != nil {
		t.Fatalf("check-in: %v", err)
	}
	if row.Lat == nil || *row.Lat != 51.5 || row.Lng == nil || *row.Lng != -0.12 {
		t.Fatalf("stored coords = %v,%v, want 51.5,-0.12", row.Lat, row.Lng)
	}
	tags, err := h.db.Client.CheckinTag.Query().
		Where(checkintag.CheckinID(row.ID)).
		All(context.Background())
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("tagged friends = %d, want 2", len(tags))
	}
}

func TestCheckinSubmitScanFlowLandsOnNewCheckin(t *testing.T) {
	h := newGalleryWebHarness(t)
	member := h.verifiedUser(t, "scan-flow", user.RoleUser)
	drink := h.galleryDrink(t, "Cream Soda", "cream-soda")

	fields := url.Values{}
	fields.Set("drink_id", strconv.Itoa(drink.ID))
	fields.Set("visibility", "public")
	fields.Set("idempotency_key", "scan-flow-1")
	fields.Set("next", "checkin")

	rec := httptest.NewRecorder()
	h.srv.checkinSubmit(rec, checkinMultipartValues(t, member, fields))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/checkins/") {
		t.Fatalf("scan redirect = %q, want the new check-in", location)
	}
}

func TestCheckinSubmitRejectsNonFriendTag(t *testing.T) {
	h := newGalleryWebHarness(t)
	member := h.verifiedUser(t, "lonely", user.RoleUser)
	stranger := h.verifiedUser(t, "stranger", user.RoleUser)
	drink := h.galleryDrink(t, "Cola", "cola")

	fields := url.Values{}
	fields.Set("drink_id", strconv.Itoa(drink.ID))
	fields.Set("visibility", "public")
	fields.Set("idempotency_key", "stranger-tag-1")
	fields.Add("friend_ids", stranger.ID)

	rec := httptest.NewRecorder()
	h.srv.checkinSubmit(rec, checkinMultipartValues(t, member, fields))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if n, _ := h.db.Client.Checkin.Query().
		Where(checkin.IdempotencyKey("stranger-tag-1")).Count(context.Background()); n != 0 {
		t.Fatalf("check-in written despite invalid tag: %d", n)
	}
}

func TestCheckinSubmitCreatesInlineVenue(t *testing.T) {
	h := newGalleryWebHarness(t)
	member := h.verifiedUser(t, "venue-maker", user.RoleUser)
	drink := h.galleryDrink(t, "Lemonade", "lemonade")

	fields := url.Values{}
	fields.Set("drink_id", strconv.Itoa(drink.ID))
	fields.Set("visibility", "public")
	fields.Set("idempotency_key", "new-venue-checkin-1")
	fields.Set("lat", "51.500")
	fields.Set("lng", "-0.120")
	fields.Set("new_venue_name", "The Corner Fridge")
	fields.Set("new_venue_city", "Leeds")
	fields.Set("new_venue_country", "United Kingdom")

	rec := httptest.NewRecorder()
	h.srv.checkinSubmit(rec, checkinMultipartValues(t, member, fields))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	row, err := h.db.Client.Checkin.Query().
		Where(checkin.IdempotencyKey("new-venue-checkin-1")).
		Only(context.Background())
	if err != nil {
		t.Fatalf("check-in: %v", err)
	}
	if row.VenueID == nil {
		t.Fatal("check-in did not attach the new venue")
	}
	v, err := h.db.Client.Venue.Get(context.Background(), *row.VenueID)
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	if v.Name != "The Corner Fridge" || v.City != "Leeds" {
		t.Fatalf("venue = %q / %q", v.Name, v.City)
	}
	if v.Lat != 51.5 || v.Lng != -0.12 {
		t.Fatalf("venue coords = %v,%v, want the location fix", v.Lat, v.Lng)
	}
}

func TestCheckinVenueOptionsJSONRanksNearbyFirst(t *testing.T) {
	h := newV12Harness(t)
	member := h.verifiedUser(t, "nearby-picker", user.RoleUser)
	ctx := context.Background()

	near, err := h.db.Client.Venue.Create().
		SetName("Near Cafe").SetSlug("near-cafe").SetLat(51.500).SetLng(-0.120).Save(ctx)
	if err != nil {
		t.Fatalf("near venue: %v", err)
	}
	far, err := h.db.Client.Venue.Create().
		SetName("Far Cafe").SetSlug("far-cafe").SetLat(51.600).SetLng(-0.120).Save(ctx)
	if err != nil {
		t.Fatalf("far venue: %v", err)
	}

	req := h.request(http.MethodGet, "/checkins/venues?lat=51.500&lng=-0.120", member, url.Values{})
	rec := httptest.NewRecorder()
	h.srv.checkinVenueOptionsJSON(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Venues []checkinVenueOption `json:"venues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(body.Venues) < 2 {
		t.Fatalf("venues = %+v", body.Venues)
	}
	if body.Venues[0].ID != near.ID {
		t.Fatalf("first venue = %d, want near %d", body.Venues[0].ID, near.ID)
	}
	if body.Venues[0].Distance == "" {
		t.Fatal("nearest venue has no distance label")
	}
	if body.Venues[0].ID == far.ID {
		t.Fatal("far venue ranked first")
	}
	if strings.Contains(rec.Body.String(), "51.5") {
		t.Fatalf("response leaked raw coordinates: %s", rec.Body.String())
	}
}

func TestCheckinDetailShowsTaggedFriendsAndMapLink(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	owner := h.verifiedUser(t, "detail-owner", user.RoleUser)
	friend := h.verifiedUser(t, "detail-friend", user.RoleUser)
	h.acceptFriendship(t, owner, friend)
	drink := h.galleryDrink(t, "Root Beer", "root-beer")
	c, err := h.db.Client.Checkin.Create().
		SetUserID(owner.ID).SetDrinkID(drink.ID).
		SetVisibility(checkin.VisibilityPublic).
		SetLat(51.5).SetLng(-0.12).Save(context.Background())
	if err != nil {
		t.Fatalf("check-in: %v", err)
	}
	if _, err := h.db.Client.CheckinTag.Create().
		SetCheckinID(c.ID).SetUserID(friend.ID).Save(context.Background()); err != nil {
		t.Fatalf("tag: %v", err)
	}

	req := h.request(http.MethodGet, "/checkins/"+c.ID, owner, url.Values{})
	req.SetPathValue("id", c.ID)
	rec := httptest.NewRecorder()
	h.srv.checkinDetailPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/u/detail-friend"`) {
		t.Fatalf("detail missing tagged friend link: %s", body)
	}
	if strings.Contains(body, "openstreetmap.org") {
		t.Fatalf("detail must not link raw coordinates: %s", body)
	}
}

func TestFeedItemShowsTaggedFriendsAndLocation(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	var buf bytes.Buffer
	err = renderer.pages["home"].ExecuteTemplate(&buf, "feed_item", social.FeedItem{
		ID:          "c1",
		Handle:      "owner",
		DisplayName: "Owner",
		DrinkName:   "Cola",
		DrinkSlug:   "cola",
		TaggedFriends: []social.FeedTaggedFriend{
			{Handle: "alice", DisplayName: "Alice"},
			{Handle: "bob", DisplayName: "Bob"},
		},
	})
	if err != nil {
		t.Fatalf("render feed item: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, "With") || !strings.Contains(body, `href="/u/alice"`) || !strings.Contains(body, " and ") {
		t.Fatalf("feed item missing tag list: %s", body)
	}
	if strings.Contains(body, "openstreetmap.org") {
		t.Fatalf("feed item must not link raw coordinates: %s", body)
	}

	// The profile page reuses feed_item through a wrapper because ProfileItem
	// does not carry the optional tag and location fields. It must still render.
	buf.Reset()
	err = renderer.pages["home"].ExecuteTemplate(&buf, "feed_item", profileFeedItem{
		ProfileItem: checkins.ProfileItem{
			ID: "p1", Handle: "owner", DisplayName: "Owner",
			DrinkName: "Cola", DrinkSlug: "cola",
		},
	})
	if err != nil {
		t.Fatalf("render profile feed item: %v", err)
	}
}

func TestSearchResultsOfferDirectLogAction(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer

	ctx := context.Background()
	if _, err := h.db.Client.Drink.Create().
		SetName("Cola Classic").SetSlug("cola-classic").Save(ctx); err != nil {
		t.Fatalf("drink: %v", err)
	}
	for _, target := range []string{"/search?q=cola", "/search?q=cola&type=all"} {
		rec := httptest.NewRecorder()
		h.srv.searchPage(rec, httptest.NewRequest(http.MethodGet, target, nil))
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d: %s", target, rec.Code, body)
		}
		if !strings.Contains(body, `href="/checkins/new?slug=cola-classic"`) {
			t.Errorf("%s: search results missing Log action: %s", target, body)
		}
	}
}
