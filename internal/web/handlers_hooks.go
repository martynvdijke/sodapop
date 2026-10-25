package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/internal/checkins"
	"sodapop/internal/integrations"
)

// haTokenCreate mints a Home Assistant inbound token for the signed-in user.
// The plaintext is only returned by the service and never rendered here; the
// integrations page lists metadata, not the secret.
func (s *Server) haTokenCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	raw, _, err := s.Integrations.CreateInboundToken(r.Context(), u.ID, r.FormValue("label"))
	if err != nil {
		http.Error(w, "could not create token", http.StatusBadRequest)
		return
	}
	// The plaintext is shown once on the next page load and never stored.
	http.SetCookie(w, &http.Cookie{
		Name:     "sd_ha_token",
		Value:    raw,
		Path:     "/integrations",
		MaxAge:   120,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.Cfg.Env == "production",
	})
	http.Redirect(w, r, "/integrations#home-assistant", http.StatusFound)
}

// haTokenRevoke retires an inbound token owned by the signed-in user.
func (s *Server) haTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if err := s.Integrations.RevokeInboundToken(r.Context(), u.ID, r.PathValue("id")); err != nil {
		http.Error(w, "token not found", http.StatusNotFound)
		return
	}
	http.Redirect(w, r, "/integrations#home-assistant", http.StatusFound)
}

type haInboundBody struct {
	GTIN           string `json:"gtin"`
	Slug           string `json:"slug"`
	Rating         *int   `json:"rating"`
	Note           string `json:"note"`
	VenueID        *int   `json:"venue_id"`
	Serving        string `json:"serving"`
	IdempotencyKey string `json:"idempotency_key"`
}

// haInbound accepts a machine check-in from a Home Assistant automation. It is
// unauthenticated at the router level: the capability token in the path is the
// credential, so errors stay generic and the token is never logged.
func (s *Server) haInbound(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	if token == "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if s.Limiter != nil && !s.Limiter.Allow("hook:"+token, 10, time.Minute) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
		return
	}
	tok, err := s.Integrations.AuthenticateInbound(r.Context(), token)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body haInboundBody
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	gtin := strings.TrimSpace(body.GTIN)
	slug := strings.TrimSpace(body.Slug)
	if (gtin == "") == (slug == "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if body.Rating != nil && (*body.Rating < 0 || *body.Rating > 20) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_rating"})
		return
	}

	ctx := r.Context()
	var drinkRow *ent.Drink
	if gtin != "" {
		res, err := s.Catalog.ResolveBarcode(ctx, gtin)
		if err != nil || res == nil || res.Drink == nil || res.Drink.Drink == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "drink_not_found"})
			return
		}
		drinkRow = res.Drink.Drink
	} else {
		detail, err := s.Catalog.GetBySlug(ctx, slug)
		if err != nil || detail == nil || detail.Drink == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "drink_not_found"})
			return
		}
		drinkRow = detail.Drink
	}

	owner, err := s.Auth.Client.User.Get(ctx, tok.UserID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(body.IdempotencyKey)
	}

	c, created, err := s.Checkins.Create(ctx, owner, checkins.CreateInput{
		DrinkID:        drinkRow.ID,
		VenueID:        body.VenueID,
		Rating:         body.Rating,
		Serving:        body.Serving,
		Note:           strings.TrimSpace(body.Note),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, checkins.ErrInvalidRating), errors.Is(err, checkins.ErrInvalidInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		case errors.Is(err, checkins.ErrNotFound), ent.IsNotFound(err):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "drink_not_found"})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		}
		return
	}

	if created {
		if earned, err := s.Badges.Evaluate(ctx, owner.ID); err != nil {
			s.Log.Warn("badge evaluation failed", "err", err)
		} else {
			for _, badgeSlug := range earned {
				s.Integrations.Dispatch(ctx, integrations.Event{
					UserID: owner.ID, Kind: "badge", Text: "Earned the " + badgeSlug + " badge.", URL: s.Cfg.BaseURL + "/you",
				})
			}
		}
		s.Integrations.Dispatch(ctx, integrations.Event{
			UserID: owner.ID, Kind: "checkin", Text: "Logged a new soda.", URL: s.Cfg.BaseURL + "/checkins/" + c.ID,
		})
	}

	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"data": map[string]any{
		"checkin_id": c.ID,
		"drink":      map[string]string{"slug": drinkRow.Slug, "name": drinkRow.Name},
	}})
}
