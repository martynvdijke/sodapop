package web

import (
	"net/http"
	"net/url"
	"strings"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
)

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

	before := strings.TrimSpace(r.URL.Query().Get("before"))
	const pageSize = 20
	items, err := s.Checkins.ProfileCheckins(ctx, checkins.ProfileCheckinsFilter{
		ViewerID: viewerID,
		TargetID: target.ID,
		Limit:    pageSize,
		Before:   before,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
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

	displayName := strings.TrimSpace(target.DisplayName)
	if displayName == "" {
		displayName = target.Handle
	}
	isOwner := viewerID != "" && viewerID == target.ID
	indexable := public
	robots := "noindex,nofollow"
	if indexable {
		robots = "index,follow"
	}

	profile := map[string]any{
		"Handle":        target.Handle,
		"DisplayName":   displayName,
		"Bio":           target.Bio,
		"Initial":       initial(displayName),
		"JoinedAt":      target.CreatedAt,
		"PublicProfile": public,
	}
	statMap := map[string]any{
		"Pours":  stats.Pours,
		"Styles": stats.Styles,
		"Venues": stats.Venues,
		"Badges": stats.Badges,
	}

	nextBefore := ""
	hasMore := len(items) == pageSize
	if hasMore {
		nextBefore = items[len(items)-1].CreatedAt
	}

	if !indexable {
		// The middleware leaves /u/* alone so opted-in profiles can be indexed;
		// private profiles must explicitly opt back out.
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	}

	s.render(w, r, http.StatusOK, "profile", map[string]any{
		"Title":           displayName,
		"Profile":         profile,
		"Stats":           statMap,
		"Checkins":        items,
		"Lists":           lists,
		"IsOwner":         isOwner,
		"IsFriend":        areFriends,
		"Authenticated":   viewerID != "",
		"Indexable":       indexable,
		"CanonicalURL":    s.Cfg.BaseURL + "/u/" + target.Handle,
		"MetaRobots":      robots,
		"MetaTitle":       displayName + " (@" + target.Handle + ") · Sodapop",
		"MetaDescription": profileDescription(target.Bio, target.Handle),
		"NextBefore":      nextBefore,
		"HasMore":         hasMore,
	})
}

func (s *Server) profileNotFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusNotFound, "error", map[string]any{
		"Title":   "Not found",
		"Message": "That profile is not in Sodapop.",
	})
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
