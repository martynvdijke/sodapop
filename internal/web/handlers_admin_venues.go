package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"sodapop/internal/catalog"
	"sodapop/internal/venues"
)

type adminVenueResult struct {
	ID      int
	Name    string
	City    string
	Kind    string
	Address string
}

type adminVenueFrom struct {
	ID    int
	Name  string
	City  string
	Names []string
}

type adminVenueDetail struct {
	ID      int
	Name    string
	City    string
	Kind    string
	Address string
	Drinks  []venues.VenueDrink
}

// adminVenuesPage is the venue merge tool: search for a target venue, optionally
// pin a source venue by id, then merge the source's names into the target.
func (s *Server) adminVenuesPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if raw := strings.TrimSpace(r.URL.Query().Get("venue")); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			http.Redirect(w, r, "/admin/venues?error=invalid", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/admin/venues/"+strconv.Itoa(id), http.StatusFound)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := map[string]any{
		"Title":   "Venue merge",
		"Query":   q,
		"Results": []adminVenueResult{},
		"From":    nil,
		"Into":    0,
	}
	data["Notice"], data["Error"] = adminVenueQueryMessages(r)

	if q != "" {
		rows, err := s.Venues.Search(ctx, q, 20)
		switch {
		case err == nil:
			results := make([]adminVenueResult, 0, len(rows))
			for _, v := range rows {
				results = append(results, adminVenueResult{
					ID: v.ID, Name: v.Name, City: v.City, Kind: v.Kind, Address: v.Address,
				})
			}
			data["Results"] = results
		case errors.Is(err, catalog.ErrQueryTooShort):
			data["Error"] = "Enter at least two characters to search venues."
		default:
			data["Error"] = "Venue search is unavailable."
		}
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("into")); raw != "" {
		if id, err := strconv.Atoi(raw); err == nil && id > 0 {
			data["Into"] = id
		}
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			data["Error"] = "That source venue id is invalid."
		} else if v, _, err := s.Venues.Get(ctx, id); err != nil {
			data["Error"] = "That source venue is not available."
		} else {
			names, err := s.Venues.Names(ctx, id)
			if err != nil {
				data["Error"] = "That source venue is not available."
			} else {
				data["From"] = adminVenueFrom{ID: v.ID, Name: v.Name, City: v.City, Names: names}
			}
		}
	}

	s.render(w, r, http.StatusOK, "admin_venues", data)
}

func adminVenueQueryMessages(r *http.Request) (notice, errMsg string) {
	switch r.URL.Query().Get("updated") {
	case "1":
		notice = "Marked as on offer."
	case "0":
		notice = "Removed from offers."
	}
	if r.URL.Query().Get("merged") == "1" {
		notice = "Venues merged."
	}
	switch r.URL.Query().Get("error") {
	case "cycle":
		errMsg = "That merge would create a loop."
	case "missing":
		errMsg = "One of those venues no longer exists."
	case "invalid":
		if r.URL.Path != "/admin/venues" {
			errMsg = "That offer change was invalid."
		} else {
			errMsg = "Choose two different, valid venues."
		}
	}
	return notice, errMsg
}

// adminVenueOffers shows a single venue's drinks so an admin can mark what is
// currently on offer there.
func (s *Server) adminVenueOffers(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.Redirect(w, r, "/admin/venues?error=invalid", http.StatusFound)
		return
	}
	ctx := r.Context()
	v, _, err := s.Venues.Get(ctx, id)
	if errors.Is(err, venues.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	drinks, err := s.Venues.DrinksForAdmin(ctx, id, 0)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := map[string]any{
		"Title":   "Venue offers",
		"Query":   "",
		"Results": []adminVenueResult{},
		"From":    nil,
		"Into":    0,
		"Detail": &adminVenueDetail{
			ID: v.ID, Name: v.Name, City: v.City, Kind: v.Kind, Address: v.Address,
			Drinks: drinks,
		},
	}
	data["Notice"], data["Error"] = adminVenueQueryMessages(r)
	s.render(w, r, http.StatusOK, "admin_venues", data)
}

// adminVenueOfferSet toggles the on_offer flag for one drink at a venue.
func (s *Server) adminVenueOfferSet(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.Redirect(w, r, "/admin/venues?error=invalid", http.StatusFound)
		return
	}
	slug := strings.TrimSpace(r.FormValue("drink_slug"))
	on := r.FormValue("on")
	if slug == "" || (on != "1" && on != "0") {
		http.Redirect(w, r, "/admin/venues/"+strconv.Itoa(id)+"?error=invalid", http.StatusFound)
		return
	}
	found, err := s.Venues.SetOnOffer(r.Context(), id, slug, on == "1")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !found {
		http.Redirect(w, r, "/admin/venues?error=missing", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/venues/"+strconv.Itoa(id)+"?updated="+on, http.StatusFound)
}

// adminVenueMerge folds the source venue into the target and records an audit
// revision on the surviving venue.
func (s *Server) adminVenueMerge(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	fromID, fromErr := strconv.Atoi(r.FormValue("from_id"))
	intoID, intoErr := strconv.Atoi(r.FormValue("into_id"))
	if fromErr != nil || intoErr != nil || fromID <= 0 || intoID <= 0 || fromID == intoID {
		http.Redirect(w, r, "/admin/venues?error=invalid", http.StatusFound)
		return
	}

	userID := ""
	if u := UserFromContext(r); u != nil {
		userID = u.ID
	}
	if err := s.Venues.Merge(r.Context(), fromID, intoID, userID); err != nil {
		switch {
		case errors.Is(err, venues.ErrMergeCycle):
			http.Redirect(w, r, "/admin/venues?error=cycle", http.StatusFound)
		case errors.Is(err, venues.ErrNotFound):
			http.Redirect(w, r, "/admin/venues?error=missing", http.StatusFound)
		case errors.Is(err, venues.ErrInvalid):
			http.Redirect(w, r, "/admin/venues?error=invalid", http.StatusFound)
		default:
			s.fail(w, r, err)
		}
		return
	}

	var actor *string
	if userID != "" {
		actor = &userID
	}
	if err := s.Catalog.RecordRevision(r.Context(), "venue", intoID, actor, "merge",
		map[string]any{"merged_from": fromID},
		map[string]any{"merged_into": intoID}); err != nil {
		// The merge is already committed; an audit failure must not surface as a
		// failed merge.
		s.Log.Warn("record venue merge revision", "err", err, "from", fromID, "into", intoID)
	}
	http.Redirect(w, r, "/admin/venues?merged=1", http.StatusFound)
}
