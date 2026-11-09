package api

import (
	"net/http"
	"strconv"
	"time"

	"sodapop/internal/checkins"
)

const (
	publicPreviewLimit = 30
	publicPreviewRate  = time.Minute
)

// publicPreviewLimited applies a read-only IP bucket to the logged-out preview.
// It is intentionally separate from the auth bucket: browsing is cheap and
// should not consume a visitor's sign-in attempts.
func (s *Server) publicPreviewLimited(w http.ResponseWriter, r *http.Request, bucket string) bool {
	if s.Limiter == nil {
		return false
	}
	if s.Limiter.Allow("preview:"+bucket+":"+s.clientIP(r), publicPreviewLimit, publicPreviewRate) {
		return false
	}
	w.Header().Set("Retry-After", "60")
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; try again later")
	return true
}

func setPreviewCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=60")
}

func previewLimit(r *http.Request, def, max int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// publicPreviewFeed handles GET /api/v1/public/feed.
func (s *Server) publicPreviewFeed(w http.ResponseWriter, r *http.Request) {
	if s.publicPreviewLimited(w, r, "feed") {
		return
	}
	before := r.URL.Query().Get("before")
	limit := previewLimit(r, 20, 50)
	items, err := s.Checkins.PublicFeed(r.Context(), checkins.PublicFeedFilter{
		Limit:  limit,
		Before: before,
	})
	if err != nil {
		s.mapError(w, err)
		return
	}
	data := make([]map[string]any, 0, len(items))
	for _, it := range items {
		data = append(data, publicItemDTO(it))
	}
	next := ""
	if len(items) == limit {
		last := items[len(items)-1]
		next = checkins.Cursor(last.CreatedAt, last.ID)
	}
	setPreviewCache(w)
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "next_before": next})
}

// publicPreviewPeople handles GET /api/v1/public/people.
func (s *Server) publicPreviewPeople(w http.ResponseWriter, r *http.Request) {
	if s.publicPreviewLimited(w, r, "people") {
		return
	}
	people, err := s.Checkins.PublicPeople(r.Context(), previewLimit(r, 24, 60))
	if err != nil {
		s.mapError(w, err)
		return
	}
	data := make([]map[string]any, 0, len(people))
	for _, p := range people {
		data = append(data, publicPersonDTO(p))
	}
	setPreviewCache(w)
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// publicPreviewUser handles GET /api/v1/public/users/{handle}.
func (s *Server) publicPreviewUser(w http.ResponseWriter, r *http.Request) {
	if s.publicPreviewLimited(w, r, "user") {
		return
	}
	profile, stats, err := s.Checkins.PublicProfile(r.Context(), r.PathValue("handle"))
	if err != nil {
		s.mapError(w, err)
		return
	}
	owner, err := s.Auth.Client.User.Get(r.Context(), profile.ID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	favourite, err := s.favouriteDrinkCard(r.Context(), owner.FavouriteDrinkID)
	if err != nil {
		s.mapError(w, err)
		return
	}
	setPreviewCache(w)
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"handle":          profile.Handle,
		"display_name":    profile.DisplayName,
		"bio":             profile.Bio,
		"joined_at":       profile.CreatedAt,
		"avatar_url":      publicAvatarURL(owner.AvatarMediaID),
		"favourite_drink": favourite,
		"stats": map[string]any{
			"pours":         stats.Pours,
			"styles":        stats.Styles,
			"venues":        stats.Venues,
			"badges":        stats.Badges,
			"unique_drinks": stats.UniqueDrinks,
			"toasts":        stats.Toasts,
		},
	}})
	return
}

// publicPreviewUserCheckins handles GET /api/v1/public/users/{handle}/checkins.
func (s *Server) publicPreviewUserCheckins(w http.ResponseWriter, r *http.Request) {
	if s.publicPreviewLimited(w, r, "user-checkins") {
		return
	}
	limit := previewLimit(r, 20, 50)
	items, err := s.Checkins.PublicUserCheckins(r.Context(), r.PathValue("handle"), limit, r.URL.Query().Get("before"))
	if err != nil {
		s.mapError(w, err)
		return
	}
	data := make([]map[string]any, 0, len(items))
	for _, it := range items {
		data = append(data, publicItemDTO(it))
	}
	next := ""
	if len(items) == limit {
		last := items[len(items)-1]
		next = checkins.Cursor(last.CreatedAt, last.ID)
	}
	setPreviewCache(w)
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "next_before": next})
}

// publicPreviewCheckin handles GET /api/v1/public/checkins/{id}.
func (s *Server) publicPreviewCheckin(w http.ResponseWriter, r *http.Request) {
	if s.publicPreviewLimited(w, r, "checkin") {
		return
	}
	detail, err := s.Checkins.PublicCheckin(r.Context(), r.PathValue("id"))
	if err != nil {
		s.mapError(w, err)
		return
	}
	setPreviewCache(w)
	writeJSON(w, http.StatusOK, map[string]any{"data": publicCheckinDTO(detail)})
}

func publicItemDTO(it checkins.ProfileItem) map[string]any {
	data := map[string]any{
		"id": it.ID,
		"user": map[string]any{
			"handle":       it.Handle,
			"display_name": it.DisplayName,
		},
		"drink": map[string]any{
			"slug":      it.DrinkSlug,
			"name":      it.DrinkName,
			"brand":     it.Brand,
			"container": it.Container,
			"volume_ml": it.VolumeML,
		},
		"rating":     it.Rating,
		"note":       it.Note,
		"toasts":     it.Toasts,
		"at_home":    it.AtHome,
		"created_at": it.CreatedAt,
	}
	if it.VenueName != "" {
		data["venue"] = map[string]any{"name": it.VenueName}
	}
	if it.PhotoID != "" {
		data["photo"] = map[string]any{
			"id":        it.PhotoID,
			"mime":      it.PhotoMime,
			"url":       "/api/v1/public/media/" + it.PhotoID,
			"thumb_url": "/api/v1/public/media/" + it.PhotoID + "/thumb",
		}
	}
	return data
}

func publicPersonDTO(p checkins.PublicPerson) map[string]any {
	data := map[string]any{
		"handle":       p.Handle,
		"display_name": p.DisplayName,
		"bio":          p.Bio,
		"pours":        p.Pours,
		"styles":       p.Styles,
		"last_pour":    p.LastPour,
	}
	if p.AvatarID != "" {
		data["avatar_url"] = "/api/v1/public/media/" + p.AvatarID
	}
	return data
}

func publicCheckinDTO(d *checkins.Detail) map[string]any {
	data := map[string]any{
		"id": d.ID,
		"user": map[string]any{
			"handle":       d.UserHandle,
			"display_name": d.UserName,
		},
		"drink": map[string]any{
			"slug":  d.DrinkSlug,
			"name":  d.DrinkName,
			"brand": d.Brand,
			"style": d.Style,
		},
		"rating":         d.Rating,
		"note":           d.Note,
		"visibility":     d.Visibility,
		"at_home":        d.AtHome,
		"toasts":         d.Toasts,
		"comments_count": d.CommentsCount,
		"created_at":     d.CreatedAt,
	}
	if d.VenueID != nil {
		data["venue"] = map[string]any{"id": *d.VenueID, "name": d.VenueName}
	}
	if d.MediaID != "" {
		data["photo"] = map[string]any{
			"id":        d.MediaID,
			"mime":      d.MediaMime,
			"url":       "/api/v1/public/media/" + d.MediaID,
			"thumb_url": "/api/v1/public/media/" + d.MediaID + "/thumb",
		}
	}
	return data
}

// publicMediaFull and publicMediaThumb stream public photos without a token.
// The media service still performs the full visibility check with an empty
// viewer, so only public check-in photos, avatars and catalog images resolve.
func (s *Server) publicMediaFull(w http.ResponseWriter, r *http.Request) {
	s.serveAPIMediaAs(w, r, "", false)
}

func (s *Server) publicMediaThumb(w http.ResponseWriter, r *http.Request) {
	s.serveAPIMediaAs(w, r, "", true)
}
