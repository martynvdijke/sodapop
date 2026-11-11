package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"sodapop/internal/auth"
	"sodapop/internal/catalog"
)

// barcodeContributeLimit is the per-member hourly cap on contributed barcodes,
// on top of the protectStrict per-minute bucket the route already applies.
const barcodeContributeLimit = 20

// contributeBarcode attaches a member-supplied barcode to the drink named in the
// path. The response body is deliberately flat ({gtin, created}) because it is
// the contract the Android client reads; everything else in the API wraps its
// payload in data, and this endpoint's caller was specified against the flat
// shape.
func (s *Server) contributeBarcode(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		GTIN      string `json:"gtin"`
		Symbology string `json:"symbology"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.GTIN) == "" {
		return bad("gtin is required")
	}
	if s.Limiter != nil && !s.Limiter.Allow("barcode:"+tok.User.ID, barcodeContributeLimit, time.Hour) {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many barcode contributions; try again later")
		return nil
	}

	gtin, created, err := s.Catalog.ContributeBarcode(r.Context(), tok.User.ID,
		r.PathValue("slug"), in.GTIN, in.Symbology)
	if err != nil {
		var conflict *catalog.BarcodeConflictError
		switch {
		case errors.Is(err, catalog.ErrInvalidBarcode):
			writeError(w, http.StatusBadRequest, "invalid_barcode",
				"barcode must be a valid GTIN-8/12/13/14 with a correct check digit")
			return nil
		case errors.As(err, &conflict):
			message := "that barcode already belongs to another drink"
			if conflict.DrinkName != "" {
				message = fmt.Sprintf("that barcode already belongs to %s", conflict.DrinkName)
			}
			writeError(w, http.StatusConflict, "barcode_taken", message)
			return nil
		case errors.Is(err, catalog.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "drink not found")
			return nil
		}
		return err
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"gtin": gtin, "created": created})
	return nil
}
