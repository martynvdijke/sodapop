package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
	"sodapop/internal/social"
)

// followRowView is one member in a follower or following list. The booleans are
// viewer-relative, so the same row renders a Follow or Unfollow button depending
// on who is looking.
type followRowView struct {
	ID               string
	Handle           string
	DisplayName      string
	AvatarID         string
	Bio              string
	FollowedByViewer bool
	IsFriend         bool
	IsViewer         bool
}

// followersPage lists the members following the profile in the URL.
func (s *Server) followersPage(w http.ResponseWriter, r *http.Request) {
	s.renderFollowList(w, r, true)
}

// followingPage lists the members the profile in the URL follows.
func (s *Server) followingPage(w http.ResponseWriter, r *http.Request) {
	s.renderFollowList(w, r, false)
}

// renderFollowList loads one keyset page in either direction and renders the
// shared list template. The two routes differ only in direction, title and tab.
func (s *Server) renderFollowList(w http.ResponseWriter, r *http.Request, followers bool) {
	viewer := UserFromContext(r)
	if viewer == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusFound)
		return
	}

	target, ok := s.visibleProfileTarget(r.Context(), r, r.PathValue("handle"))
	if !ok {
		s.profileNotFound(w, r)
		return
	}

	before := strings.TrimSpace(r.URL.Query().Get("before"))
	opts := social.FollowListOptions{ViewerID: viewer.ID, Cursor: before}

	load := func(cursor string) (*social.FollowPage, error) {
		opts.Cursor = cursor
		if followers {
			return s.Social.Followers(r.Context(), target.ID, opts)
		}
		return s.Social.Following(r.Context(), target.ID, opts)
	}

	page, err := load(before)
	if err != nil {
		if errors.Is(err, social.ErrInvalidCursor) {
			// A stale cursor is treated as the first page rather than an error.
			before = ""
			page, err = load("")
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}

	rows := make([]followRowView, 0, len(page.Users))
	for _, u := range page.Users {
		row := followRowView{
			ID:               u.ID,
			Handle:           u.Handle,
			DisplayName:      u.DisplayName,
			Bio:              u.Bio,
			FollowedByViewer: u.FollowedByViewer,
			IsFriend:         u.IsFriend,
			IsViewer:         u.ID == viewer.ID,
		}
		if row.DisplayName == "" {
			row.DisplayName = u.Handle
		}
		if u.AvatarMediaID != nil {
			row.AvatarID = *u.AvatarMediaID
		}
		rows = append(rows, row)
	}

	title := "Followers"
	mode := "followers"
	otherMode := "following"
	otherLabel := "Following"
	empty := "Nobody follows @" + target.Handle + " yet."
	if !followers {
		title = "Following"
		mode = "following"
		otherMode = "followers"
		otherLabel = "Followers"
		empty = "@" + target.Handle + " does not follow anyone yet."
	}

	s.render(w, r, http.StatusOK, "follow_list", map[string]any{
		"Title":      title + " · @" + target.Handle,
		"Heading":    title,
		"Mode":       mode,
		"OtherMode":  otherMode,
		"OtherLabel": otherLabel,
		"Handle":     target.Handle,
		"DisplayName": func() string {
			if target.DisplayName != "" {
				return target.DisplayName
			}
			return target.Handle
		}(),
		"Users":          rows,
		"Count":          len(rows),
		"NextCursor":     page.NextCursor,
		"HasMore":        page.HasMore,
		"ShowingEarlier": before != "",
		"EmptyText":      empty,
		"IsOwner":        target.ID == viewer.ID,
	})
}

// visibleProfileTarget resolves a handle for a per-user list page using the same
// rule the profile page applies: a missing, deleted or deleted-status account is
// hidden; a block in either direction is hidden; and a non-public profile is
// hidden unless the viewer is the owner or an accepted friend.
//
// It returns ok=false for every refusal so the caller can answer 404 without
// leaking which rule matched, exactly like profilePage.
func (s *Server) visibleProfileTarget(ctx context.Context, r *http.Request, rawHandle string) (*ent.User, bool) {
	handle := strings.ToLower(strings.TrimSpace(rawHandle))
	if handle == "" {
		return nil, false
	}
	target, err := s.Auth.Client.User.Query().
		Where(user.HandleEqualFold(handle), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		return nil, false
	}

	viewer := UserFromContext(r)
	viewerID := ""
	if viewer != nil {
		viewerID = viewer.ID
	}
	if viewerID != "" && viewerID != target.ID {
		blocked, err := s.Social.AreBlocked(ctx, viewerID, target.ID)
		if err != nil || blocked {
			return nil, false
		}
	}
	areFriends := viewerID != "" && viewerID != target.ID && s.Social.AreFriends(ctx, viewerID, target.ID)
	prefs, err := s.Auth.GetOrCreateSettings(ctx, target.ID)
	if err != nil {
		return nil, false
	}
	switch checkins.DecideProfileAccess(prefs.PublicProfile, target.Status == user.StatusDeleted, viewerID, target.ID, areFriends) {
	case checkins.ProfileVisible:
		return target, true
	default:
		return nil, false
	}
}
