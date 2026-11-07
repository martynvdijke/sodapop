package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"sodapop/internal/auth"
	"sodapop/internal/catalog"
)

// maxAvatarBytes is the upload cap for profile avatars. It is deliberately
// smaller than the general photo cap: an avatar is only ever shown small.
const maxAvatarBytes = 5 << 20

// avatarURL maps a stored avatar media id to its authenticated media route.
// A nil or empty id becomes a JSON null, which clients treat as "no avatar".
func avatarURL(id *string) *string {
	if id == nil || *id == "" {
		return nil
	}
	url := "/api/v1/media/" + *id
	return &url
}

// publicAvatarURL is the anonymous analogue used by the public preview. The
// public media route only resolves avatars of active, non-deleted users.
func publicAvatarURL(id *string) *string {
	if id == nil || *id == "" {
		return nil
	}
	url := "/api/v1/public/media/" + *id
	return &url
}

// favouriteDrinkCard loads the pinned drink as a compact profile card. A
// missing, merged or otherwise hidden row is reported as an absent pin rather
// than an error, so a stale foreign key never breaks a profile read.
func (s *Server) favouriteDrinkCard(ctx context.Context, id *int) (map[string]any, error) {
	if id == nil || *id <= 0 {
		return nil, nil
	}
	d, err := s.Catalog.GetByID(ctx, *id)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	card := map[string]any{
		"id":             d.Drink.ID,
		"name":           d.Drink.Name,
		"slug":           d.Drink.Slug,
		"brand":          d.BrandName,
		"style":          d.StyleName,
		"image_url":      nil,
		"average_rating": nil,
	}
	if d.Drink.ImageURL != nil && strings.TrimSpace(*d.Drink.ImageURL) != "" {
		card["image_url"] = *d.Drink.ImageURL
	}
	if d.Rating.Avg != nil {
		card["average_rating"] = *d.Rating.Avg
	}
	return card, nil
}

// meAvatarPut handles POST /api/v1/me/avatar. The upload is stored with the
// avatar kind and then attached to the caller in one request.
func (s *Server) meAvatarPut(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+4096)
	if err := r.ParseMultipartForm(maxAvatarBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "avatar exceeds the 5 MiB limit")
			return nil
		}
		return bad("invalid multipart upload")
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, hdr, err := r.FormFile("avatar")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			return bad("avatar file is required")
		}
		return bad("invalid avatar upload")
	}
	defer file.Close()

	stored, err := s.Media.Store(r.Context(), tok.User.ID, "avatar", file, hdr.Filename, hdr.Header.Get("Content-Type"))
	if err != nil {
		if writeCheckinMediaError(w, err) {
			return nil
		}
		return err
	}
	if err := s.Auth.SetAvatar(r.Context(), tok.User.ID, stored.ID); err != nil {
		// The upload belongs to the caller, so SetAvatar normally cannot fail.
		// If it does, do not leave an unreferenced avatar behind.
		s.discardCheckinMedia(r.Context(), stored)
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"avatar_url": "/api/v1/media/" + stored.ID})
	return nil
}

// meAvatarDelete handles DELETE /api/v1/me/avatar.
func (s *Server) meAvatarDelete(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Auth.ClearAvatar(r.Context(), tok.User.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// meFavouriteDrinkPut handles PUT /api/v1/me/favourite-drink.
func (s *Server) meFavouriteDrinkPut(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		DrinkID *int `json:"drink_id"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.DrinkID == nil {
		return bad("drink_id is required")
	}
	if *in.DrinkID <= 0 {
		return bad("drink_id must be a positive integer")
	}
	if err := s.Auth.SetFavouriteDrink(r.Context(), tok.User.ID, *in.DrinkID); err != nil {
		if errors.Is(err, auth.ErrDrinkNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "drink not found")
			return nil
		}
		return err
	}
	card, err := s.favouriteDrinkCard(r.Context(), in.DrinkID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"favourite_drink": card})
	return nil
}

// meFavouriteDrinkDelete handles DELETE /api/v1/me/favourite-drink.
func (s *Server) meFavouriteDrinkDelete(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if err := s.Auth.ClearFavouriteDrink(r.Context(), tok.User.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// parseIntQuery parses an optional integer query value. It returns nil when
// the parameter is absent and a badRequestError when it is not a number.
func parseIntQuery(raw, name string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, bad(name + " must be an integer")
	}
	return &n, nil
}
