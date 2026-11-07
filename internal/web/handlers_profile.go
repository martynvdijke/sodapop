package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/ent/follow"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
	"sodapop/internal/social"
)

// profileFavouriteDrink is the pinned-drink payload the profile header can
// render. ImageURL is nullable because catalog rows may have no artwork.
type profileFavouriteDrink struct {
	ID       int
	Name     string
	Slug     string
	Brand    string
	Style    string
	ImageURL *string
}

// profileFiltersView repopulates the profile filter form. Empty strings mean
// the filter is not set.
type profileFiltersView struct {
	From        string
	To          string
	RatingMin   string
	RatingMax   string
	MinLikes    string
	HasPhoto    bool
	Visibility  string
	Sort        string
	Active      bool
	ActiveCount int
}

// profilePaginationView carries the paging contract for the profile feed.
// Cursor mode fills OlderURL; offset mode fills NextURL/PrevURL.
type profilePaginationView struct {
	Mode     string
	Page     int
	HasNext  bool
	HasPrev  bool
	NextURL  string
	PrevURL  string
	OlderURL string
}

// profileFeedItem lets profile rows share the feed_item partial with feed
// rows. ProfileItem does not load friend tags or coordinates, so those
// optional fields stay nil and the partial simply omits them.
type profileFeedItem struct {
	checkins.ProfileItem
	TaggedFriends []social.FeedTaggedFriend
	Lat           *float64
	Lng           *float64
	Comments      int
	Saved         bool
}

func profileFeedItems(items []checkins.ProfileItem) []profileFeedItem {
	out := make([]profileFeedItem, len(items))
	for i, item := range items {
		out[i] = profileFeedItem{ProfileItem: item}
	}
	return out
}

// profilePage renders the opt-in public profile at GET /u/{handle}. Access is
// decided by checkins.DecideProfileAccess so a private profile is either a
// login redirect (anonymous) or a 404 (signed-in stranger), never a 403.
func (s *Server) profilePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	handle := strings.ToLower(strings.TrimSpace(r.PathValue("handle")))
	if handle == "" {
		s.profileNotFound(w, r)
		return
	}

	target, err := s.Auth.Client.User.Query().
		Where(user.HandleEqualFold(handle), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			s.profileNotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}

	// A soft-deleted or deleted account is a missing profile, not a tombstone.
	viewer := UserFromContext(r)
	viewerID := ""
	if viewer != nil {
		viewerID = viewer.ID
	}
	areFriends := viewerID != "" && viewerID != target.ID && s.Social.AreFriends(ctx, viewerID, target.ID)

	// A block in either direction is indistinguishable from a missing profile,
	// including for opted-in public profiles. This check runs before any
	// profile content is loaded.
	isBlocked := false
	if viewerID != "" && viewerID != target.ID {
		isBlocked, err = s.Social.AreBlocked(ctx, viewerID, target.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if isBlocked {
			s.profileNotFound(w, r)
			return
		}
	}

	settings, err := s.Auth.GetOrCreateSettings(ctx, target.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	public := settings.PublicProfile

	switch checkins.DecideProfileAccess(public, target.Status == user.StatusDeleted, viewerID, target.ID, areFriends) {
	case checkins.ProfileHidden:
		s.profileNotFound(w, r)
		return
	case checkins.ProfileNeedsLogin:
		http.Redirect(w, r, "/login?next="+url.QueryEscape("/u/"+target.Handle), http.StatusFound)
		return
	}

	isOwner := viewerID != "" && viewerID == target.ID
	before := strings.TrimSpace(r.URL.Query().Get("before"))
	const pageSize = 20
	filter, filtersView, filterParams, page := parseProfileFilters(r.URL.Query(), isOwner)
	filter.ViewerID = viewerID
	filter.TargetID = target.ID
	filter.Limit = pageSize
	filter.PageSize = pageSize
	filter.Page = page
	filter.Before = before
	result, err := s.Checkins.ProfileCheckinsPage(ctx, filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items := result.Items
	csrf := ""
	if sess := SessionFromContext(r); sess != nil {
		csrf = sess.CsrfToken
	}
	for i := range items {
		items[i].CSRF = csrf
	}
	stats, err := s.Checkins.ProfileStats(ctx, viewerID, target.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	lists, err := s.Checkins.ProfileLists(ctx, viewerID, target.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	// Earned badges only: the full ListForUser result includes unearned
	// definitions, which are teaching material for the owner, not profile data.
	statuses, err := s.Badges.ListForUser(ctx, target.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	badges := make([]profileBadgeView, 0, len(statuses))
	for _, st := range statuses {
		if !st.Earned {
			continue
		}
		badges = append(badges, profileBadgeView{
			Name:        st.Name,
			Description: st.Description,
			AwardedAt:   st.AwardedAt,
		})
	}
	topStyles, err := s.Checkins.ProfileTopStyles(ctx, viewerID, target.ID, 3)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	displayName := strings.TrimSpace(target.DisplayName)
	if displayName == "" {
		displayName = target.Handle
	}
	isFollowing := false
	if viewerID != "" && viewerID != target.ID {
		isFollowing, _ = s.Auth.Client.Follow.Query().
			Where(follow.FollowerID(viewerID), follow.FolloweeID(target.ID)).Exist(ctx)
	}
	followerCount, _ := s.Auth.Client.Follow.Query().
		Where(follow.FolloweeID(target.ID)).Count(ctx)
	followingCount, _ := s.Auth.Client.Follow.Query().
		Where(follow.FollowerID(target.ID)).Count(ctx)
	indexable := public
	robots := "noindex,nofollow"
	if indexable {
		robots = "index,follow"
	}

	avatarID := ""
	if target.AvatarMediaID != nil {
		avatarID = *target.AvatarMediaID
	}
	profile := map[string]any{
		"Handle":        target.Handle,
		"DisplayName":   displayName,
		"Bio":           target.Bio,
		"Initial":       initial(displayName),
		"JoinedAt":      target.CreatedAt,
		"PublicProfile": public,
		"AvatarID":      avatarID,
	}
	// A pinned drink is shown only if the catalog row still exists. It follows
	// the profile's own visibility: reachable profiles never leak a private row
	// because favourite drinks are always catalog/public data.
	if target.FavouriteDrinkID != nil {
		if d, err := s.Auth.Client.Drink.Query().
			Where(drink.ID(*target.FavouriteDrinkID)).
			WithBrand().
			WithStyle().
			Only(ctx); err == nil {
			fav := profileFavouriteDrink{
				ID:       d.ID,
				Name:     d.Name,
				Slug:     d.Slug,
				ImageURL: d.ImageURL,
			}
			if d.Edges.Brand != nil {
				fav.Brand = d.Edges.Brand.Name
			}
			if d.Edges.Style != nil {
				fav.Style = d.Edges.Style.Name
			}
			profile["FavouriteDrink"] = fav
		}
	}
	statMap := map[string]any{
		"Pours":  stats.Pours,
		"Styles": stats.Styles,
		"Venues": stats.Venues,
		"Badges": stats.Badges,
	}

	// The legacy cursor link is kept only for the default unfiltered newest
	// view. Every filtered, sorted, or paged view uses offset links instead.
	nextBefore := ""
	hasMore := false
	if result.Mode == checkins.ProfileModeCursor && result.HasNext && len(items) > 0 {
		last := items[len(items)-1]
		nextBefore = checkins.Cursor(last.CreatedAt, last.ID)
		hasMore = true
	}
	pagination := profilePaginationFor(target.Handle, result, filterParams, nextBefore)

	if !indexable {
		// The middleware leaves /u/* alone so opted-in profiles can be indexed;
		// private profiles must explicitly opt back out.
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	}

	s.render(w, r, http.StatusOK, "profile", map[string]any{
		"Title":           displayName,
		"Profile":         profile,
		"Stats":           statMap,
		"Checkins":        profileFeedItems(items),
		"Lists":           lists,
		"Badges":          badges,
		"TopStyles":       topStyles,
		"IsOwner":         isOwner,
		"IsFriend":        areFriends,
		"IsFollowing":     isFollowing,
		"IsBlocked":       isBlocked,
		"BlockedByTarget": false,
		"CanBlock":        viewerID != "" && viewerID != target.ID,
		"CanReport":       viewer != nil && viewer.EmailVerifiedAt != nil && viewerID != target.ID,
		"FollowerCount":   followerCount,
		"FollowingCount":  followingCount,
		"Authenticated":   viewerID != "",
		"Indexable":       indexable,
		"CanonicalURL":    s.baseURL() + "/u/" + target.Handle,
		"MetaRobots":      robots,
		"MetaTitle":       displayName + " (@" + target.Handle + ") · Sodapop",
		"MetaDescription": profileDescription(target.Bio, target.Handle),
		"NextBefore":      nextBefore,
		"HasMore":         hasMore,
		"Filters":         filtersView,
		"Pagination":      pagination,
		"FilterQuery":     filterParams.Encode(),
	})
}

// parseProfileFilters validates the profile feed query. Invalid values are
// ignored rather than rejected so a stale link still renders the profile.
// Visibility is owner-only: another viewer's value is dropped before it reaches
// the service, so the filter can never widen the relationship-based set.
func parseProfileFilters(q url.Values, isOwner bool) (checkins.ProfileCheckinsFilter, profileFiltersView, url.Values, int) {
	var (
		filter checkins.ProfileCheckinsFilter
		view   profileFiltersView
		params url.Values
		page   = 1
	)
	params = url.Values{}

	if raw := strings.TrimSpace(q.Get("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}

	if t, ok := parseProfileDate(q.Get("from")); ok {
		view.From = t.Format("2006-01-02")
		filter.From = t.UTC().Format(time.RFC3339)
		params.Set("from", view.From)
	}
	// "to" is an inclusive calendar day in the UI, so the service boundary is
	// the following midnight (the service treats To as exclusive).
	if t, ok := parseProfileDate(q.Get("to")); ok {
		view.To = t.Format("2006-01-02")
		filter.To = t.UTC().AddDate(0, 0, 1).Format(time.RFC3339)
		params.Set("to", view.To)
	}

	ratingMin, minOK := parseProfileRating(q.Get("rating_min"))
	ratingMax, maxOK := parseProfileRating(q.Get("rating_max"))
	if minOK && maxOK && ratingMin > ratingMax {
		// Contradictory range: ignore both rather than silently swapping them.
		minOK, maxOK = false, false
	}
	if minOK {
		v := ratingMin
		filter.RatingMin = &v
		view.RatingMin = strconv.Itoa(v)
		params.Set("rating_min", view.RatingMin)
	}
	if maxOK {
		v := ratingMax
		filter.RatingMax = &v
		view.RatingMax = strconv.Itoa(v)
		params.Set("rating_max", view.RatingMax)
	}

	if n, ok := parseProfileMinLikes(q.Get("min_likes")); ok {
		filter.MinLikes = &n
		view.MinLikes = strconv.Itoa(n)
		params.Set("min_likes", view.MinLikes)
	}

	if q.Get("photo") == "1" {
		filter.HasPhoto = true
		view.HasPhoto = true
		params.Set("photo", "1")
	}

	if isOwner {
		switch strings.ToLower(strings.TrimSpace(q.Get("visibility"))) {
		case "public", "friends", "private":
			view.Visibility = strings.ToLower(strings.TrimSpace(q.Get("visibility")))
			filter.Visibility = view.Visibility
			params.Set("visibility", view.Visibility)
		}
	}

	switch strings.ToLower(strings.TrimSpace(q.Get("sort"))) {
	case "newest", "oldest", "rating_high", "rating_low", "likes":
		view.Sort = strings.ToLower(strings.TrimSpace(q.Get("sort")))
		filter.Sort = view.Sort
		params.Set("sort", view.Sort)
	}

	for _, set := range []bool{
		view.From != "", view.To != "", view.RatingMin != "", view.RatingMax != "",
		view.MinLikes != "", view.HasPhoto, view.Visibility != "", view.Sort != "",
	} {
		if set {
			view.ActiveCount++
		}
	}
	view.Active = view.ActiveCount > 0
	return filter, view, params, page
}

func parseProfileDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// parseProfileRating accepts 0..20 and clamps out-of-range integers into that
// band. Non-numeric values are ignored.
func parseProfileRating(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if n < 0 {
		n = 0
	}
	if n > 20 {
		n = 20
	}
	return n, true
}

func parseProfileMinLikes(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// profilePaginationFor builds offset links that preserve every active filter.
// The cursor path keeps the existing single "older" link and escapes its value.
func profilePaginationFor(handle string, result *checkins.ProfileCheckinsResult, params url.Values, nextBefore string) profilePaginationView {
	out := profilePaginationView{
		Mode:    result.Mode,
		Page:    result.Page,
		HasNext: result.HasNext,
	}
	if result.Mode == checkins.ProfileModeCursor {
		if nextBefore != "" {
			out.OlderURL = "/u/" + handle + "?" + (url.Values{"before": {nextBefore}}).Encode()
		}
		return out
	}
	out.HasPrev = result.Page > 1
	if result.HasNext {
		out.NextURL = profilePageURL(handle, params, result.Page+1)
	}
	if out.HasPrev {
		out.PrevURL = profilePageURL(handle, params, result.Page-1)
	}
	return out
}

func profilePageURL(handle string, params url.Values, page int) string {
	v := url.Values{}
	for k, vals := range params {
		v[k] = append([]string(nil), vals...)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	} else {
		v.Del("page")
	}
	if len(v) == 0 {
		return "/u/" + handle
	}
	return "/u/" + handle + "?" + v.Encode()
}

// profileBadgeView is the safe subset of a badge shown on a public profile.
// Unearned badge definitions never reach the template.
type profileBadgeView struct {
	Name        string
	Description string
	AwardedAt   string
}

func (s *Server) profileNotFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusNotFound, "error", map[string]any{
		"Title":   "Not found",
		"Message": "That profile is not in Sodapop.",
	})
}

func (s *Server) followUser(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	handle := strings.ToLower(strings.TrimSpace(r.PathValue("handle")))
	if err := s.Social.Follow(r.Context(), u.ID, handle); err != nil {
		if err == social.ErrSelfAction {
			http.Error(w, "you cannot follow yourself", http.StatusBadRequest)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/u/"+url.PathEscape(handle), http.StatusFound)
}

func (s *Server) unfollowUser(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	handle := strings.ToLower(strings.TrimSpace(r.PathValue("handle")))
	if err := s.Social.Unfollow(r.Context(), u.ID, handle); err != nil {
		if err == social.ErrSelfAction {
			http.Error(w, "you cannot unfollow yourself", http.StatusBadRequest)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/u/"+url.PathEscape(handle), http.StatusFound)
}

// profileDescription trims the bio to 155 characters and falls back to a
// generic line when the bio is empty.
func profileDescription(bio, handle string) string {
	bio = strings.TrimSpace(bio)
	if bio == "" {
		return "Soda check-ins from @" + handle
	}
	runes := []rune(bio)
	if len(runes) > 155 {
		return string(runes[:155])
	}
	return bio
}
