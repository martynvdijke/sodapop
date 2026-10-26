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
	"sodapop/internal/catalog"
	"sodapop/internal/media"
)

const maxDrinkImageBytes = 25 << 20

// RegisterGalleryRoutes wires the community drink gallery endpoints. Call it
// from Server.Register; keeping the routes here avoids touching server.go when
// the lane is integrated.
func (s *Server) RegisterGalleryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /drinks/{slug}/images", s.requireVerified(s.drinkImageUpload))
	mux.HandleFunc("POST /drinks/images/{id}/primary", s.requireVerified(s.drinkImageSetPrimary))
	mux.HandleFunc("POST /drinks/images/{id}/delete", s.requireVerified(s.drinkImageDelete))
}

// GalleryPageData is the template contract for the drink page gallery. During
// integration, merge it into the render map and expose these names to the
// drink template:
//
//	g := s.GalleryPageData(r.Context(), d.Drink.ID, u)
//	data["Images"] = g.Images
//	data["CanUpload"] = g.CanUpload
//	data["CanModerate"] = g.CanModerate
//	data["GalleryError"] = g.GalleryError
type GalleryPageData struct {
	Images       []catalog.DrinkImage
	CanUpload    bool
	CanModerate  bool
	GalleryError string
}

// GalleryPageData loads a drink's gallery for the HTML page. A failed read is
// reported in GalleryError instead of failing the whole drink page.
func (s *Server) GalleryPageData(ctx context.Context, drinkID int, u *ent.User) GalleryPageData {
	out := GalleryPageData{
		Images:      []catalog.DrinkImage{},
		CanUpload:   u != nil && u.EmailVerifiedAt != nil,
		CanModerate: u != nil && catalog.CanModerateGallery(string(u.Role)),
	}
	images, err := s.Catalog.Gallery(ctx, drinkID)
	if err != nil {
		out.GalleryError = "The gallery could not be loaded."
		return out
	}
	out.Images = images
	return out
}

// drinkImageUpload accepts the multipart `image` field plus an optional
// `attribution` caption for one drink. The route is wrapped in requireVerified.
func (s *Server) drinkImageUpload(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	if u == nil {
		http.Error(w, "sign in to upload", http.StatusUnauthorized)
		return
	}
	if s.Limiter != nil && !s.Limiter.Allow("drinkimage:"+u.ID, 10, time.Hour) {
		w.Header().Set("Retry-After", "3600")
		http.Error(w, "Too many gallery uploads. Try again later.", http.StatusTooManyRequests)
		return
	}

	slug := r.PathValue("slug")
	if strings.TrimSpace(slug) == "" {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDrinkImageBytes+4096)
	if err := r.ParseMultipartForm(maxDrinkImageBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "That image is too large. Gallery images must be 25 MiB or smaller.", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	if !s.csrfOK(w, r) {
		return
	}

	d, err := s.Catalog.GetBySlug(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file, hdr, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "choose an image to upload", http.StatusBadRequest)
		return
	}
	defer file.Close()

	_, err = s.Catalog.AddImage(r.Context(), s.Media, u.ID, d.Drink.ID, file,
		hdr.Filename, hdr.Header.Get("Content-Type"), r.FormValue("attribution"))
	if err != nil {
		switch {
		case errors.Is(err, catalog.ErrInvalidImage):
			http.Error(w, "That file must be a JPEG, PNG, WebP or GIF image.", http.StatusUnsupportedMediaType)
		case errors.Is(err, media.ErrTooLarge):
			http.Error(w, "That image is too large. Gallery images must be 12 MB or smaller.", http.StatusRequestEntityTooLarge)
		case errors.Is(err, catalog.ErrNotFound):
			http.NotFound(w, r)
		case errors.Is(err, catalog.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/drinks/"+url.PathEscape(d.Drink.Slug)+"#gallery", http.StatusFound)
}

// drinkImageSetPrimary promotes one image. Admins and moderators may do this;
// the service enforces the same rule so a direct call cannot bypass the
// handler.
func (s *Server) drinkImageSetPrimary(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Error(w, "sign in to moderate", http.StatusUnauthorized)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	img, err := s.Auth.Client.DrinkImage.Get(r.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	slug, err := s.galleryDrinkSlug(r.Context(), img.DrinkID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.Catalog.SetPrimary(r.Context(), catalog.CanModerateGallery(string(u.Role)), id); err != nil {
		switch {
		case errors.Is(err, catalog.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		case errors.Is(err, catalog.ErrNotFound):
			http.NotFound(w, r)
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/drinks/"+url.PathEscape(slug)+"#gallery", http.StatusFound)
}

// drinkImageDelete permanently removes one gallery image. Admins and moderators
// may do this. The drink stays; only the image row and its media row are
// removed.
func (s *Server) drinkImageDelete(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Error(w, "sign in to moderate", http.StatusUnauthorized)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	img, err := s.Auth.Client.DrinkImage.Get(r.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	slug, err := s.galleryDrinkSlug(r.Context(), img.DrinkID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.Catalog.DeleteImage(r.Context(), catalog.CanModerateGallery(string(u.Role)), id); err != nil {
		switch {
		case errors.Is(err, catalog.ErrForbidden):
			http.Error(w, "forbidden", http.StatusForbidden)
		case errors.Is(err, catalog.ErrNotFound):
			http.NotFound(w, r)
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/drinks/"+url.PathEscape(slug)+"#gallery", http.StatusFound)
}

func (s *Server) galleryDrinkSlug(ctx context.Context, drinkID int) (string, error) {
	d, err := s.Catalog.GetByID(ctx, drinkID)
	if err != nil {
		return "", err
	}
	return d.Drink.Slug, nil
}
