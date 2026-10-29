package web

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"sodapop/ent"
	"sodapop/internal/sharelinks"
)

func newShareHarness(t *testing.T) (*v12Harness, *http.ServeMux) {
	t.Helper()
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	h.srv.ShareLinks = sharelinks.New(h.db.SQL)
	mux := http.NewServeMux()
	h.srv.Register(mux)
	return h, mux
}

// verifiedShareUser creates a user whose email is confirmed, as required by
// the mutation routes.
func verifiedShareUser(t *testing.T, h *v12Harness, handle string) *ent.User {
	t.Helper()
	u := h.user(t, handle)
	updated, err := h.db.Client.User.UpdateOneID(u.ID).
		SetEmailVerifiedAt(time.Now().UTC().Format(time.RFC3339)).
		Save(context.Background())
	if err != nil {
		t.Fatalf("verify %s: %v", handle, err)
	}
	return updated
}

func serveShare(mux *http.ServeMux, u *ent.User, req *http.Request) *httptest.ResponseRecorder {
	if u != nil {
		ctx := context.WithValue(req.Context(), userKey{}, u)
		ctx = context.WithValue(ctx, sessionKey{}, &ent.Session{CsrfToken: "test-csrf"})
		req = req.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func shareFormRequest(method, target string, form url.Values) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestPublicSharePageHasSocialMeta(t *testing.T) {
	h, mux := newShareHarness(t)
	u := h.user(t, "sharer")
	link, err := h.srv.ShareLinks.Create(context.Background(), u.ID, "passport")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	rec := serveShare(mux, nil, httptest.NewRequest(http.MethodGet, "/share/"+link.Token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("share page = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	wants := []string{
		`<meta property="og:image" content="http://localhost/share/` + link.Token + `/card.png">`,
		`<meta property="og:image:width" content="1200">`,
		`<meta property="og:image:height" content="630">`,
		`<meta property="og:image:alt" content="Sodapop soda passport card for Sharer">`,
		`<meta property="og:url" content="http://localhost/share/` + link.Token + `">`,
		`<meta name="twitter:image" content="http://localhost/share/` + link.Token + `/card.png">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`noindex,nofollow`,
		`@sharer`,
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("share page missing %q", want)
		}
	}
}

func TestShareCardIsPNG(t *testing.T) {
	h, mux := newShareHarness(t)
	u := h.user(t, "carder")
	link, err := h.srv.ShareLinks.Create(context.Background(), u.ID, "streak")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	rec := serveShare(mux, nil, httptest.NewRequest(http.MethodGet, "/share/"+link.Token+"/card.png", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("card = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q", got)
	}
	if rec.Header().Get("Content-Length") == "" {
		t.Error("Content-Length is missing")
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 630 {
		t.Errorf("card = %dx%d, want 1200x630", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestRevokedShareLinkIsNotFound(t *testing.T) {
	h, mux := newShareHarness(t)
	u := h.user(t, "revoker")
	link, err := h.srv.ShareLinks.Create(context.Background(), u.ID, "passport")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	if ok, err := h.srv.ShareLinks.Revoke(context.Background(), u.ID, link.ID); err != nil || !ok {
		t.Fatalf("revoke = %v, %v", ok, err)
	}

	for _, path := range []string{"/share/" + link.Token, "/share/" + link.Token + "/card.png"} {
		rec := serveShare(mux, nil, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
	}
	rec := serveShare(mux, nil, httptest.NewRequest(http.MethodGet, "/share/unknown-token", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown token = %d, want 404", rec.Code)
	}
}

func TestShareCreateRequiresAuthAndCSRF(t *testing.T) {
	h, mux := newShareHarness(t)
	form := url.Values{"csrf_token": {"test-csrf"}, "kind": {"passport"}}

	anon := serveShare(mux, nil, shareFormRequest(http.MethodPost, "/share", form))
	if anon.Code != http.StatusFound {
		t.Fatalf("anonymous create = %d, want 302", anon.Code)
	}
	if loc := anon.Header().Get("Location"); !strings.HasPrefix(loc, "/login") {
		t.Errorf("anonymous redirect = %q, want /login", loc)
	}

	u := verifiedShareUser(t, h, "csrf-victim")
	badForm := url.Values{"csrf_token": {"wrong"}, "kind": {"passport"}}
	bad := serveShare(mux, u, shareFormRequest(http.MethodPost, "/share", badForm))
	if bad.Code != http.StatusForbidden {
		t.Fatalf("bad CSRF = %d, want 403", bad.Code)
	}
	if links, err := h.srv.ShareLinks.ListForUser(context.Background(), u.ID, 10); err != nil || len(links) != 0 {
		t.Fatalf("link created despite CSRF failure: %d links, err %v", len(links), err)
	}
}

func TestShareCreateAndRevokeRoundTrip(t *testing.T) {
	h, mux := newShareHarness(t)
	u := verifiedShareUser(t, h, "roundtrip")
	form := url.Values{"csrf_token": {"test-csrf"}, "kind": {"passport"}}

	created := serveShare(mux, u, shareFormRequest(http.MethodPost, "/share", form))
	if created.Code != http.StatusFound {
		t.Fatalf("create = %d, want 302: %s", created.Code, created.Body.String())
	}
	loc, err := url.Parse(created.Header().Get("Location"))
	if err != nil {
		t.Fatalf("location: %v", err)
	}
	sharePath := loc.Query().Get("share")
	if !strings.HasPrefix(sharePath, "/share/") {
		t.Fatalf("share path = %q", sharePath)
	}

	page := serveShare(mux, u, httptest.NewRequest(http.MethodGet, sharePath, nil))
	if page.Code != http.StatusOK {
		t.Fatalf("created share page = %d, want 200", page.Code)
	}

	links, err := h.srv.ShareLinks.ListForUser(context.Background(), u.ID, 10)
	if err != nil || len(links) != 1 {
		t.Fatalf("links = %d, err %v", len(links), err)
	}
	revoke := url.Values{"csrf_token": {"test-csrf"}}
	revoked := serveShare(mux, u, shareFormRequest(http.MethodPost, "/share/"+links[0].ID+"/revoke", revoke))
	if revoked.Code != http.StatusFound {
		t.Fatalf("revoke = %d, want 302", revoked.Code)
	}

	gone := serveShare(mux, nil, httptest.NewRequest(http.MethodGet, sharePath, nil))
	if gone.Code != http.StatusNotFound {
		t.Fatalf("revoked share page = %d, want 404", gone.Code)
	}
	if links, err := h.srv.ShareLinks.ListForUser(context.Background(), u.ID, 10); err != nil || len(links) != 0 {
		t.Fatalf("links after revoke = %d, err %v", len(links), err)
	}
}

func TestShareCreateRejectsInvalidKind(t *testing.T) {
	h, mux := newShareHarness(t)
	u := verifiedShareUser(t, h, "picky")
	form := url.Values{"csrf_token": {"test-csrf"}, "kind": {"not-a-card"}}

	rec := serveShare(mux, u, shareFormRequest(http.MethodPost, "/share", form))
	if rec.Code != http.StatusFound {
		t.Fatalf("invalid kind = %d, want 302", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("location: %v", err)
	}
	if loc.Query().Get("share_error") != "invalid" {
		t.Errorf("share_error = %q, want invalid", loc.Query().Get("share_error"))
	}
	if links, err := h.srv.ShareLinks.ListForUser(context.Background(), u.ID, 10); err != nil || len(links) != 0 {
		t.Fatalf("invalid kind created %d links, err %v", len(links), err)
	}
}

func TestSharePathsStayNoindex(t *testing.T) {
	for _, path := range []string{"/share/abc", "/share/abc/card.png"} {
		if indexablePath(path) {
			t.Errorf("%s is crawlable; share links must stay noindex", path)
		}
	}
}

func TestShareLinksAreScopedToTheirOwner(t *testing.T) {
	h, mux := newShareHarness(t)
	owner := h.user(t, "owner-links")
	other := h.user(t, "other-links")
	link, err := h.srv.ShareLinks.Create(context.Background(), owner.ID, "streak")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	otherPage := serveShare(mux, other, httptest.NewRequest(http.MethodGet, "/you", nil))
	if otherPage.Code != http.StatusOK {
		t.Fatalf("other /you = %d, want 200", otherPage.Code)
	}
	if strings.Contains(otherPage.Body.String(), link.Token) {
		t.Fatal("another user's share token appeared on /you")
	}
	if strings.Contains(otherPage.Body.String(), "http://localhost/share/") {
		t.Fatal("another user's share URL appeared on /you")
	}

	ownerPage := serveShare(mux, owner, httptest.NewRequest(http.MethodGet, "/you", nil))
	if ownerPage.Code != http.StatusOK {
		t.Fatalf("owner /you = %d, want 200", ownerPage.Code)
	}
	if !strings.Contains(ownerPage.Body.String(), link.Token) {
		t.Fatal("owner's own share link is missing from /you")
	}
}

func TestShareViewsAreRateLimitedPerIP(t *testing.T) {
	h, mux := newShareHarness(t)
	u := h.user(t, "limited")
	link, err := h.srv.ShareLinks.Create(context.Background(), u.ID, "passport")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	ip := "192.0.2.1" // httptest.NewRequest's default RemoteAddr peer.
	for i := 0; i < 60; i++ {
		if !h.srv.Limiter.Allow("shareview:"+ip, 60, time.Minute) {
			t.Fatalf("bucket refused request %d while filling", i+1)
		}
	}

	for _, path := range []string{"/share/" + link.Token, "/share/" + link.Token + "/card.png"} {
		rec := serveShare(mux, nil, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("%s = %d, want 429", path, rec.Code)
		}
	}

	// A different client still resolves the same link.
	fresh := httptest.NewRequest(http.MethodGet, "/share/"+link.Token, nil)
	fresh.RemoteAddr = "198.51.100.9:1234"
	if rec := serveShare(mux, nil, fresh); rec.Code != http.StatusOK {
		t.Errorf("fresh IP = %d, want 200", rec.Code)
	}
}

func TestShareMutationsRequireVerifiedEmail(t *testing.T) {
	h, mux := newShareHarness(t)
	u := h.user(t, "unverified-sharer")

	form := url.Values{"csrf_token": {"test-csrf"}, "kind": {"passport"}}
	created := serveShare(mux, u, shareFormRequest(http.MethodPost, "/share", form))
	if created.Code != http.StatusForbidden {
		t.Fatalf("unverified create = %d, want 403", created.Code)
	}
	if links, err := h.srv.ShareLinks.ListForUser(context.Background(), u.ID, 10); err != nil || len(links) != 0 {
		t.Fatalf("unverified create made %d links, err %v", len(links), err)
	}

	link, err := h.srv.ShareLinks.Create(context.Background(), u.ID, "passport")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	revoke := url.Values{"csrf_token": {"test-csrf"}}
	revoked := serveShare(mux, u, shareFormRequest(http.MethodPost, "/share/"+link.ID+"/revoke", revoke))
	if revoked.Code != http.StatusForbidden {
		t.Fatalf("unverified revoke = %d, want 403", revoked.Code)
	}
	if _, err := h.srv.ShareLinks.Resolve(context.Background(), link.Token); err != nil {
		t.Fatalf("unverified revoke changed the link: %v", err)
	}
}

func TestShareNoticeOnlyShowsOwnActiveLink(t *testing.T) {
	h, mux := newShareHarness(t)
	owner := h.user(t, "notice-owner")
	other := h.user(t, "notice-other")
	own, err := h.srv.ShareLinks.Create(context.Background(), owner.ID, "passport")
	if err != nil {
		t.Fatalf("create own link: %v", err)
	}
	foreign, err := h.srv.ShareLinks.Create(context.Background(), other.ID, "passport")
	if err != nil {
		t.Fatalf("create foreign link: %v", err)
	}

	ownPage := serveShare(mux, owner, httptest.NewRequest(http.MethodGet, "/you?share=/share/"+own.Token, nil))
	if ownPage.Code != http.StatusOK {
		t.Fatalf("own notice page = %d, want 200", ownPage.Code)
	}
	if !strings.Contains(ownPage.Body.String(), "Your link is ready.") {
		t.Error("owner's own token did not render the notice")
	}

	for _, query := range []string{foreign.Token, "unknown-token"} {
		page := serveShare(mux, owner, httptest.NewRequest(http.MethodGet, "/you?share=/share/"+query, nil))
		if page.Code != http.StatusOK {
			t.Fatalf("foreign notice page = %d, want 200", page.Code)
		}
		body := page.Body.String()
		if strings.Contains(body, "Your link is ready.") {
			t.Errorf("notice rendered for %q", query)
		}
		if strings.Contains(body, query) {
			t.Errorf("foreign token %q leaked into /you", query)
		}
	}
}
