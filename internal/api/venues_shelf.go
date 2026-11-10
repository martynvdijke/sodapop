package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sodapop/internal/auth"
	"sodapop/internal/venues"
)

// shelfNoteMaxRunes mirrors the venues service's stored note limit. The handler
// checks it too so the caller gets a targeted message instead of the service's
// wrapped ErrInvalid.
const shelfNoteMaxRunes = 140

// shelfMaxPriceCents mirrors the venues service's price ceiling. Prices are
// plain minor units; Sodapop has no currency concept.
const shelfMaxPriceCents = 100000

// venueShelfGet returns the merged shelf for one venue: member stock reports and
// recent check-ins collapsed to one row per drink. The result is scoped to the
// viewer, so Mine/ReportID only ever describe the caller's own report.
func (s *Server) venueShelfGet(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return bad("invalid venue id")
	}
	entries, err := s.Venues.Shelf(r.Context(), id, tok.User.ID, time.Now())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": entries,
		"meta": map[string]any{"count": len(entries)},
	})
	return nil
}

// venueShelfPost records that the caller has seen a drink at a venue. It is an
// upsert: reporting the same drink again refreshes the sighting, price and note.
func (s *Server) venueShelfPost(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		return bad("invalid venue id")
	}
	var in struct {
		DrinkSlug  string `json:"drink_slug"`
		PriceCents *int   `json:"price_cents"`
		Note       string `json:"note"`
		SeenAt     string `json:"seen_at"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}

	slug := strings.TrimSpace(in.DrinkSlug)
	if slug == "" {
		return bad("drink_slug is required")
	}
	if in.PriceCents != nil && (*in.PriceCents < 0 || *in.PriceCents > shelfMaxPriceCents) {
		return bad("price_cents must be between 0 and 100000")
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > shelfNoteMaxRunes {
		return bad("note must be 140 characters or fewer")
	}
	seenAt := strings.TrimSpace(in.SeenAt)
	if seenAt != "" {
		if _, err := time.Parse(time.RFC3339, seenAt); err != nil {
			return bad("seen_at must be RFC 3339")
		}
	}

	entry, err := s.Venues.ReportShelf(r.Context(), tok.User.ID, id, venues.ShelfReportInput{
		DrinkSlug:  slug,
		PriceCents: in.PriceCents,
		Note:       note,
		// An omitted seen_at means "seen just now", which is what the app means
		// when it does not ask the member for a time.
		SeenNow: seenAt == "",
		SeenAt:  seenAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, venues.ErrDrinkNotFound):
			writeError(w, http.StatusNotFound, "not_found", "drink not found")
		case errors.Is(err, venues.ErrSeenAtFuture):
			writeError(w, http.StatusBadRequest, "invalid_request", "seen_at must not be in the future")
		case errors.Is(err, venues.ErrSeenAtTooOld):
			writeError(w, http.StatusBadRequest, "invalid_request", "seen_at must be within the last 30 days")
		default:
			// mapError turns ErrInvalid/ErrNotFound into their 400/404 shapes and
			// everything else into a 500.
			return err
		}
		return nil
	}
	if entry == nil {
		s.internal(w, errors.New("shelf report produced no entry"))
		return nil
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": entry})
	return nil
}

// venueShelfDelete retracts the caller's own stock report. A report that does
// not exist, belongs to someone else, or was already retracted is a 404: the API
// never reveals another member's report id.
func (s *Server) venueShelfDelete(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if _, err := strconv.Atoi(r.PathValue("id")); err != nil {
		return bad("invalid venue id")
	}
	ok, err := s.Venues.RetractShelf(r.Context(), tok.User.ID, r.PathValue("report_id"))
	if err != nil {
		return err
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "report not found")
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
