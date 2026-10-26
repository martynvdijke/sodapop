package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"sodapop/internal/auth"
)

// maxVenueMediaBytes matches the web upload ceiling. The small extra allowance
// covers multipart framing around the 25 MiB file.
const maxVenueMediaBytes = 25 << 20

// listVenueMedia returns a venue's active photos and menus. There is no total or
// pagination metadata: a venue's media is a bounded, public list.
func (s *Server) listVenueMedia(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return bad("invalid venue id")
	}
	items, err := s.Venues.VenueMedia(r.Context(), id)
	if err != nil {
		return err
	}
	admin := isAdmin(tok)
	out := make([]map[string]any, 0, len(items))
	for _, v := range items {
		out = append(out, venueMediaDTO(v.ID, v.MediaID, v.Kind, v.Label, v.UploadedBy,
			v.UploaderHandle, v.CreatedAt, admin || v.UploadedBy == tok.User.ID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
	return nil
}

// uploadVenueMedia accepts the multipart `image` field plus `kind` and `label`.
// The media service validates the bytes and the venues service enforces caps.
func (s *Server) uploadVenueMedia(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return bad("invalid venue id")
	}
	if s.Limiter != nil && !s.Limiter.Allow("venuemedia:"+tok.User.ID, 10, time.Hour) {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many venue uploads; try again later")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxVenueMediaBytes+4096)
	if err := r.ParseMultipartForm(maxVenueMediaBytes); err != nil {
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

	row, err := s.Venues.AddVenueMedia(r.Context(), s.Media, tok.User.ID, id,
		r.FormValue("kind"), r.FormValue("label"), file, hdr.Filename, hdr.Header.Get("Content-Type"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": venueMediaDTO(
		row.ID, row.MediaID, string(row.Kind), row.Label, tok.User.ID,
		tok.User.Handle, row.CreatedAt, true)})
	return nil
}

// deleteVenueMedia soft-deletes a venue photo or menu owned by the token holder.
// Admins may remove any row; everyone else gets 404.
func (s *Server) deleteVenueMedia(w http.ResponseWriter, r *http.Request, tok *auth.APIToken, id string) error {
	if err := s.Venues.RemoveVenueMedia(r.Context(), tok.User.ID, isAdmin(tok), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// venueDelete dispatches the two DELETE shapes that share
// /api/v1/venues/{first}/{second}. Go's ServeMux rejects those as separate
// patterns because neither is more specific, so the split lives here.
func (s *Server) venueDelete(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	first := r.PathValue("venueID")
	second := r.PathValue("target")
	if first == "media" {
		return s.deleteVenueMedia(w, r, tok, second)
	}
	if second == "save" {
		id, err := strconv.Atoi(first)
		if err != nil {
			return bad("invalid venue id")
		}
		if err := s.Venues.Unsave(r.Context(), tok.User.ID, id); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"saved": false}})
		return nil
	}
	writeError(w, http.StatusNotFound, "not_found", "not found")
	return nil
}

func venueMediaDTO(id, mediaID, kind, label, uploadedBy, uploaderHandle, createdAt string, canRemove bool) map[string]any {
	return map[string]any{
		"id":              id,
		"media_id":        mediaID,
		"kind":            kind,
		"label":           label,
		"uploaded_by":     uploadedBy,
		"uploader_handle": uploaderHandle,
		"created_at":      createdAt,
		"can_remove":      canRemove,
	}
}
