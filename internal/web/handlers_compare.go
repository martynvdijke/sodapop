package web

import (
	"errors"
	"net/http"
	"strings"

	"sodapop/internal/catalog"
)

// comparePage renders a read-only side-by-side comparison of two to six
// drinks. Invalid selections keep the form visible with a message instead of
// failing the request, so users can adjust the query without losing the page.
func (s *Server) comparePage(w http.ResponseWriter, r *http.Request) {
	slugs := parseCompareSlugs(r)

	details, err := s.Catalog.Compare(r.Context(), slugs)
	if err != nil {
		switch {
		case errors.Is(err, catalog.ErrNotFound):
			s.render(w, r, http.StatusNotFound, "error", map[string]any{
				"Title":   "Not found",
				"Message": "One of those drinks is not in the catalog.",
			})
		case errors.Is(err, catalog.ErrCompareCount):
			s.render(w, r, http.StatusOK, "compare", map[string]any{
				"Title":        "Compare",
				"CompareError": "Choose between 2 and 6 sodas to compare.",
			})
		case errors.Is(err, catalog.ErrCompareDuplicate):
			s.render(w, r, http.StatusOK, "compare", map[string]any{
				"Title":        "Compare",
				"CompareError": "Each soda can only be compared once.",
			})
		default:
			s.fail(w, r, err)
		}
		return
	}

	s.render(w, r, http.StatusOK, "compare", map[string]any{
		"Title":   "Compare",
		"Compare": details,
	})
}

// parseCompareSlugs accepts both ?drinks=a,b and repeated ?drinks=a&drinks=b.
// Empty entries are left for the service to ignore.
func parseCompareSlugs(r *http.Request) []string {
	var slugs []string
	for _, raw := range r.URL.Query()["drinks"] {
		for _, part := range strings.Split(raw, ",") {
			slugs = append(slugs, strings.TrimSpace(part))
		}
	}
	return slugs
}
