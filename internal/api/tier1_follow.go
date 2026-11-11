package api

import (
	"errors"
	"net/http"

	"sodapop/internal/auth"
	"sodapop/internal/checkins"
	"sodapop/internal/social"
)

// followUserDTO is the wire shape of one member in a follower/following list.
// avatar_url follows the same authenticated media route the rest of the API
// uses, and is null when the member has no avatar.
func followUserDTO(u social.FollowUser) map[string]any {
	return map[string]any{
		"handle":             u.Handle,
		"display_name":       u.DisplayName,
		"avatar_url":         avatarURL(u.AvatarMediaID),
		"bio":                u.Bio,
		"followed_by_viewer": u.FollowedByViewer,
		"is_friend":          u.IsFriend,
	}
}

// userFollowers lists the members following the profile in the path.
func (s *Server) userFollowers(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	return s.followListResponse(w, r, tok, true)
}

// userFollowing lists the members the profile in the path follows.
func (s *Server) userFollowing(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	return s.followListResponse(w, r, tok, false)
}

// followListResponse resolves the handle, applies the same visibility rule the
// profile page uses, and returns one keyset page. A private profile, a deleted
// account and a blocked pair are all a 404 so the list cannot be used to probe
// for an account the viewer may not see.
func (s *Server) followListResponse(w http.ResponseWriter, r *http.Request, tok *auth.APIToken, followers bool) error {
	ctx := r.Context()
	target, err := checkins.LookupProfile(ctx, s.Auth.Client, r.PathValue("handle"))
	if err != nil {
		return err
	}

	if target.ID != tok.User.ID {
		blocked, err := s.Social.AreBlocked(ctx, tok.User.ID, target.ID)
		if err != nil {
			return err
		}
		if blocked {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return nil
		}
	}

	areFriends := target.ID != tok.User.ID && s.Social.AreFriends(ctx, tok.User.ID, target.ID)
	prefs, err := s.Auth.GetOrCreateSettings(ctx, target.ID)
	if err != nil {
		return err
	}
	// LookupProfile already excludes deleted accounts and deleted status, so the
	// deleted flag is always false here; the public-profile gate is the part
	// that matters.
	if checkins.DecideProfileAccess(prefs.PublicProfile, false, tok.User.ID, target.ID, areFriends) != checkins.ProfileVisible {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}

	opts := social.FollowListOptions{
		ViewerID: tok.User.ID,
		Cursor:   r.URL.Query().Get("before"),
		Limit:    queryInt(r, "limit", social.DefaultFollowPageSize, social.MaxFollowPageSize),
	}
	var page *social.FollowPage
	if followers {
		page, err = s.Social.Followers(ctx, target.ID, opts)
	} else {
		page, err = s.Social.Following(ctx, target.ID, opts)
	}
	if err != nil {
		if errors.Is(err, social.ErrInvalidCursor) {
			return bad("before cursor is invalid")
		}
		return err
	}

	out := make([]map[string]any, 0, len(page.Users))
	for _, u := range page.Users {
		out = append(out, followUserDTO(u))
	}
	meta := map[string]any{"count": len(out), "has_more": page.HasMore, "next_cursor": nil}
	if page.HasMore {
		meta["next_cursor"] = page.NextCursor
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": out, "meta": meta})
	return nil
}
