package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"sodapop/internal/auth"
	"sodapop/internal/checkins"
	"sodapop/internal/comments"
)

// Tier 1 check-in editing surface. Routes are registered in server.go so route
// ownership stays in one place.

// optionalInt is the presence-aware decode of a nillable JSON field. present
// says the key was in the body; value is nil when the key was explicitly null,
// which is how a PATCH asks for "clear this".
type optionalInt struct {
	value   *int
	present bool
}

func decodeOptionalInt(raw json.RawMessage) (optionalInt, error) {
	if len(raw) == 0 {
		return optionalInt{}, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return optionalInt{present: true}, nil
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return optionalInt{present: true}, bad("invalid integer field")
	}
	return optionalInt{value: &v, present: true}, nil
}

// updateCheckin patches an owned check-in inside the 24-hour edit window. A
// foreign, deleted, or expired row is a 404 so the endpoint cannot be used to
// probe for other members' check-ins.
func (s *Server) updateCheckin(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Rating        json.RawMessage `json:"rating"`
		Serving       *string         `json:"serving"`
		PrepMethod    *string         `json:"prep_method"`
		PrepNote      *string         `json:"prep_note"`
		Note          *string         `json:"note"`
		VenueID       json.RawMessage `json:"venue_id"`
		Visibility    *string         `json:"visibility"`
		PriceCents    json.RawMessage `json:"price_cents"`
		PriceCurrency *string         `json:"price_currency"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}

	rating, err := decodeOptionalInt(in.Rating)
	if err != nil {
		return err
	}
	venue, err := decodeOptionalInt(in.VenueID)
	if err != nil {
		return err
	}
	price, err := decodeOptionalInt(in.PriceCents)
	if err != nil {
		return err
	}

	update := checkins.UpdateInput{
		Rating:        rating.value,
		ClearRating:   rating.present && rating.value == nil,
		VenueID:       venue.value,
		ClearVenue:    venue.present && venue.value == nil,
		PriceCents:    price.value,
		ClearPrice:    price.present && price.value == nil,
		Serving:       in.Serving,
		PrepMethod:    in.PrepMethod,
		PrepNote:      in.PrepNote,
		Note:          in.Note,
		Visibility:    in.Visibility,
		PriceCurrency: in.PriceCurrency,
	}

	id := r.PathValue("id")
	if _, err := s.Checkins.Update(r.Context(), tok.User.ID, id, update); err != nil {
		if errors.Is(err, checkins.ErrEditWindowClosed) {
			writeError(w, http.StatusBadRequest, "edit_window_closed", err.Error())
			return nil
		}
		return err
	}

	detail, err := s.Checkins.Get(r.Context(), tok.User.ID, id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": checkinDTO(detail, tok)})
	return nil
}

// updateComment rewrites a comment the token holder authored. Non-authors get a
// 404, matching the delete path, so the endpoint cannot be used to discover
// someone else's comment ids.
func (s *Server) updateComment(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}

	id := r.PathValue("id")
	if err := s.Comments.Edit(r.Context(), tok.User.ID, id, in.Body); err != nil {
		return err
	}

	c, err := s.DB.Client.Comment.Get(r.Context(), id)
	if err != nil {
		return err
	}
	view := comments.Comment{
		ID:          c.ID,
		UserID:      c.UserID,
		Handle:      tok.User.Handle,
		DisplayName: tok.User.DisplayName,
		Body:        c.Body,
		CreatedAt:   c.CreatedAt,
	}
	if c.EditedAt != nil {
		view.EditedAt = *c.EditedAt
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": commentDTO(view, true)})
	return nil
}
