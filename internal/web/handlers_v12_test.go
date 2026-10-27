package web

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/friendship"
	"sodapop/ent/list"
	"sodapop/ent/listitem"
	"sodapop/ent/notification"
	"sodapop/ent/venuesave"
	"sodapop/internal/auth"
	"sodapop/internal/badges"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/ratelimit"
	"sodapop/internal/saves"
	"sodapop/internal/social"
	"sodapop/internal/taste"
	"sodapop/internal/venues"
)

type v12Harness struct {
	srv    *Server
	db     *db.DB
	render *Renderer
}

func newV12Harness(t *testing.T) *v12Harness {
	t.Helper()
	cfg := &config.Config{
		AppName:         "Sodapop",
		Env:             "test",
		BaseURL:         "http://localhost",
		Version:         "test",
		SessionKey:      []byte("0123456789abcdef0123456789abcdef"),
		SessionTTL:      time.Hour,
		BcryptCost:      10,
		RateLimitPerMin: 120,
	}
	database, err := db.Init(filepath.Join(t.TempDir(), "v12.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	authSvc := auth.New(cfg, database)
	socialSvc := social.New(database)
	checkinSvc := checkins.New(database, socialSvc)
	srv := &Server{
		Cfg:      cfg,
		Auth:     authSvc,
		Catalog:  catalog.New(cfg, database),
		Checkins: checkinSvc,
		Saves:    saves.New(database, checkinSvc),
		Social:   socialSvc,
		Venues:   venues.New(database),
		Badges:   badges.New(database),
		Taste:    taste.New(database),
		DB:       database,
		Log:      log,
		Limiter:  ratelimit.New(),
	}
	return &v12Harness{srv: srv, db: database}
}

func (h *v12Harness) user(t *testing.T, handle string) *ent.User {
	t.Helper()
	u, err := h.db.Client.User.Create().
		SetHandle(handle).SetEmail(handle + "@example.com").SetDisplayName(strings.ToUpper(handle[:1]) + handle[1:]).
		Save(context.Background())
	if err != nil {
		t.Fatalf("user %s: %v", handle, err)
	}
	return u
}

func (h *v12Harness) request(method, target string, u *ent.User, form url.Values) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), userKey{}, u)
	ctx = context.WithValue(ctx, sessionKey{}, &ent.Session{CsrfToken: "test-csrf"})
	return req.WithContext(ctx)
}

func (h *v12Harness) form() url.Values {
	return url.Values{"csrf_token": {"test-csrf"}}
}

func (h *v12Harness) renderWith(t *testing.T, page, source string) *httptest.ResponseRecorder {
	t.Helper()
	tmpl := template.Must(template.New("base.html").Parse(source))
	errTmpl := template.Must(template.New("base.html").Parse(`ERR:{{.Message}}`))
	h.srv.Render = &Renderer{pages: map[string]*template.Template{page: tmpl, "error": errTmpl}}
	return httptest.NewRecorder()
}

func TestListEditValidationAndOwnership(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")
	other := h.user(t, "other")
	l, err := h.db.Client.List.Create().
		SetUserID(owner.ID).SetName("Fridge").SetSlug("fridge").SetVisibility(list.VisibilityPrivate).
		Save(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	tests := []struct {
		name       string
		actor      *ent.User
		form       url.Values
		wantStatus int
		wantName   string
		wantVis    list.Visibility
	}{
		{"owner updates", owner, url.Values{"name": {"Weekend"}, "description": {"Cold ones"}, "visibility": {"friends"}}, http.StatusFound, "Weekend", list.VisibilityFriends},
		{"invalid visibility", owner, url.Values{"name": {"Nope"}, "visibility": {"secret"}}, http.StatusBadRequest, "Weekend", list.VisibilityFriends},
		{"empty name", owner, url.Values{"name": {"   "}, "visibility": {"private"}}, http.StatusBadRequest, "Weekend", list.VisibilityFriends},
		{"too long name", owner, url.Values{"name": {strings.Repeat("a", 81)}, "visibility": {"private"}}, http.StatusBadRequest, "Weekend", list.VisibilityFriends},
		{"too long description", owner, url.Values{"name": {"Fine"}, "description": {strings.Repeat("b", 501)}, "visibility": {"private"}}, http.StatusBadRequest, "Weekend", list.VisibilityFriends},
		{"non-owner is not found", other, url.Values{"name": {"Hijack"}, "visibility": {"public"}}, http.StatusNotFound, "Weekend", list.VisibilityFriends},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := h.form()
			for key, values := range tt.form {
				form[key] = values
			}
			req := h.request(http.MethodPost, "/lists/"+l.ID+"/edit", tt.actor, form)
			req.SetPathValue("id", l.ID)
			rec := httptest.NewRecorder()
			h.srv.listEdit(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			got, err := h.db.Client.List.Get(context.Background(), l.ID)
			if err != nil {
				t.Fatalf("reload list: %v", err)
			}
			if got.Name != tt.wantName || got.Visibility != tt.wantVis {
				t.Fatalf("list = %q/%q, want %q/%q", got.Name, got.Visibility, tt.wantName, tt.wantVis)
			}
		})
	}
}

func TestListEditRefusesSystemList(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")
	system, err := h.db.Client.List.Create().SetUserID(owner.ID).SetName("Wishlist").SetSlug("wishlist").
		SetVisibility(list.VisibilityPrivate).SetIsSystem(true).Save(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	form := h.form()
	form.Set("name", "Renamed")
	form.Set("visibility", "public")
	req := h.request(http.MethodPost, "/lists/"+system.ID+"/edit", owner, form)
	req.SetPathValue("id", system.ID)
	rec := httptest.NewRecorder()
	h.srv.listEdit(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	got, err := h.db.Client.List.Get(context.Background(), system.ID)
	if err != nil {
		t.Fatalf("reload list: %v", err)
	}
	if got.Name != "Wishlist" || got.Visibility != list.VisibilityPrivate {
		t.Fatalf("system list was modified: %q/%q", got.Name, got.Visibility)
	}
}

func TestListDeleteOwnerAndSystemRules(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")
	other := h.user(t, "other")
	ctx := context.Background()
	drink, err := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}

	custom, _ := h.db.Client.List.Create().SetUserID(owner.ID).SetName("Custom").SetSlug("custom").Save(ctx)
	if _, err := h.db.Client.ListItem.Create().SetListID(custom.ID).SetDrinkID(drink.ID).Save(ctx); err != nil {
		t.Fatalf("item: %v", err)
	}
	system, _ := h.db.Client.List.Create().SetUserID(owner.ID).SetName("Wishlist").SetSlug("wishlist").
		SetIsSystem(true).Save(ctx)

	tests := []struct {
		name       string
		actor      *ent.User
		listID     string
		wantStatus int
		wantGone   bool
	}{
		{"owner deletes custom list", owner, custom.ID, http.StatusFound, true},
		{"system list refuses deletion", owner, system.ID, http.StatusBadRequest, false},
		{"non-owner is not found", other, system.ID, http.StatusNotFound, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := h.request(http.MethodPost, "/lists/"+tt.listID+"/delete", tt.actor, h.form())
			req.SetPathValue("id", tt.listID)
			rec := httptest.NewRecorder()
			h.srv.listDelete(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			exists, err := h.db.Client.List.Query().Where(list.ID(tt.listID)).Exist(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantGone && exists {
				t.Fatalf("list %s still exists", tt.listID)
			}
			if !tt.wantGone && !exists {
				t.Fatalf("list %s was deleted", tt.listID)
			}
			if tt.wantGone {
				n, err := h.db.Client.ListItem.Query().Where(listitem.ListID(tt.listID)).Count(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Fatalf("list items remaining = %d, want 0", n)
				}
			}
		})
	}
}

func TestVenuesSavedPageAndUnsave(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "saver")
	ctx := context.Background()
	v, err := h.db.Client.Venue.Create().
		SetName("Corner Fridge").SetSlug("corner-fridge").SetKind("shop").SetCity("Leeds").Save(ctx)
	if err != nil {
		t.Fatalf("venue: %v", err)
	}
	if err := h.srv.Venues.Save(ctx, u.ID, v.ID); err != nil {
		t.Fatalf("save: %v", err)
	}

	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	rec := httptest.NewRecorder()
	h.srv.venuesSavedPage(rec, h.request(http.MethodGet, "/venues/saved", u, url.Values{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := fmt.Sprintf("/venues/%d", v.ID)
	if body := rec.Body.String(); !strings.Contains(body, "Corner Fridge") || !strings.Contains(body, want) {
		t.Fatalf("body missing venue %s: %q", want, body)
	}

	req := h.request(http.MethodPost, "/venues/"+fmt.Sprint(v.ID)+"/unsave", u, h.form())
	req.SetPathValue("id", fmt.Sprint(v.ID))
	rec = httptest.NewRecorder()
	h.srv.venueUnsave(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("unsave status = %d, want 302", rec.Code)
	}
	n, err := h.db.Client.VenueSave.Query().Where(venuesave.UserID(u.ID), venuesave.VenueID(v.ID)).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("saved venues after unsave = %d, want 0", n)
	}
}

func TestNotificationFriendRequestIDOnlyForIncomingPending(t *testing.T) {
	h := newV12Harness(t)
	viewer := h.user(t, "viewer")
	incoming := h.user(t, "incoming")
	outgoing := h.user(t, "outgoing")
	accepted := h.user(t, "accepted")
	ctx := context.Background()

	// incoming -> viewer, pending.
	if _, err := h.db.Client.Friendship.Create().
		SetUserA(incoming.ID).SetUserB(viewer.ID).
		SetStatus(friendship.StatusPending).SetRequestedBy(incoming.ID).Save(ctx); err != nil {
		t.Fatalf("incoming friendship: %v", err)
	}
	// viewer -> outgoing, pending (the notification is outgoing).
	if _, err := h.db.Client.Friendship.Create().
		SetUserA(viewer.ID).SetUserB(outgoing.ID).
		SetStatus(friendship.StatusPending).SetRequestedBy(viewer.ID).Save(ctx); err != nil {
		t.Fatalf("outgoing friendship: %v", err)
	}
	// accepted friendship.
	if _, err := h.db.Client.Friendship.Create().
		SetUserA(accepted.ID).SetUserB(viewer.ID).
		SetStatus(friendship.StatusAccepted).SetRequestedBy(accepted.ID).Save(ctx); err != nil {
		t.Fatalf("accepted friendship: %v", err)
	}

	for _, actor := range []*ent.User{incoming, outgoing, accepted} {
		if _, err := h.db.Client.Notification.Create().
			SetUserID(viewer.ID).SetKind(notification.KindFriendRequest).
			SetActorID(actor.ID).SetSubjectID("1").SetText("wants to be friends").Save(ctx); err != nil {
			t.Fatalf("notification: %v", err)
		}
	}

	rec := h.renderWith(t, "notifications", `{{range .Notifications}}{{.ActorHandle}}={{.FriendRequestID}};{{end}}`)
	h.srv.notificationsPage(rec, h.request(http.MethodGet, "/notifications", viewer, url.Values{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "incoming=") || strings.Contains(body, "incoming=;") {
		t.Fatalf("incoming request missing friendship id: %q", body)
	}
	if !strings.Contains(body, "outgoing=;") {
		t.Fatalf("outgoing request should not expose an id: %q", body)
	}
	if !strings.Contains(body, "accepted=;") {
		t.Fatalf("accepted friendship should not expose an id: %q", body)
	}
}

func TestFollowUnfollowIdempotentAndSelfRejected(t *testing.T) {
	h := newV12Harness(t)
	viewer := h.user(t, "viewer")
	target := h.user(t, "target")
	ctx := context.Background()

	post := func(handle string) *httptest.ResponseRecorder {
		req := h.request(http.MethodPost, "/u/"+handle+"/follow", viewer, h.form())
		req.SetPathValue("handle", handle)
		rec := httptest.NewRecorder()
		h.srv.followUser(rec, req)
		return rec
	}
	remove := func(handle string) *httptest.ResponseRecorder {
		req := h.request(http.MethodPost, "/u/"+handle+"/unfollow", viewer, h.form())
		req.SetPathValue("handle", handle)
		rec := httptest.NewRecorder()
		h.srv.unfollowUser(rec, req)
		return rec
	}
	count := func() int {
		n, err := h.db.Client.Follow.Query().Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if rec := post(target.Handle); rec.Code != http.StatusFound {
		t.Fatalf("follow status = %d, want 302", rec.Code)
	}
	if rec := post(target.Handle); rec.Code != http.StatusFound {
		t.Fatalf("second follow status = %d, want 302", rec.Code)
	}
	if got := count(); got != 1 {
		t.Fatalf("follows = %d, want 1", got)
	}
	if rec := remove(target.Handle); rec.Code != http.StatusFound {
		t.Fatalf("unfollow status = %d, want 302", rec.Code)
	}
	if rec := remove(target.Handle); rec.Code != http.StatusFound {
		t.Fatalf("second unfollow status = %d, want 302", rec.Code)
	}
	if got := count(); got != 0 {
		t.Fatalf("follows after unfollow = %d, want 0", got)
	}
	if rec := post(viewer.Handle); rec.Code != http.StatusBadRequest {
		t.Fatalf("self-follow status = %d, want 400", rec.Code)
	}
}

func TestFollowBlockedPairIsSilentNotForbidden(t *testing.T) {
	h := newV12Harness(t)
	viewer := h.user(t, "viewer")
	target := h.user(t, "target")
	ctx := context.Background()

	// Target blocks the viewer; a follow must look exactly like following an
	// unknown handle: a redirect, never a 403 that reveals the block.
	if err := h.srv.Social.Block(ctx, target.ID, viewer.Handle); err != nil {
		t.Fatalf("block: %v", err)
	}
	req := h.request(http.MethodPost, "/u/"+target.Handle+"/follow", viewer, h.form())
	req.SetPathValue("handle", target.Handle)
	rec := httptest.NewRecorder()
	h.srv.followUser(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("blocked follow status = %d, want 302 (never 403): %s", rec.Code, rec.Body.String())
	}
	n, err := h.db.Client.Follow.Query().Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("follow rows after blocked follow = %d, want 0", n)
	}
}

func TestHomeFollowingTabUsesFollowingFeed(t *testing.T) {
	h := newV12Harness(t)
	viewer := h.user(t, "viewer")
	followed := h.user(t, "followed")
	friend := h.user(t, "friend")
	ctx := context.Background()
	if err := h.srv.Social.Follow(ctx, viewer.ID, followed.Handle); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if _, err := h.db.Client.Friendship.Create().
		SetUserA(friend.ID).SetUserB(viewer.ID).
		SetStatus(friendship.StatusAccepted).SetRequestedBy(friend.ID).Save(ctx); err != nil {
		t.Fatalf("friendship: %v", err)
	}
	drink, _ := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if _, err := h.db.Client.Checkin.Create().SetUserID(followed.ID).SetDrinkID(drink.ID).
		SetVisibility(checkin.VisibilityPublic).Save(ctx); err != nil {
		t.Fatalf("followed checkin: %v", err)
	}
	if _, err := h.db.Client.Checkin.Create().SetUserID(friend.ID).SetDrinkID(drink.ID).
		SetVisibility(checkin.VisibilityPublic).Save(ctx); err != nil {
		t.Fatalf("friend checkin: %v", err)
	}

	rec := h.renderWith(t, "home", `{{.FeedTab}}|{{range .Feed}}{{.Handle}},{{end}}`)
	h.srv.home(rec, h.request(http.MethodGet, "/?tab=following", viewer, url.Values{}))
	if body := rec.Body.String(); !strings.HasPrefix(body, "following|") || !strings.Contains(body, "followed,") || strings.Contains(body, "friend,") {
		t.Fatalf("following tab body = %q", body)
	}

	rec = h.renderWith(t, "home", `{{.FeedTab}}|{{range .Feed}}{{.Handle}},{{end}}`)
	h.srv.home(rec, h.request(http.MethodGet, "/", viewer, url.Values{}))
	if body := rec.Body.String(); !strings.HasPrefix(body, "friends|") || !strings.Contains(body, "followed,") || !strings.Contains(body, "friend,") {
		t.Fatalf("default tab body = %q", body)
	}
}

func TestProfileFollowerCountsAndIsFollowing(t *testing.T) {
	h := newV12Harness(t)
	viewer := h.user(t, "viewer")
	target := h.user(t, "target")
	follower := h.user(t, "follower")
	ctx := context.Background()
	if err := h.srv.Social.Follow(ctx, viewer.ID, target.Handle); err != nil {
		t.Fatalf("viewer follow: %v", err)
	}
	if err := h.srv.Social.Follow(ctx, follower.ID, target.Handle); err != nil {
		t.Fatalf("follower follow: %v", err)
	}
	a, b := viewer.ID, target.ID
	if a > b {
		a, b = b, a
	}
	if _, err := h.db.Client.Friendship.Create().
		SetUserA(a).SetUserB(b).SetStatus(friendship.StatusAccepted).SetRequestedBy(target.ID).Save(ctx); err != nil {
		t.Fatalf("friendship: %v", err)
	}
	rec := h.renderWith(t, "profile", `{{.IsFollowing}}|{{.FollowerCount}}|{{.FollowingCount}}`)
	req := h.request(http.MethodGet, "/u/target", viewer, url.Values{})
	req.SetPathValue("handle", "target")
	h.srv.profilePage(rec, req)
	if body := rec.Body.String(); body != "true|2|0" {
		t.Fatalf("profile body = %q, want true|2|0", body)
	}
}
