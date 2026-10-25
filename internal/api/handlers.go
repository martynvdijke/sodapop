package api

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sodapop/internal/auth"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
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
	u := tok.User
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
	}})
	return nil
}

func (s *Server) myCheckins(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	items, err := s.Checkins.ListForUser(r.Context(), tok.User.ID, tok.User.ID, queryInt(r, "limit", 20, 50), r.URL.Query().Get("before"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "meta": map[string]any{"count": len(items)}})
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
	p, err := checkins.LookupProfile(r.Context(), s.Auth.Client, r.PathValue("handle"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"handle": p.Handle, "display_name": p.DisplayName, "bio": p.Bio, "created_at": p.CreatedAt,
	}})
	return nil
}

func (s *Server) userCheckins(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	p, err := checkins.LookupProfile(r.Context(), s.Auth.Client, r.PathValue("handle"))
	if err != nil {
		return err
	}
	if p.ID != tok.User.ID && !s.Social.AreFriends(r.Context(), tok.User.ID, p.ID) {
		// Return 404 rather than 403 so token holders cannot probe for accounts.
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	items, err := s.Checkins.ListForUser(r.Context(), tok.User.ID, p.ID, queryInt(r, "limit", 20, 50), r.URL.Query().Get("before"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
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
func (s *Server) search(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	q := r.URL.Query().Get("q")
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
		Name        string `json:"name"`
		Brand       string `json:"brand"`
		StyleSlug   string `json:"style_slug"`
		Container   string `json:"container"`
		VolumeML    int    `json:"volume_ml"`
		Sweetener   string `json:"sweetener"`
		Carbonation string `json:"carbonation"`
		Acid        string `json:"acid"`
		Barcode     string `json:"barcode"`
		Description string `json:"description"`
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
		"id": d.ID, "slug": d.Slug, "name": d.Name, "status": d.Status, "message": msg,
	}})
	return nil
}

func (s *Server) createCheckin(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		DrinkID        int      `json:"drink_id"`
		VenueID        *int     `json:"venue_id"`
		Rating         *float64 `json:"rating"`
		Serving        string   `json:"serving"`
		Note           string   `json:"note"`
		Visibility     string   `json:"visibility"`
		IdempotencyKey string   `json:"idempotency_key"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	var rating *int
	if in.Rating != nil {
		scaled := int(math.Round(*in.Rating * 4))
		if *in.Rating < 0 || *in.Rating > 5 || math.Abs(*in.Rating*4-float64(scaled)) > 1e-9 {
			return checkins.ErrInvalidRating
		}
		rating = &scaled
	}
	c, created, err := s.Checkins.Create(r.Context(), tok.User, checkins.CreateInput{
		DrinkID: in.DrinkID, VenueID: in.VenueID, Rating: rating, Serving: in.Serving,
		Note: in.Note, Visibility: in.Visibility, IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		w.Header().Set("Location", "/api/v1/checkins/"+c.ID)
	}
	writeJSON(w, status, map[string]any{"data": map[string]any{
		"id": c.ID, "drink_id": c.DrinkID, "rating": c.Rating, "visibility": c.Visibility,
		"created_at": c.CreatedAt, "created": created,
	}})
	return nil
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

func (s *Server) searchVenues(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	results, err := s.Venues.Search(r.Context(), r.URL.Query().Get("q"), queryInt(r, "limit", 10, 20))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": results})
	return nil
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
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"venue": v, "drinks": drinks}})
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
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if !s.Limiter.Allow("newvenue:"+tok.User.ID, 10, time.Hour) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many venue submissions")
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
		"caffeine_mg_per_100ml": dr.CaffeineMgPer100ml, "sugar_g_per_100ml": dr.SugarGPer100ml,
		"calories_kcal_per_100ml": dr.CaloriesKcalPer100ml, "sweetener": dr.Sweetener,
		"carbonation": dr.Carbonation, "acid": dr.Acid, "status": dr.Status,
		"rating": d.Rating, "flavours": d.Flavours, "aliases": d.Aliases, "barcode_count": d.BarcodeNum,
		"avg_rating": d.Rating.Avg, "rater_count": d.Rating.RaterCount, "rating_shown": d.Rating.Shown,
	}
}

func venuesInputAlias() {}
