package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
	"sodapop/internal/sharecard"
	"sodapop/internal/sharelinks"
)

// shareLinkView is the owner-facing row on /passport and /you.
type shareLinkView struct {
	ID        string
	Kind      string
	KindLabel string
	URL       string
	CreatedOn string
}

// shareCell is one small stat on the public share page.
type shareCell struct {
	Label string
	Value int
}

// shareView is the template contract for pages/share.html.
type shareView struct {
	URL            string
	CardURL        string
	Kind           string
	KindLabel      string
	DisplayName    string
	Handle         string
	Initial        string
	HeadlineValue  int
	HeadlineLabel  string
	Cells          []shareCell
	Checkins       int
	CurrentStreak  int
	GeneratedOn    string
	ImageAlt       string
	KindDescriptor string
}

func shareKindLabel(kind string) string {
	if kind == "streak" {
		return "Streak card"
	}
	return "Passport card"
}

func (s *Server) buildShareView(r *http.Request, link *sharelinks.Link, ownerHandle, ownerName string, passport *checkins.Passport, stats *checkins.Stats) shareView {
	handle := strings.TrimSpace(ownerHandle)
	name := strings.TrimSpace(ownerName)
	if name == "" {
		name = handle
	}
	if name == "" {
		name = "Sodapop member"
	}
	relative := "/share/" + link.Token
	absolute := s.absoluteURL(r, relative)

	kind := "passport"
	if link.Kind == "streak" {
		kind = "streak"
	}
	headline := passport.Totals.Checkins
	headlineLabel := "check-ins logged"
	if headline == 1 {
		headlineLabel = "check-in logged"
	}
	if kind == "streak" {
		headline = stats.CurrentStreak
		headlineLabel = "days in a row"
		if headline == 1 {
			headlineLabel = "day in a row"
		}
	}

	cells := []shareCell{
		{Label: "Styles", Value: passport.Totals.Styles},
		{Label: "Venue kinds", Value: passport.Totals.VenueKinds},
		{Label: "Countries", Value: passport.Totals.Countries},
		{Label: "Longest streak", Value: stats.LongestStreak},
	}
	visible := make([]shareCell, 0, 4)
	for _, cell := range cells {
		if cell.Value > 0 {
			visible = append(visible, cell)
		}
	}
	if kind == "streak" {
		// The headline is already the current streak; show totals instead of
		// repeating the same number twice.
		visible = []shareCell{
			{Label: "Check-ins", Value: passport.Totals.Checkins},
			{Label: "Styles", Value: passport.Totals.Styles},
			{Label: "Venue kinds", Value: passport.Totals.VenueKinds},
			{Label: "Countries", Value: passport.Totals.Countries},
		}
		filtered := make([]shareCell, 0, 4)
		for _, cell := range visible {
			if cell.Value > 0 {
				filtered = append(filtered, cell)
			}
		}
		visible = filtered
	}
	if len(visible) > 4 {
		visible = visible[:4]
	}

	kindDescriptor := "soda passport"
	if kind == "streak" {
		kindDescriptor = "soda streak"
	}
	return shareView{
		URL:            absolute,
		CardURL:        absolute + "/card.png",
		Kind:           kind,
		KindLabel:      shareKindLabel(kind),
		DisplayName:    name,
		Handle:         handle,
		Initial:        initial(name),
		HeadlineValue:  headline,
		HeadlineLabel:  headlineLabel,
		Cells:          visible,
		Checkins:       passport.Totals.Checkins,
		CurrentStreak:  stats.CurrentStreak,
		GeneratedOn:    time.Now().UTC().Format("2 Jan 2006"),
		ImageAlt:       "Sodapop " + kindDescriptor + " card for " + name,
		KindDescriptor: kindDescriptor,
	}
}

// sharePage renders the public HTML card. It is intentionally noindex: the
// capability URL is for people and crawlers that fetch previews, not search.
func (s *Server) sharePage(w http.ResponseWriter, r *http.Request) {
	if s.ShareLinks == nil {
		s.shareNotFound(w, r, sharelinks.ErrNotFound)
		return
	}
	if !s.allowShareView(w, r) {
		return
	}
	link, err := s.ShareLinks.Resolve(r.Context(), r.PathValue("token"))
	if err != nil {
		s.shareNotFound(w, r, err)
		return
	}
	owner, err := s.shareOwner(r.Context(), link.UserID)
	if err != nil {
		s.shareNotFound(w, r, err)
		return
	}
	passport, stats, err := s.shareStats(r.Context(), owner.ID)
	if err != nil {
		s.shareNotFound(w, r, err)
		return
	}
	view := s.buildShareView(r, link, owner.Handle, owner.DisplayName, passport, stats)
	description := shareDescription(view)
	s.render(w, r, http.StatusOK, "share", map[string]any{
		"Title":           view.DisplayName,
		"Share":           view,
		"MetaTitle":       view.DisplayName + " · " + view.KindLabel + " · Sodapop",
		"MetaDescription": description,
		"CanonicalURL":    view.URL,
		"OGImage":         view.CardURL,
		"OGImageAlt":      view.ImageAlt,
		"OGType":          "website",
		"TwitterCard":     "summary_large_image",
		"MetaRobots":      "noindex,nofollow",
	})
}

// shareCard serves the raster preview. It is cacheable for a few minutes so
// crawler retries and chat previews do not re-render on every fetch.
func (s *Server) shareCard(w http.ResponseWriter, r *http.Request) {
	if s.ShareLinks == nil {
		s.shareNotFound(w, r, sharelinks.ErrNotFound)
		return
	}
	if !s.allowShareView(w, r) {
		return
	}
	link, err := s.ShareLinks.Resolve(r.Context(), r.PathValue("token"))
	if err != nil {
		s.shareNotFound(w, r, err)
		return
	}
	owner, err := s.shareOwner(r.Context(), link.UserID)
	if err != nil {
		s.shareNotFound(w, r, err)
		return
	}
	passport, stats, err := s.shareStats(r.Context(), owner.ID)
	if err != nil {
		s.shareNotFound(w, r, err)
		return
	}
	png, err := sharecard.Render(sharecard.Data{
		Handle:        owner.Handle,
		DisplayName:   owner.DisplayName,
		Checkins:      passport.Totals.Checkins,
		Styles:        passport.Totals.Styles,
		Countries:     passport.Totals.Countries,
		VenueKinds:    passport.Totals.VenueKinds,
		CurrentStreak: stats.CurrentStreak,
		LongestStreak: stats.LongestStreak,
		Kind:          link.Kind,
		GeneratedAt:   time.Now().UTC(),
	})
	if err != nil {
		s.shareFail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// Deliberate tradeoff: a shared cache may serve a revoked card for up to
	// five minutes. The capability URL stays unguessable and the HTML page,
	// which is not cached, stops resolving the token immediately.
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// allowShareView applies a per-client-IP budget to anonymous share reads
// before the token is resolved. It protects both the aggregate queries and the
// PNG rasterizer from cheap enumeration or amplification.
func (s *Server) allowShareView(w http.ResponseWriter, r *http.Request) bool {
	if s.Limiter == nil {
		return true
	}
	if !s.Limiter.Allow("shareview:"+s.setupSourceIP(r), 60, time.Minute) {
		http.Error(w, "too many requests; try again later", http.StatusTooManyRequests)
		return false
	}
	return true
}

func (s *Server) shareNotFound(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sharelinks.ErrNotFound) {
		s.render(w, r, http.StatusNotFound, "error", map[string]any{
			"Title":   "Not found",
			"Message": "That share link is not available.",
		})
		return
	}
	s.shareFail(w, r, err)
}

// shareFail logs a share failure without writing the capability token from the
// URL path into the logs.
func (s *Server) shareFail(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("share request failed", "err", err)
	s.render(w, r, http.StatusInternalServerError, "error", map[string]any{
		"Title": "Something went wrong", "Message": "The request could not be completed. Try again.",
	})
}

func (s *Server) shareOwner(ctx context.Context, userID string) (*userView, error) {
	row, err := s.Auth.Client.User.Query().
		Where(
			user.ID(userID),
			user.DeletedAtIsNil(),
			user.StatusNEQ(user.StatusDeleted),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, sharelinks.ErrNotFound
		}
		return nil, err
	}
	return &userView{ID: row.ID, Handle: row.Handle, DisplayName: row.DisplayName}, nil
}

// userView is the narrow identity the share page needs.
type userView struct {
	ID          string
	Handle      string
	DisplayName string
}

func (s *Server) shareStats(ctx context.Context, userID string) (*checkins.Passport, *checkins.Stats, error) {
	passport, err := s.Checkins.Passport(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	stats, err := s.Checkins.Stats(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return passport, stats, nil
}

func shareDescription(view shareView) string {
	name := view.DisplayName
	if view.Handle != "" {
		name = "@" + view.Handle
	}
	if view.Kind == "streak" {
		if view.CurrentStreak == 1 {
			return name + " is on a 1-day soda streak on Sodapop."
		}
		return name + " is on a " + strconv.Itoa(view.CurrentStreak) + "-day soda streak on Sodapop."
	}
	return name + " has logged " + strconv.Itoa(view.Checkins) + " soda check-ins on Sodapop."
}

// shareCreate mints a new link and sends the owner back to the page they used.
func (s *Server) shareCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.ShareLinks == nil {
		s.fail(w, r, errors.New("share links are not configured"))
		return
	}
	u := UserFromContext(r)
	kind := strings.TrimSpace(r.FormValue("kind"))
	back := safeBackPath(r, "/passport")
	if !sharelinks.ValidKind(kind) {
		http.Redirect(w, r, withQuery(back, "share_error", "invalid"), http.StatusFound)
		return
	}
	if s.Limiter != nil && !s.Limiter.Allow("share:"+u.ID, 20, time.Hour) {
		http.Error(w, "too many share links; try again later", http.StatusTooManyRequests)
		return
	}
	link, err := s.ShareLinks.Create(r.Context(), u.ID, kind)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, withQuery(back, "share", "/share/"+link.Token), http.StatusFound)
}

// shareRevoke revokes one of the signed-in user's links. The service scopes the
// update by user id, so an id guessed from elsewhere changes nothing.
func (s *Server) shareRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.ShareLinks == nil {
		s.fail(w, r, errors.New("share links are not configured"))
		return
	}
	u := UserFromContext(r)
	if _, err := s.ShareLinks.Revoke(r.Context(), u.ID, r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, safeBackPath(r, "/passport"), http.StatusFound)
}

// shareLinkViews lists the owner's active links for the management panel.
func (s *Server) shareLinkViews(r *http.Request, userID string) []shareLinkView {
	if s.ShareLinks == nil {
		return nil
	}
	links, err := s.ShareLinks.ListForUser(r.Context(), userID, 20)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("list share links", "err", err)
		}
		return nil
	}
	views := make([]shareLinkView, 0, len(links))
	for _, link := range links {
		views = append(views, shareLinkView{
			ID:        link.ID,
			Kind:      link.Kind,
			KindLabel: shareKindLabel(link.Kind),
			URL:       s.absoluteURL(r, "/share/"+link.Token),
			CreatedOn: shareDate(link.CreatedAt),
		})
	}
	return views
}

func shareDate(value string) string {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return t.Format("2 Jan 2006")
}

// absoluteURL builds a crawler-safe absolute URL. Configured BaseURL wins so a
// reverse proxy and the site identity agree; otherwise the request host is used.
func (s *Server) absoluteURL(r *http.Request, path string) string {
	if base := strings.TrimRight(s.baseURL(), "/"); base != "" {
		return base + path
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	if r.Host == "" {
		return path
	}
	return scheme + "://" + r.Host + path
}

// safeBackPath accepts only a same-host absolute path from the Referer. It
// falls back to a known page instead of ever redirecting off-site.
func safeBackPath(r *http.Request, fallback string) string {
	ref := strings.TrimSpace(r.Header.Get("Referer"))
	if ref == "" {
		return fallback
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return fallback
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, r.Host) {
		return fallback
	}
	p := parsed.EscapedPath()
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return fallback
	}
	if parsed.RawQuery != "" {
		p += "?" + parsed.RawQuery
	}
	return p
}

func withQuery(target, key, value string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	q := parsed.Query()
	q.Set(key, value)
	parsed.RawQuery = q.Encode()
	return parsed.RequestURI()
}

// shareNotice reads the post-create URL and any fixed error code from the
// query. The URL is only shown when it resolves to an active link owned by
// userID, so a crafted ?share= cannot make the page advertise somebody else's
// capability token.
func (s *Server) shareNotice(r *http.Request, userID string) (string, string) {
	shareURL := strings.TrimSpace(r.URL.Query().Get("share"))
	if !strings.HasPrefix(shareURL, "/share/") || strings.Contains(shareURL, "..") {
		shareURL = ""
	}
	if shareURL != "" && !s.ownsShareURL(r.Context(), userID, shareURL) {
		shareURL = ""
	}
	message := ""
	switch r.URL.Query().Get("share_error") {
	case "invalid":
		message = "Choose a valid share card to continue."
	}
	return shareURL, message
}

// ownsShareURL reports whether path names an active capability link belonging
// to userID. Unknown, revoked, and foreign tokens all return false.
func (s *Server) ownsShareURL(ctx context.Context, userID, path string) bool {
	if s.ShareLinks == nil {
		return false
	}
	token := strings.TrimPrefix(path, "/share/")
	if token == "" || strings.ContainsAny(token, "/?#") {
		return false
	}
	link, err := s.ShareLinks.Resolve(ctx, token)
	if err != nil {
		return false
	}
	return link.UserID == userID
}
