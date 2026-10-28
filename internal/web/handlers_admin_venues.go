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

// adminVenuesPage is the venue merge tool: search for a target venue, optionally
// pin a source venue by id, then merge the source's names into the target.
func (s *Server) adminVenuesPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	if r.URL.Query().Get("merged") == "1" {
		notice = "Venues merged."
	}
	switch r.URL.Query().Get("error") {
	case "cycle":
		errMsg = "That merge would create a loop."
	case "missing":
		errMsg = "One of those venues no longer exists."
	case "invalid":
		errMsg = "Choose two different, valid venues."
	}
	return notice, errMsg
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
