package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sodapop/internal/checkins"
	"sodapop/internal/comments"
)

// checkinEditForm holds the values the edit page shows, so a validation error
// can re-render exactly what the member submitted instead of the stored row.
type checkinEditForm struct {
	Rating      string
	Serving     string
	PrepMethod  string
	PrepNote    string
	Note        string
	Visibility  string
	VenueID     int
	PriceAmount string
	Currency    string
}

// editFormFromDetail prefills the form from a stored check-in.
func editFormFromDetail(d *checkins.Detail) checkinEditForm {
	form := checkinEditForm{
		Serving:    d.Serving,
		PrepMethod: d.PrepMethod,
		PrepNote:   d.PrepNote,
		Note:       d.Note,
		Visibility: d.Visibility,
		Currency:   d.PriceCurrency,
	}
	if d.Rating != nil {
		form.Rating = fmt.Sprintf("%.2f", float64(*d.Rating)/4)
	}
	if d.VenueID != nil {
		form.VenueID = *d.VenueID
	}
	if d.PriceCents != nil {
		form.PriceAmount = fmt.Sprintf("%d.%02d", *d.PriceCents/100, *d.PriceCents%100)
	}
	return form
}

// checkinEditData is the view model for checkin_edit.html.
func (s *Server) checkinEditData(ctx context.Context, r *http.Request, detail *checkins.Detail, form checkinEditForm, message string) map[string]any {
	return map[string]any{
		"Title":   "Edit check-in",
		"Checkin": detail,
		"Form":    form,
		"Error":   message,
		"Ratings": ratingChoices(),
		"Venues":  s.checkinVenueOptions(ctx, "", form.VenueID),
	}
}

// checkinEditNotFound renders the same 404 the detail page uses, so a non-owner
// cannot tell a foreign check-in from a missing one.
func (s *Server) checkinEditNotFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusNotFound, "error", map[string]any{
		"Title": "Not found", "Message": "That checkin is not available.",
	})
}

// ownedEditableCheckin loads a check-in the caller wrote and may still edit.
// Every refusal is reported as not-found via nil so callers cannot leak the
// existence of another member's row or of an expired edit.
func (s *Server) ownedEditableCheckin(ctx context.Context, r *http.Request) *checkins.Detail {
	u := UserFromContext(r)
	if u == nil {
		return nil
	}
	detail, err := s.Checkins.Get(ctx, u.ID, r.PathValue("id"))
	if err != nil || detail.UserID != u.ID {
		return nil
	}
	if !checkins.CanEdit(detail.CreatedAt, time.Now()) {
		return nil
	}
	return detail
}

// checkinEditPage renders the author edit form inside the 24-hour window.
func (s *Server) checkinEditPage(w http.ResponseWriter, r *http.Request) {
	detail := s.ownedEditableCheckin(r.Context(), r)
	if detail == nil {
		s.checkinEditNotFound(w, r)
		return
	}
	form := editFormFromDetail(detail)
	s.render(w, r, http.StatusOK, "checkin_edit", s.checkinEditData(r.Context(), r, detail, form, ""))
}

// checkinEditSubmit parses the edit form and applies it. Validation errors
// re-render the form with the submitted values; a closed window or a foreign row
// is a 404 like the GET page.
func (s *Server) checkinEditSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	detail, err := s.Checkins.Get(r.Context(), u.ID, r.PathValue("id"))
	if err != nil || detail.UserID != u.ID {
		s.checkinEditNotFound(w, r)
		return
	}

	form := checkinEditForm{
		Rating:      strings.TrimSpace(r.FormValue("rating")),
		Serving:     strings.TrimSpace(r.FormValue("serving")),
		PrepMethod:  strings.TrimSpace(r.FormValue("prep_method")),
		PrepNote:    strings.TrimSpace(r.FormValue("prep_note")),
		Note:        strings.TrimSpace(r.FormValue("note")),
		Visibility:  strings.TrimSpace(r.FormValue("visibility")),
		PriceAmount: strings.TrimSpace(r.FormValue("price")),
		Currency:    strings.ToUpper(strings.TrimSpace(r.FormValue("currency"))),
	}
	if v, convErr := strconv.Atoi(strings.TrimSpace(r.FormValue("venue_id"))); convErr == nil && v > 0 {
		form.VenueID = v
	}

	fail := func(status int, message string) {
		s.render(w, r, status, "checkin_edit", s.checkinEditData(r.Context(), r, detail, form, message))
	}

	in := checkins.UpdateInput{
		Serving:    &form.Serving,
		PrepMethod: &form.PrepMethod,
		PrepNote:   &form.PrepNote,
		Note:       &form.Note,
	}
	if form.Serving == "" {
		in.Serving = nil
	}
	if form.PrepMethod == "" {
		in.PrepMethod = nil
	}
	if form.Visibility != "" {
		in.Visibility = &form.Visibility
	}
	if form.Rating == "" {
		in.ClearRating = true
	} else {
		f, convErr := strconv.ParseFloat(form.Rating, 64)
		if convErr != nil || f < 0 || f > 5 {
			fail(http.StatusBadRequest, "Choose a rating between 0 and 5 stars.")
			return
		}
		scaled := int(math.Round(f * 4))
		in.Rating = &scaled
	}
	if form.VenueID > 0 {
		venueID := form.VenueID
		in.VenueID = &venueID
	} else if !detail.AtHome {
		// An @home check-in points at the shared hidden venue. The picker never
		// lists it, so treating "no venue selected" as a clear would silently
		// drop the @home marker. Leave it untouched instead.
		in.ClearVenue = true
	}
	price, priceErr := parseShelfPrice(form.PriceAmount)
	if priceErr != nil {
		fail(http.StatusBadRequest, "Enter a price like 1.50, or leave it blank.")
		return
	}
	if price == nil {
		in.ClearPrice = true
	} else {
		in.PriceCents = price
	}
	in.PriceCurrency = &form.Currency

	if _, err := s.Checkins.Update(r.Context(), u.ID, detail.ID, in); err != nil {
		switch {
		case errors.Is(err, checkins.ErrNotFound):
			s.checkinEditNotFound(w, r)
		case errors.Is(err, checkins.ErrEditWindowClosed):
			fail(http.StatusForbidden, "This check-in is more than 24 hours old and can no longer be edited.")
		case errors.Is(err, checkins.ErrInvalidRating):
			fail(http.StatusBadRequest, "Choose a rating between 0 and 5 stars.")
		case errors.Is(err, checkins.ErrInvalidCurrency):
			fail(http.StatusBadRequest, "Currency must be a three-letter code such as EUR, or blank.")
		case errors.Is(err, checkins.ErrInvalidPrice):
			fail(http.StatusBadRequest, "Enter a price like 1.50, or leave it blank.")
		case errors.Is(err, checkins.ErrInvalidInput):
			fail(http.StatusBadRequest, "Check the note, venue and visibility, then try again.")
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/checkins/"+detail.ID, http.StatusFound)
}

// commentEdit rewrites the body of a comment the viewer authored. A foreign or
// missing comment is a 404; an invalid body returns to the thread with the
// existing comment error banner.
func (s *Server) commentEdit(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	commentID := r.PathValue("id")
	body := r.FormValue("body")

	checkinID := strings.TrimSpace(r.FormValue("checkin_id"))
	if checkinID == "" {
		if c, err := s.DB.Client.Comment.Get(r.Context(), commentID); err == nil {
			checkinID = c.CheckinID
		}
	}

	if err := s.commentService().Edit(r.Context(), u.ID, commentID, body); err != nil {
		switch {
		case errors.Is(err, comments.ErrNotFound):
			http.NotFound(w, r)
		case errors.Is(err, comments.ErrInvalid):
			code := "empty"
			if len([]rune(strings.TrimSpace(body))) > 1000 {
				code = "long"
			}
			target := "/"
			if checkinID != "" {
				target = "/checkins/" + checkinID + "?comment_error=" + code + "#comments"
			}
			http.Redirect(w, r, target, http.StatusFound)
		default:
			s.fail(w, r, err)
		}
		return
	}

	target := "/"
	if checkinID != "" {
		target = "/checkins/" + checkinID + "#comment-" + commentID
	}
	http.Redirect(w, r, target, http.StatusFound)
}
