package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"sodapop/ent"
	"sodapop/internal/venues"
)

const maxVenueMediaBytes = 25 << 20

// venueMediaUpload accepts a photo or menu image for a venue. The route is
// wrapped in requireVerified.
func (s *Server) venueMediaUpload(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	if u == nil {
		http.Error(w, "sign in to upload", http.StatusUnauthorized)
		return
	}
	if s.Limiter != nil && !s.Limiter.Allow("venuemedia:"+u.ID, 10, time.Hour) {
		w.Header().Set("Retry-After", "3600")
		http.Error(w, "Too many venue uploads. Try again later.", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxVenueMediaBytes+4096)
	if err := r.ParseMultipartForm(maxVenueMediaBytes); err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	if !s.csrfOK(w, r) {
		return
	}
	venueID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || venueID <= 0 {
		http.NotFound(w, r)
		return
	}
	file, hdr, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "choose an image to upload", http.StatusBadRequest)
		return
	}
	defer file.Close()

	_, err = s.Venues.AddVenueMedia(r.Context(), s.Media, u.ID, venueID,
		r.FormValue("kind"), r.FormValue("label"), file, hdr.Filename, hdr.Header.Get("Content-Type"))
	if err != nil {
		switch {
		case errors.Is(err, venues.ErrInvalidMedia):
			http.Error(w, "That file must be a JPEG, PNG, WebP or GIF image.", http.StatusUnsupportedMediaType)
		case errors.Is(err, venues.ErrInvalidKind):
			http.Error(w, "Choose photo or menu.", http.StatusBadRequest)
		case errors.Is(err, venues.ErrInvalidLabel):
			http.Error(w, "Keep the caption to 80 characters or fewer.", http.StatusBadRequest)
		case errors.Is(err, venues.ErrTooLarge):
			http.Error(w, "That image is too large. Photos must be 12 MB or smaller.", http.StatusRequestEntityTooLarge)
		case errors.Is(err, venues.ErrMediaLimit):
			http.Error(w, "This venue has reached its photo and menu limit. Remove one before uploading another.", http.StatusConflict)
		case errors.Is(err, venues.ErrNotFound):
			http.NotFound(w, r)
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/venues/"+strconv.Itoa(venueID)+"#photos", http.StatusFound)
}

// venueMediaDelete soft-deletes a venue photo or menu. The uploader may remove
// their own row; admins and moderators may remove any.
func (s *Server) venueMediaDelete(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Error(w, "sign in to remove media", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	row, err := s.Auth.Client.VenueMedia.Get(r.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	admin := u.Role == "admin" || u.Role == "moderator"
	if err := s.Venues.RemoveVenueMedia(r.Context(), u.ID, admin, id); err != nil {
		if errors.Is(err, venues.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/venues/"+strconv.Itoa(row.VenueID), http.StatusFound)
}
