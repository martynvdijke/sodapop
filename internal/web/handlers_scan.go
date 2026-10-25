package web

import (
	"net/http"

	"sodapop/ent"
	"sodapop/ent/style"
	"sodapop/internal/auth"
	"sodapop/internal/catalog"
)

// scanPage resolves a scanned GTIN and starts a check-in for the matching drink.
func (s *Server) scanPage(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("gtin")
	defaultVisibility := "public"
	if u := UserFromContext(r); u != nil && u.DefaultVisibility != "" {
		defaultVisibility = string(u.DefaultVisibility)
	}
	if raw == "" {
		// The check-in template owns the scanner overlay. With no drink the page
		// is just the camera/manual-entry surface.
		s.render(w, r, http.StatusOK, "checkin", map[string]any{
			"Title":             "Scan a soda",
			"Drink":             nil,
			"Ratings":           ratingChoices(),
			"Idempotency":       auth.RandomToken(16),
			"Venues":            []venueOption{},
			"DefaultVisibility": defaultVisibility,
			"AutoScan":          true,
		})
		return
	}

	gtin, err := catalog.NormalizeGTIN(raw)
	if err != nil {
		s.render(w, r, http.StatusBadRequest, "error", map[string]any{
			"Title":   "Invalid barcode",
			"Message": "That barcode is not a valid GTIN.",
		})
		return
	}

	res, err := s.Catalog.ResolveBarcode(r.Context(), gtin)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	if res.Drink == nil {
		styles, err := s.Auth.Client.Style.Query().
			Order(ent.Asc(style.FieldName)).All(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		s.render(w, r, http.StatusOK, "newdrink", map[string]any{
			"Title":   "Add a soda",
			"Styles":  styles,
			"Barcode": res.GTIN,
		})
		return
	}

	rows, _ := s.Auth.SQL.QueryContext(r.Context(), `
		SELECT id, name, COALESCE(city,'') FROM venues
		WHERE merged_into_id IS NULL ORDER BY updated_at DESC LIMIT 40`)
	venues := []venueOption{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var v venueOption
			if rows.Scan(&v.ID, &v.Name, &v.City) == nil {
				venues = append(venues, v)
			}
		}
	}
	s.render(w, r, http.StatusOK, "checkin", map[string]any{
		"Title": "Log " + res.Drink.Drink.Name, "Drink": res.Drink, "Ratings": ratingChoices(),
		"Idempotency": auth.RandomToken(16), "Venues": venues, "Scanned": true,
		"DefaultVisibility": defaultVisibility,
	})
}
