package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/media"
)

func profileDataTemplate() string {
	return `F={{.Filters.From}}|T={{.Filters.To}}|RMIN={{.Filters.RatingMin}}|RMAX={{.Filters.RatingMax}}` +
		`|ML={{.Filters.MinLikes}}|PHOTO={{.Filters.HasPhoto}}|VIS={{.Filters.Visibility}}` +
		`|SORT={{.Filters.Sort}}|ACTIVE={{.Filters.Active}}|COUNT={{.Filters.ActiveCount}}` +
		`|MODE={{.Pagination.Mode}}|PAGE={{.Pagination.Page}}` +
		`|HN={{.Pagination.HasNext}}|HP={{.Pagination.HasPrev}}` +
		`|NU={{.Pagination.NextURL}}|PU={{.Pagination.PrevURL}}|OU={{.Pagination.OlderURL}}` +
		`|FQ={{.FilterQuery}}|AV={{.Profile.AvatarID}}` +
		`|FAV={{with .Profile.FavouriteDrink}}{{.ID}}/{{.Name}}/{{.Slug}}/{{.Brand}}/{{.Style}}/{{.ImageURL}}{{end}}`
}

func getProfile(t *testing.T, h *v12Harness, viewer *ent.User, target, query string) string {
	t.Helper()
	rec := h.renderWith(t, "profile", profileDataTemplate())
	req := h.request(http.MethodGet, "/u/"+target+"?"+query, viewer, url.Values{})
	req.SetPathValue("handle", target)
	h.srv.profilePage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile status = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestProfileFiltersClampDatesRatingsLikesAndPage(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")

	body := getProfile(t, h, owner, "owner",
		"from=2024-01-05&to=2024-01-07&rating_min=-3&rating_max=99&min_likes=2&photo=1&visibility=public&sort=likes&page=2")

	for _, want := range []string{
		"F=2024-01-05", "T=2024-01-07", "RMIN=0", "RMAX=20", "ML=2",
		"PHOTO=true", "VIS=public", "SORT=likes", "ACTIVE=true", "COUNT=8",
		"MODE=offset", "PAGE=2", "HN=false", "HP=true",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("profile body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "page=2") {
		t.Fatalf("FilterQuery must omit page: %s", body)
	}
	if !strings.Contains(body, "from=2024-01-05") ||
		!strings.Contains(body, "rating_min=0") ||
		!strings.Contains(body, "rating_max=20") ||
		!strings.Contains(body, "min_likes=2") ||
		!strings.Contains(body, "visibility=public") ||
		!strings.Contains(body, "sort=likes") {
		t.Fatalf("FilterQuery lost an active filter: %s", body)
	}
	// Page 2 has no rows in this fixture, so the previous-page link exists and
	// the next-page link does not.
	if !strings.Contains(body, "PU=/u/owner?") || !strings.Contains(body, "NU=|") {
		t.Fatalf("pagination links wrong: %s", body)
	}
}

func TestProfileFiltersIgnoreInvalidAndInvertedValues(t *testing.T) {
	h := newV12Harness(t)
	owner := h.user(t, "owner")

	body := getProfile(t, h, owner, "owner",
		"from=not-a-date&to=2024-13-99&rating_min=9&rating_max=4&min_likes=-2&photo=yes&visibility=secret&sort=bogus&page=-1")

	for _, want := range []string{
		"F=|T=|RMIN=|RMAX=|ML=|PHOTO=false|VIS=|SORT=|ACTIVE=false|COUNT=0",
		"MODE=cursor", "PAGE=1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("invalid filters should be ignored, missing %q: %s", want, body)
		}
	}
}

func TestProfileVisibilityFilterIsOwnerOnly(t *testing.T) {
	h := newV12Harness(t)
	viewer := h.user(t, "viewer")
	target := h.user(t, "target")
	ctx := context.Background()
	prefs, err := h.srv.Auth.GetOrCreateSettings(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := prefs.Update().SetPublicProfile(true).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	body := getProfile(t, h, viewer, "target", "visibility=private&sort=oldest")
	if !strings.Contains(body, "VIS=|") {
		t.Fatalf("non-owner visibility filter should be dropped: %s", body)
	}
	if strings.Contains(body, "visibility=") {
		t.Fatalf("non-owner visibility must not reach the filter query: %s", body)
	}
}

func TestProfileAvatarAndPinnedFavouriteDrink(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	owner := h.user(t, "owner")

	brand, err := h.db.Client.Brand.Create().SetName("Acme Soda").SetSlug("acme-soda").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	style, err := h.db.Client.Style.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.db.Client.Drink.Create().
		SetName("Cherry Cola").SetSlug("cherry-cola").
		SetBrandID(brand.ID).SetStyleID(style.ID).SetImageURL("/img/cherry.png").
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row, err := h.db.Client.Media.Create().
		SetUserID(owner.ID).SetKind(media.KindAvatar).
		SetPath("2026/01/avatar.png").SetMime("image/png").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Auth.SetAvatar(ctx, owner.ID, row.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Auth.SetFavouriteDrink(ctx, owner.ID, d.ID); err != nil {
		t.Fatal(err)
	}

	body := getProfile(t, h, owner, "owner", "")
	if !strings.Contains(body, "AV="+row.ID) {
		t.Fatalf("profile avatar missing: %s", body)
	}
	want := "FAV=" + itoa(d.ID) + "/Cherry Cola/cherry-cola/Acme Soda/Cola//img/cherry.png"
	if !strings.Contains(body, want) {
		t.Fatalf("pinned drink missing, want %q in %s", want, body)
	}
}

func TestSettingsDataExposesAvatarID(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	u := h.user(t, "avataruser")
	row, err := h.db.Client.Media.Create().
		SetUserID(u.ID).SetKind(media.KindAvatar).
		SetPath("2026/01/avatar.png").SetMime("image/png").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Auth.SetAvatar(ctx, u.ID, row.ID); err != nil {
		t.Fatal(err)
	}
	u, err = h.db.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := h.request(http.MethodGet, "/settings", u, url.Values{})
	data, err := h.srv.settingsData(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	profile, ok := data["Profile"].(map[string]string)
	if !ok {
		t.Fatalf("Profile data type = %T", data["Profile"])
	}
	if profile["AvatarID"] != row.ID {
		t.Fatalf("AvatarID = %q, want %q", profile["AvatarID"], row.ID)
	}
}

func TestDrinkFavouriteRequiresCSRFAndAuth(t *testing.T) {
	h := newV12Harness(t)
	h.renderWith(t, "drink", "unused")
	u := h.user(t, "pinner")
	d, err := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Auth: the route wrapper sends anonymous visitors to login.
	anon := httptest.NewRequest(http.MethodPost, "/drinks/cola/favourite", strings.NewReader(""))
	anonRec := httptest.NewRecorder()
	h.srv.requireUser(h.srv.drinkFavourite)(anonRec, anon)
	if anonRec.Code != http.StatusFound || anonRec.Header().Get("Location") != "/login?next=/drinks/cola/favourite" {
		t.Fatalf("anonymous favourite = %d %q", anonRec.Code, anonRec.Header().Get("Location"))
	}

	// CSRF: a signed-in request without the token is refused.
	req := h.request(http.MethodPost, "/drinks/cola/favourite", u, url.Values{})
	req.SetPathValue("slug", "cola")
	rec := httptest.NewRecorder()
	h.srv.drinkFavourite(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d, want 403", rec.Code)
	}
	row, err := h.db.Client.User.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.FavouriteDrinkID != nil {
		t.Fatal("drink was pinned without CSRF")
	}

	// Success pins and redirects back to the drink.
	req = h.request(http.MethodPost, "/drinks/cola/favourite", u, h.form())
	req.SetPathValue("slug", "cola")
	rec = httptest.NewRecorder()
	h.srv.drinkFavourite(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/drinks/cola" {
		t.Fatalf("favourite = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	row, err = h.db.Client.User.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.FavouriteDrinkID == nil || *row.FavouriteDrinkID != d.ID {
		t.Fatalf("FavouriteDrinkID = %v, want %d", row.FavouriteDrinkID, d.ID)
	}
}

func TestDrinkUnfavouriteClearsPinAndUnknownDrinkIs404(t *testing.T) {
	h := newV12Harness(t)
	h.renderWith(t, "drink", "unused")
	ctx := context.Background()
	u := h.user(t, "pinner")
	d, err := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Auth.SetFavouriteDrink(ctx, u.ID, d.ID); err != nil {
		t.Fatal(err)
	}

	req := h.request(http.MethodPost, "/drinks/cola/unfavourite", u, h.form())
	req.SetPathValue("slug", "cola")
	rec := httptest.NewRecorder()
	h.srv.drinkUnfavourite(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/drinks/cola" {
		t.Fatalf("unfavourite = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	row, err := h.db.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.FavouriteDrinkID != nil {
		t.Fatalf("FavouriteDrinkID = %v, want nil", row.FavouriteDrinkID)
	}

	req = h.request(http.MethodPost, "/drinks/missing/favourite", u, h.form())
	req.SetPathValue("slug", "missing")
	rec = httptest.NewRecorder()
	h.srv.drinkFavourite(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown drink = %d, want 404", rec.Code)
	}
}

func TestDrinkPagePassesFavouriteState(t *testing.T) {
	h := newV12Harness(t)
	ctx := context.Background()
	u := h.user(t, "pinner")
	d, err := h.db.Client.Drink.Create().SetName("Cola").SetSlug("cola").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Auth.SetFavouriteDrink(ctx, u.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	u, err = h.db.Client.User.Get(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	rec := h.renderWith(t, "drink", `{{.IsFavourite}}|{{.FavouriteDrinkID}}`)
	req := h.request(http.MethodGet, "/drinks/cola", u, url.Values{})
	req.SetPathValue("slug", "cola")
	h.srv.drinkPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("drink page = %d: %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Body.String(), "true|"+itoa(d.ID); got != want {
		t.Fatalf("drink page = %q, want %q", got, want)
	}
}

// itoa keeps the test free of extra imports while making the pinned-drink
// assertion readable.
func itoa(n int) string {
	return strconv.Itoa(n)
}
