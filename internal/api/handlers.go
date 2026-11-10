package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/follow"
	"sodapop/internal/auth"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/geocode"
	"sodapop/internal/integrations"
	"sodapop/internal/media"
	"sodapop/internal/search"
	"sodapop/internal/social"
	"sodapop/internal/venues"
)

// me returns the authenticated identity and token scopes. It is the only
// endpoint that reveals the account email address.
func (s *Server) me(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	scopes := make([]string, 0, len(tok.Scopes))
	for sc := range tok.Scopes {
		scopes = append(scopes, sc)
	}
	u, err := s.Auth.Client.User.Get(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	favourite, err := s.favouriteDrinkCard(r.Context(), u.FavouriteDrinkID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"id":                 u.ID,
		"handle":             u.Handle,
		"display_name":       u.DisplayName,
		"bio":                u.Bio,
		"email":              u.Email,
		"email_verified":     u.EmailVerifiedAt != nil,
		"role":               u.Role,
		"default_visibility": u.DefaultVisibility,
		"scopes":             scopes,
		"created_at":         u.CreatedAt,
		"avatar_url":         avatarURL(u.AvatarMediaID),
		"favourite_drink":    favourite,
	}})
	return nil
}

func (s *Server) myCheckins(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	filter, err := parseCheckinFilters(r)
	if err != nil {
		return err
	}
	filter.ViewerID = tok.User.ID
	filter.TargetID = tok.User.ID
	res, err := s.Checkins.ProfileCheckinsPage(r.Context(), filter)
	if err != nil {
		return err
	}
	items := profileItemsAsItems(res.Items)
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "meta": checkinPageMeta(res)})
	return nil
}

func (s *Server) myStats(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	stats, err := s.Checkins.Stats(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": stats})
	return nil
}

// myActivity returns the caller's own activity timeline. It is self-only by
// construction and has no cross-user or global variant.
func (s *Server) myActivity(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	items, err := s.Social.ActivityForUser(r.Context(), tok.User.ID, queryInt(r, "limit", 20, 50))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "meta": map[string]any{"count": len(items)}})
	return nil
}

func (s *Server) feed(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	items, err := s.Social.Feed(r.Context(), tok.User.ID, queryInt(r, "limit", 20, 50), r.URL.Query().Get("before"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "meta": map[string]any{"count": len(items)}})
	return nil
}

func (s *Server) friends(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	f, err := s.Social.Friends(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": f})
	return nil
}

func (s *Server) requestFriend(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Handle string `json:"handle"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Handle) == "" {
		return bad("handle is required")
	}
	if !s.Limiter.Allow("friendreq:"+tok.User.ID, 20, time.Hour) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many friend requests")
		return nil
	}
	// Unknown handles return the same accepted response, so the endpoint cannot
	// be used to enumerate accounts.
	_, err := s.Social.Request(r.Context(), tok.User.ID, in.Handle)
	if err != nil && !errors.Is(err, social.ErrSelfAction) {
		return err
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"status": "requested"}})
	return nil
}

func (s *Server) acceptFriend(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Social.Accept(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "accepted"}})
	return nil
}

func (s *Server) removeFriend(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Social.Remove(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) userProfile(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	ctx := r.Context()
	p, err := checkins.LookupProfile(ctx, s.Auth.Client, r.PathValue("handle"))
	if err != nil {
		return err
	}
	isBlocked := false
	if p.ID != tok.User.ID {
		isBlocked, err = s.Social.AreBlocked(ctx, tok.User.ID, p.ID)
		if err != nil {
			return err
		}
		if isBlocked {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return nil
		}
	}

	stats, err := s.Checkins.ProfileStats(ctx, tok.User.ID, p.ID)
	if err != nil {
		return err
	}
	topStyles, err := s.Checkins.ProfileTopStyles(ctx, tok.User.ID, p.ID, 3)
	if err != nil {
		return err
	}
	lists, err := s.Checkins.ProfileLists(ctx, tok.User.ID, p.ID)
	if err != nil {
		return err
	}

	// Only earned badges are profile data. Unearned definitions stay private to
	// the owner's badge page.
	statuses, err := s.Badges.ListForUser(ctx, p.ID)
	if err != nil {
		return err
	}
	badges := make([]map[string]any, 0, len(statuses))
	for _, st := range statuses {
		if !st.Earned {
			continue
		}
		badges = append(badges, map[string]any{
			"slug": st.Slug, "name": st.Name, "description": st.Description,
			"awarded_at": st.AwardedAt,
		})
	}

	isFollowing := false
	if p.ID != tok.User.ID {
		isFollowing, _ = s.Auth.Client.Follow.Query().
			Where(follow.FollowerID(tok.User.ID), follow.FolloweeID(p.ID)).Exist(ctx)
	}
	followerCount, _ := s.Auth.Client.Follow.Query().Where(follow.FolloweeID(p.ID)).Count(ctx)
	followingCount, _ := s.Auth.Client.Follow.Query().Where(follow.FollowerID(p.ID)).Count(ctx)

	listOut := make([]map[string]any, 0, len(lists))
	for _, l := range lists {
		listOut = append(listOut, map[string]any{
			"id": l.ID, "name": l.Name, "description": l.Description,
			"visibility": l.Visibility, "item_count": l.Items,
		})
	}
	styleOut := make([]map[string]any, 0, len(topStyles))
	for _, st := range topStyles {
		styleOut = append(styleOut, map[string]any{
			"slug": st.Slug, "name": st.Name, "count": st.Count,
		})
	}

	owner, err := s.Auth.Client.User.Get(ctx, p.ID)
	if err != nil {
		return err
	}
	favourite, err := s.favouriteDrinkCard(ctx, owner.FavouriteDrinkID)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"handle":          p.Handle,
		"display_name":    p.DisplayName,
		"bio":             p.Bio,
		"created_at":      p.CreatedAt,
		"avatar_url":      avatarURL(owner.AvatarMediaID),
		"favourite_drink": favourite,
		"stats": map[string]any{
			"checkins":        stats.Pours,
			"distinct_drinks": stats.UniqueDrinks,
			"toasts_received": stats.Toasts,
		},
		"follower_count":  followerCount,
		"following_count": followingCount,
		"is_following":    isFollowing,
		"is_friend":       s.Social.AreFriends(ctx, tok.User.ID, p.ID),
		"can_block":       p.ID != tok.User.ID,
		"badges":          badges,
		"top_styles":      styleOut,
		"lists":           listOut,
	}})
	return nil
}

func (s *Server) userCheckins(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	p, err := checkins.LookupProfile(r.Context(), s.Auth.Client, r.PathValue("handle"))
	if err != nil {
		return err
	}
	if p.ID != tok.User.ID {
		blocked, err := s.Social.AreBlocked(r.Context(), tok.User.ID, p.ID)
		if err != nil {
			return err
		}
		if blocked || !s.Social.AreFriends(r.Context(), tok.User.ID, p.ID) {
			// Return 404 rather than 403 so token holders cannot probe for accounts.
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return nil
		}
	}
	filter, err := parseCheckinFilters(r)
	if err != nil {
		return err
	}
	filter.ViewerID = tok.User.ID
	filter.TargetID = p.ID
	res, err := s.Checkins.ProfileCheckinsPage(r.Context(), filter)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": profileItemsAsItems(res.Items),
		"meta": checkinPageMeta(res),
	})
	return nil
}

func (s *Server) getDrink(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	d, err := s.Catalog.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": drinkDTO(d)})
	return nil
}

// search returns at most ten results. There is no cursor, page count or total,
// and an empty query is rejected: the endpoint cannot be walked as a catalog.
// With type=all the data envelope holds grouped drinks, brands and styles.
func (s *Server) search(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	q := r.URL.Query().Get("q")
	if strings.EqualFold(r.URL.Query().Get("type"), "all") {
		if len([]rune(strings.TrimSpace(q))) < catalog.MinSearchLen {
			return catalog.ErrQueryTooShort
		}
		results, err := search.New(s.Catalog).Search(r.Context(), q, 0)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"drinks": results.Drinks,
				"brands": results.Brands,
				"styles": results.Styles,
			},
			"meta": map[string]any{"count": results.Total, "limit": 10, "note": "results are capped; the catalog cannot be enumerated"},
		})
		return nil
	}

	results, err := s.Catalog.Search(r.Context(), q, r.URL.Query().Get("style"), queryInt(r, "limit", 10, 10))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": results,
		"meta": map[string]any{"count": len(results), "limit": 10, "note": "results are capped; the catalog cannot be enumerated"},
	})
	return nil
}

func (s *Server) barcode(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	res, err := s.Catalog.ResolveBarcode(r.Context(), r.PathValue("gtin"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": res})
	return nil
}

func (s *Server) createDrink(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Name         string  `json:"name"`
		Brand        string  `json:"brand"`
		StyleSlug    string  `json:"style_slug"`
		Container    string  `json:"container"`
		BeverageType string  `json:"beverage_type"`
		ABVPercent   float64 `json:"abv_percent"`
		VolumeML     int     `json:"volume_ml"`
		Sweetener    string  `json:"sweetener"`
		Carbonation  string  `json:"carbonation"`
		Acid         string  `json:"acid"`
		Barcode      string  `json:"barcode"`
		Description  string  `json:"description"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if !s.Limiter.Allow("newdrink:"+tok.User.ID, 20, time.Hour) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many new drinks; try again later")
		return nil
	}
	d, created, err := s.Catalog.CreateProvisional(r.Context(), tok.User.ID, catalog.CreateInput{
		Name: in.Name, Brand: in.Brand, StyleSlug: in.StyleSlug, Container: in.Container,
		BeverageType: in.BeverageType, ABVPercent: in.ABVPercent,
		VolumeML: in.VolumeML, Sweetener: in.Sweetener, Carbonation: in.Carbonation,
		Acid: in.Acid, Barcode: in.Barcode, Description: in.Description,
	})
	if err != nil {
		return err
	}
	status := http.StatusOK
	msg := "existing equivalent drink returned"
	if created {
		status = http.StatusCreated
		msg = "provisional drink created"
	}
	writeJSON(w, status, map[string]any{"data": map[string]any{
		"id": d.ID, "slug": d.Slug, "name": d.Name, "status": d.Status,
		"beverage_type": d.BeverageType, "abv_percent": d.AbvPercent, "message": msg,
	}})
	return nil
}

// maxCheckinUploadBytes matches the other media uploads. The small extra
// allowance covers multipart framing around the 25 MiB file.
const maxCheckinUploadBytes = 25 << 20

// checkinRequest carries the fields shared by the JSON and multipart forms.
type checkinRequest struct {
	DrinkID        int
	VenueID        *int
	Rating         *float64
	Serving        string
	PrepMethod     string
	PrepNote       string
	Note           string
	Visibility     string
	IdempotencyKey string
	Lat            *float64
	Lng            *float64
	FriendIDs      []string
	AtHome         bool
}

// createCheckin accepts JSON, or multipart/form-data with an optional `image`
// (alias `photo`) so the Android app can upload a photo in one request.
func (s *Server) createCheckin(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType == "multipart/form-data" {
		return s.createCheckinMultipart(w, r, tok)
	}

	var in struct {
		DrinkID        int      `json:"drink_id"`
		VenueID        *int     `json:"venue_id"`
		Rating         *float64 `json:"rating"`
		Serving        string   `json:"serving"`
		PrepMethod     string   `json:"prep_method"`
		PrepNote       string   `json:"prep_note"`
		Note           string   `json:"note"`
		Visibility     string   `json:"visibility"`
		IdempotencyKey string   `json:"idempotency_key"`
		Lat            *float64 `json:"lat"`
		Lng            *float64 `json:"lng"`
		FriendIDs      []string `json:"friend_ids"`
		AtHome         bool     `json:"at_home"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	return s.saveCheckin(w, r, tok, checkinRequest{
		DrinkID: in.DrinkID, VenueID: in.VenueID, Rating: in.Rating, Serving: in.Serving,
		PrepMethod: in.PrepMethod, PrepNote: in.PrepNote,
		Note: in.Note, Visibility: in.Visibility, IdempotencyKey: in.IdempotencyKey,
		Lat: in.Lat, Lng: in.Lng, FriendIDs: in.FriendIDs, AtHome: in.AtHome,
	}, nil)
}

// createCheckinMultipart parses the photo form. The body is capped before
// parsing so an oversized upload never reaches the media service.
func (s *Server) createCheckinMultipart(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxCheckinUploadBytes+4096)
	if err := r.ParseMultipartForm(maxCheckinUploadBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "image exceeds the 25 MiB request limit")
			return nil
		}
		return bad("invalid multipart upload")
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	in := checkinRequest{
		Serving:        strings.TrimSpace(r.FormValue("serving")),
		PrepMethod:     strings.TrimSpace(r.FormValue("prep_method")),
		PrepNote:       r.FormValue("prep_note"),
		Note:           r.FormValue("note"),
		Visibility:     strings.TrimSpace(r.FormValue("visibility")),
		IdempotencyKey: strings.TrimSpace(r.FormValue("idempotency_key")),
		AtHome:         parseBoolish(r.FormValue("at_home")),
	}
	drinkID, err := strconv.Atoi(strings.TrimSpace(r.FormValue("drink_id")))
	if err != nil || drinkID <= 0 {
		return bad("drink_id is required")
	}
	in.DrinkID = drinkID
	if raw := strings.TrimSpace(r.FormValue("venue_id")); raw != "" {
		venueID, err := strconv.Atoi(raw)
		if err != nil || venueID <= 0 {
			return bad("invalid venue_id")
		}
		in.VenueID = &venueID
	}
	if raw := strings.TrimSpace(r.FormValue("rating")); raw != "" {
		rating, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return bad("invalid rating")
		}
		in.Rating = &rating
	}
	if raw := strings.TrimSpace(r.FormValue("lat")); raw != "" {
		lat, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return bad("invalid lat")
		}
		in.Lat = &lat
	}
	if raw := strings.TrimSpace(r.FormValue("lng")); raw != "" {
		lng, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return bad("invalid lng")
		}
		in.Lng = &lng
	}
	if r.MultipartForm != nil {
		in.FriendIDs = append(in.FriendIDs, r.MultipartForm.Value["friend_ids"]...)
	}

	file, hdr, err := r.FormFile("image")
	if err != nil {
		if !errors.Is(err, http.ErrMissingFile) {
			return bad("invalid image upload")
		}
		file, hdr, err = r.FormFile("photo")
		if err != nil {
			if !errors.Is(err, http.ErrMissingFile) {
				return bad("invalid image upload")
			}
			file, hdr = nil, nil
		}
	}
	var stored *ent.Media
	if file != nil {
		defer file.Close()
		stored, err = s.Media.Store(r.Context(), tok.User.ID, "checkin", file, hdr.Filename, hdr.Header.Get("Content-Type"))
		if err != nil {
			if writeCheckinMediaError(w, err) {
				return nil
			}
			return err
		}
	}
	return s.saveCheckin(w, r, tok, in, stored)
}

// saveCheckin validates the shared fields and writes the check-in. A stored
// upload that the write cannot use is discarded so no orphan remains.
func (s *Server) saveCheckin(w http.ResponseWriter, r *http.Request, tok *auth.APIToken, in checkinRequest, stored *ent.Media) error {
	var rating *int
	if in.Rating != nil {
		scaled := int(math.Round(*in.Rating * 4))
		if *in.Rating < 0 || *in.Rating > 5 || math.Abs(*in.Rating*4-float64(scaled)) > 1e-9 {
			s.discardCheckinMedia(r.Context(), stored)
			return checkins.ErrInvalidRating
		}
		rating = &scaled
	}
	photoMediaID := ""
	if stored != nil {
		photoMediaID = stored.ID
	}
	c, created, err := s.Checkins.Create(r.Context(), tok.User, checkins.CreateInput{
		DrinkID: in.DrinkID, VenueID: in.VenueID, Rating: rating, Serving: in.Serving,
		PrepMethod: in.PrepMethod, PrepNote: in.PrepNote,
		Note: in.Note, Visibility: in.Visibility, IdempotencyKey: in.IdempotencyKey,
		PhotoMediaID: photoMediaID, Lat: in.Lat, Lng: in.Lng, FriendIDs: in.FriendIDs,
		AtHome: in.AtHome,
	})
	if err != nil {
		s.discardCheckinMedia(r.Context(), stored)
		return err
	}
	if !created && stored != nil {
		// Idempotent replay: the original row keeps its own photo, so the fresh
		// upload is unreachable.
		s.discardCheckinMedia(r.Context(), stored)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		w.Header().Set("Location", "/api/v1/checkins/"+c.ID)
		if s.Badges != nil {
			if earned, err := s.Badges.Evaluate(r.Context(), tok.User.ID); err == nil {
				for _, badge := range earned {
					s.Integrations.Dispatch(r.Context(), integrations.Event{UserID: tok.User.ID, Kind: "badge", Text: "Earned the " + badge + " badge."})
				}
			}
		}
		if s.Integrations != nil {
			s.Integrations.Dispatch(r.Context(), integrations.Event{UserID: tok.User.ID, Kind: "checkin", Text: "Logged a soda.", URL: s.Integrations.BaseURL() + "/you"})
		}
	}
	tags := []map[string]any{}
	if detail, err := s.Checkins.Get(r.Context(), tok.User.ID, c.ID); err == nil {
		tags = taggedFriendDTOs(detail.TaggedFriends)
	}
	data := map[string]any{
		"id": c.ID, "drink_id": c.DrinkID, "rating": c.Rating, "visibility": c.Visibility,
		"serving": c.Serving, "prep_method": c.PrepMethod, "prep_note": c.PrepNote,
		"created_at": c.CreatedAt, "created": created,
		"venue_id": c.VenueID, "at_home": in.AtHome, "tagged_friends": tags,
	}
	if c.PhotoMediaID != nil && *c.PhotoMediaID != "" {
		data["photo_media_id"] = *c.PhotoMediaID
	}
	writeJSON(w, status, map[string]any{"data": data})
	return nil
}

// discardCheckinMedia best-effort removes a stored upload and its files when
// the check-in write cannot use it. Cleanup failures are ignored: the original
// error is the one worth reporting.
func (s *Server) discardCheckinMedia(ctx context.Context, row *ent.Media) {
	if row == nil || s.DB == nil || s.DB.Client == nil {
		return
	}
	cleanupCtx := context.WithoutCancel(ctx)
	_ = s.DB.Client.Media.DeleteOneID(row.ID).Exec(cleanupCtx)
	if s.Media == nil || s.Media.Dir == "" {
		return
	}
	dir := filepath.Clean(s.Media.Dir)
	for _, rel := range []string{row.Path, row.ThumbPath, row.PosterPath} {
		if strings.TrimSpace(rel) == "" {
			continue
		}
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if !strings.HasPrefix(full, dir+string(os.PathSeparator)) {
			continue
		}
		_ = os.Remove(full)
	}
}

// writeCheckinMediaError maps media errors to the API's existing shapes. It
// reports whether it wrote a response.
func writeCheckinMediaError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, media.ErrUnsupported), errors.Is(err, media.ErrUnsupportedImage):
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "that file must be a JPEG, PNG, WebP or GIF image")
	case errors.Is(err, media.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "image is too large")
	case errors.Is(err, media.ErrInvalidKind):
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid media kind")
	default:
		return false
	}
	return true
}

func (s *Server) deleteCheckin(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Checkins.Delete(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) toast(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Social.Toast(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"toasted": true}})
	return nil
}

func (s *Server) untoast(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Social.Untoast(r.Context(), tok.User.ID, r.PathValue("id")); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"toasted": false}})
	return nil
}

// searchVenueRow is one nearby or local result. Local rows carry a selectable
// id; external rows carry provider identifiers and no id until promoted.
type searchVenueRow struct {
	ID         *int    `json:"id,omitempty"`
	Slug       string  `json:"slug,omitempty"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind,omitempty"`
	Address    string  `json:"address,omitempty"`
	City       string  `json:"city,omitempty"`
	Region     string  `json:"region,omitempty"`
	Country    string  `json:"country,omitempty"`
	Lat        float64 `json:"lat,omitempty"`
	Lng        float64 `json:"lng,omitempty"`
	DistanceM  *int    `json:"distance_m,omitempty"`
	Source     string  `json:"source"`
	Provider   string  `json:"provider,omitempty"`
	ProviderID string  `json:"provider_id,omitempty"`
	AtHome     bool    `json:"at_home,omitempty"`
}

func (s *Server) searchVenues(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	q := r.URL.Query()
	lat, lng, err := optionalCoords(q.Get("lat"), q.Get("lng"))
	if err != nil {
		return err
	}
	radius := 0
	if raw := strings.TrimSpace(q.Get("radius")); raw != "" {
		radius, err = strconv.Atoi(raw)
		if err != nil || radius <= 0 {
			return bad("radius must be a positive number of metres")
		}
	}
	source := strings.TrimSpace(q.Get("source"))
	if source == "" {
		source = "local"
	}
	switch source {
	case "local", "external", "all":
	default:
		return bad("source must be local, external or all")
	}
	wantExternal := source == "external" || source == "all"
	if wantExternal && !parseBoolish(q.Get("consent")) {
		writeError(w, http.StatusBadRequest, "consent_required", "external place search requires consent=true")
		return nil
	}

	limit := queryInt(r, "limit", 10, 20)
	if wantExternal && limit > 15 {
		limit = 15
	}
	local, err := s.Venues.SearchNear(r.Context(), q.Get("q"), limit, lat, lng, radius)
	if err != nil {
		return err
	}
	rows := make([]searchVenueRow, 0, len(local))
	for _, v := range local {
		id := v.ID
		rows = append(rows, searchVenueRow{
			ID: &id, Slug: v.Slug, Name: v.Name, Kind: v.Kind, Address: v.Address,
			City: v.City, Region: v.Region, Country: v.Country, Lat: v.Lat, Lng: v.Lng,
			DistanceM: v.DistanceM, Source: "local", AtHome: v.AtHome,
		})
	}
	if !wantExternal {
		writeJSON(w, http.StatusOK, map[string]any{"data": rows})
		return nil
	}

	externalStatus := "ok"
	external := []searchVenueRow{}
	switch {
	case lat == nil || lng == nil:
		externalStatus = "unavailable"
	case s.Nearby == nil || !s.Nearby.Enabled:
		externalStatus = "unavailable"
	case !s.Limiter.Allow("nearby-geo:"+tok.User.ID, 6, time.Minute):
		externalStatus = "rate_limited"
	default:
		places, err := s.Nearby.Nearby(r.Context(), *lat, *lng)
		if err != nil {
			// A provider failure must never fail the request; local rows stand.
			externalStatus = "unavailable"
		} else {
			radiusM := radius
			if radiusM <= 0 {
				radiusM = 2000
			}
			external = appendExternalRows(places, rows, *lat, *lng, radiusM, limitForSource(source, limit))
		}
	}
	if source == "external" {
		// External-only responses omit the local rows but still used them for
		// dedupe above.
		rows = external
	} else {
		rows = append(rows, external...)
		if len(rows) > 15 {
			rows = rows[:15]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows, "external_status": externalStatus})
	return nil
}

func limitForSource(source string, limit int) int {
	if source == "external" {
		if limit > 15 {
			return 15
		}
		return limit
	}
	return 15
}

// appendExternalRows filters provider places against the local results and the
// requested radius, then returns at most limit rows.
func appendExternalRows(places []geocode.Place, local []searchVenueRow, lat, lng float64, radiusM, limit int) []searchVenueRow {
	out := []searchVenueRow{}
	for _, p := range places {
		if len(out) >= limit {
			break
		}
		d := geoDistanceMeters(lat, lng, p.Lat, p.Lng)
		if d > float64(radiusM) {
			continue
		}
		if duplicateOfLocal(p, local, lat, lng) {
			continue
		}
		rounded := int(math.Round(d))
		out = append(out, searchVenueRow{
			Name: p.Name, Kind: p.Kind, City: p.City, Country: p.Country,
			Lat: p.Lat, Lng: p.Lng, DistanceM: &rounded,
			Source: "external", Provider: "photon", ProviderID: p.ProviderID,
		})
	}
	return out
}

// duplicateOfLocal reports whether a provider place is the same as a local
// venue: within 150 m or an identical case-insensitive name.
func duplicateOfLocal(p geocode.Place, local []searchVenueRow, lat, lng float64) bool {
	for _, v := range local {
		if strings.EqualFold(strings.TrimSpace(v.Name), strings.TrimSpace(p.Name)) {
			return true
		}
		if v.Lat != 0 || v.Lng != 0 {
			if geoDistanceMeters(p.Lat, p.Lng, v.Lat, v.Lng) <= 150 {
				return true
			}
		}
	}
	_ = lat
	_ = lng
	return false
}

func geoDistanceMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthRadiusM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// parseBoolish accepts the boolean spellings a HTML form or the Android app
// may send. Anything unrecognised is false.
func parseBoolish(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// optionalCoords parses an optional lat/lng pair. Both must be present or both
// absent; a single value or an out-of-range value is a bad request.
func optionalCoords(latRaw, lngRaw string) (*float64, *float64, error) {
	latRaw = strings.TrimSpace(latRaw)
	lngRaw = strings.TrimSpace(lngRaw)
	if latRaw == "" && lngRaw == "" {
		return nil, nil, nil
	}
	if latRaw == "" || lngRaw == "" {
		return nil, nil, bad("lat and lng must be provided together")
	}
	lat, err := strconv.ParseFloat(latRaw, 64)
	if err != nil || math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 {
		return nil, nil, bad("lat must be -90..90")
	}
	lng, err := strconv.ParseFloat(lngRaw, 64)
	if err != nil || math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180 || lng > 180 {
		return nil, nil, bad("lng must be -180..180")
	}
	return &lat, &lng, nil
}

func (s *Server) getVenue(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return bad("invalid venue id")
	}
	v, drinks, err := s.Venues.Get(r.Context(), id)
	if err != nil {
		return err
	}
	data := map[string]any{"venue": v, "drinks": drinks}
	// The shelf is a convenience block on the detail page: a failure there must
	// not fail a venue read, so it degrades to an absent field.
	if shelf, shelfErr := s.Venues.Shelf(r.Context(), id, tok.User.ID, time.Now()); shelfErr != nil {
		s.Log.WarnContext(r.Context(), "api: venue shelf", "err", shelfErr, "venue_id", id)
	} else {
		data["shelf"] = shelf
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
	return nil
}

func (s *Server) createVenue(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Name    string  `json:"name"`
		Kind    string  `json:"kind"`
		Address string  `json:"address"`
		City    string  `json:"city"`
		Region  string  `json:"region"`
		Country string  `json:"country"`
		Lat     float64 `json:"lat"`
		Lng     float64 `json:"lng"`
		Website string  `json:"website"`
		// ExternalProvider/ExternalID promote a nearby Photon result into a
		// real venue. Both must be present together.
		ExternalProvider string `json:"external_provider"`
		ExternalID       string `json:"external_id"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if !s.Limiter.Allow("newvenue:"+tok.User.ID, 10, time.Hour) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many venue submissions")
		return nil
	}
	if in.ExternalProvider != "" || in.ExternalID != "" {
		id, err := s.Venues.CreateFromExternal(r.Context(), in.ExternalProvider, in.ExternalID,
			in.Name, in.Kind, in.City, in.Country, in.Lat, in.Lng)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": id, "name": in.Name, "created": false, "promoted": true}})
		return nil
	}
	v, created, err := s.Venues.Create(r.Context(), tok.User.ID, venues.CreateInput{
		Name: in.Name, Kind: in.Kind, Address: in.Address, City: in.City,
		Region: in.Region, Country: in.Country, Lat: in.Lat, Lng: in.Lng, Website: in.Website,
	})
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"data": map[string]any{"id": v.ID, "slug": v.Slug, "name": v.Name, "created": created}})
	return nil
}

func (s *Server) saveVenue(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return bad("invalid venue id")
	}
	if err := s.Venues.Save(r.Context(), tok.User.ID, id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"saved": true}})
	return nil
}

func (s *Server) unsaveVenue(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return bad("invalid venue id")
	}
	if err := s.Venues.Unsave(r.Context(), tok.User.ID, id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"saved": false}})
	return nil
}

// mobileToken exchanges a browser session (cookie + CSRF) for a personal token.
func (s *Server) mobileToken(w http.ResponseWriter, r *http.Request) {
	sess, u, err := s.Auth.CurrentSession(r.Context(), r)
	if err != nil {
		unauthorized(w, "browser session required")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}
	if !auth.ValidCSRF(r, sess) {
		writeError(w, http.StatusForbidden, "csrf_failed", "missing or invalid CSRF token")
		return
	}
	if u.EmailVerifiedAt == nil {
		writeError(w, http.StatusForbidden, "email_not_verified", "verify your email address first")
		return
	}
	token, _, err := s.Auth.CreateAPIToken(r.Context(), u.ID, "Android app", []string{"read", "write", "feed"})
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"token": token, "name": "Android app"}})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return bad("invalid JSON body: " + err.Error())
	}
	return nil
}

func drinkDTO(d *catalog.DrinkDetail) map[string]any {
	dr := d.Drink
	return map[string]any{
		"id": dr.ID, "slug": dr.Slug, "name": dr.Name, "description": dr.Description,
		"brand": d.BrandName, "style": d.StyleName, "container": dr.Container, "volume_ml": dr.VolumeMl,
		"beverage_type": dr.BeverageType, "abv_percent": dr.AbvPercent,
		"caffeine_mg_per_100ml": dr.CaffeineMgPer100ml, "sugar_g_per_100ml": dr.SugarGPer100ml,
		"calories_kcal_per_100ml": dr.CaloriesKcalPer100ml, "sweetener": dr.Sweetener,
		"carbonation": dr.Carbonation, "acid": dr.Acid, "status": dr.Status,
		"rating": d.Rating, "flavours": d.Flavours, "aliases": d.Aliases, "barcode_count": d.BarcodeNum,
		"avg_rating": d.Rating.Avg, "rater_count": d.Rating.RaterCount, "rating_shown": d.Rating.Shown,
	}
}

func venuesInputAlias() {}
