package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"sodapop/internal/auth"
	"sodapop/internal/catalog"
	"sodapop/internal/media"
)

// maxDrinkImageBytes matches the web gallery upload ceiling. The extra
// allowance covers multipart framing around the 25 MiB file.
const maxDrinkImageBytes = 25 << 20

// RegisterGalleryRoutes wires the drink gallery API. Call it from
// Server.Register; it returns no collection endpoints beyond one drink's
// bounded gallery.
func (s *Server) RegisterGalleryRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /api/v1/drinks/{slug}/images", s.protect("read", false, s.listDrinkImages))
	s.route(mux, "POST /api/v1/drinks/{slug}/images", s.protect("write", true, s.uploadDrinkImage))
	s.route(mux, "POST /api/v1/drinks/images/{id}/primary", s.protect("write", true, s.setDrinkImagePrimary))
	s.route(mux, "DELETE /api/v1/drinks/images/{id}", s.protect("write", true, s.deleteDrinkImage))
}

// listDrinkImages returns one drink's gallery, newest first. There is no total
// or pagination metadata.
func (s *Server) listDrinkImages(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	d, err := s.Catalog.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		return err
	}
	images, err := s.Catalog.Gallery(r.Context(), d.Drink.ID)
	if err != nil {
		return err
	}
	admin := isAdmin(tok)
	out := make([]map[string]any, 0, len(images))
	for _, img := range images {
		out = append(out, drinkImageDTO(img, admin))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}

// uploadDrinkImage accepts the multipart `image` field plus optional
// `attribution`. New images are not auto-promoted.
func (s *Server) uploadDrinkImage(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	d, err := s.Catalog.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		return err
	}
	if s.Limiter != nil && !s.Limiter.Allow("drinkimage:"+tok.User.ID, 10, time.Hour) {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many gallery uploads; try again later")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDrinkImageBytes+4096)
	if err := r.ParseMultipartForm(maxDrinkImageBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "image exceeds the 25 MiB request limit")
			return nil
		}
		return bad("invalid multipart upload")
	}
	file, hdr, err := r.FormFile("image")
	if err != nil {
		return bad("image is required")
	}
	defer file.Close()

	row, err := s.Catalog.AddImage(r.Context(), s.Media, tok.User.ID, d.Drink.ID, file,
		hdr.Filename, hdr.Header.Get("Content-Type"), r.FormValue("attribution"))
	if err != nil {
		if writeGalleryError(w, err) {
			return nil
		}
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": drinkImageDTO(catalog.DrinkImage{
		ID: row.ID, MediaID: mediaIDFromImageURL(row.URL), URL: row.URL,
		IsPrimary: row.IsPrimary, Attribution: row.Attribution, CreatedAt: row.CreatedAt,
	}, isAdmin(tok))})
	return nil
}

// setDrinkImagePrimary promotes one image. Admin-only; non-admins get 403.
func (s *Server) setDrinkImagePrimary(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		return bad("invalid image id")
	}
	if err := s.Catalog.SetPrimary(r.Context(), isAdmin(tok), id); err != nil {
		if writeGalleryError(w, err) {
			return nil
		}
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": id, "is_primary": true}})
	return nil
}

// deleteDrinkImage permanently removes one image and its media row. Admin-only.
func (s *Server) deleteDrinkImage(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		return bad("invalid image id")
	}
	if err := s.Catalog.DeleteImage(r.Context(), isAdmin(tok), id); err != nil {
		if writeGalleryError(w, err) {
			return nil
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// writeGalleryError maps gallery-specific errors that the shared mapError does
// not know about. It reports whether it wrote a response.
func writeGalleryError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, catalog.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "admin only")
	case errors.Is(err, catalog.ErrInvalidImage):
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "that file must be a JPEG, PNG, WebP or GIF image")
	case errors.Is(err, media.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "image is too large")
	default:
		return false
	}
	return true
}

func drinkImageDTO(img catalog.DrinkImage, admin bool) map[string]any {
	return map[string]any{
		"id":           img.ID,
		"media_id":     img.MediaID,
		"url":          img.URL,
		"is_primary":   img.IsPrimary,
		"attribution":  img.Attribution,
		"created_at":   img.CreatedAt,
		"can_moderate": admin,
	}
}

func mediaIDFromImageURL(url string) string {
	const prefix = "/media/"
	if len(url) <= len(prefix) || url[:len(prefix)] != prefix {
		return ""
	}
	return url[len(prefix):]
}
