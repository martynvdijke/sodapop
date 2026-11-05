package web

import (
	"net/http"

	"sodapop/ent"
	"sodapop/ent/style"
	"sodapop/internal/catalog"
)

// scanPage resolves a scanned GTIN and starts a check-in for the matching drink.
func (s *Server) scanPage(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("gtin")
	if raw == "" {
		// The check-in template owns the scanner overlay. With no drink the page
		// is just the camera/manual-entry surface.
		data := s.checkinFormData(r.Context(), r, nil)
		data["Title"] = "Scan a soda"
		data["AutoScan"] = true
		s.render(w, r, http.StatusOK, "checkin", data)
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

	data := s.checkinFormData(r.Context(), r, res.Drink)
	data["Title"] = "Log " + res.Drink.Drink.Name
	data["Scanned"] = true
	s.render(w, r, http.StatusOK, "checkin", data)
}
