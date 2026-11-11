package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sodapop/internal/catalog"
)

// barcodeContributeLimit is the per-member hourly cap on contributed barcodes.
// It is deliberately generous for a real shopper and far too small for someone
// trying to seed the catalog from a product list.
const barcodeContributeLimit = 20

// barcodeConflictView is the template-facing shape of a barcode owned by
// another drink, so the page can link to it without template gymnastics.
type barcodeConflictView struct {
	Slug string
	Name string
}

// drinkBarcodeContribute attaches a barcode the member typed or scanned to the
// drink on the page. Failures re-render the page with a message; success
// redirects with a short notice, matching how the other drink mutations behave.
func (s *Server) drinkBarcodeContribute(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	d, err := s.Catalog.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.drinkNotFound(w, r)
		return
	}

	fail := func(status int, message string, extra map[string]any) {
		data := map[string]any{
			"BarcodeError": message,
			"BarcodeValue": strings.TrimSpace(r.FormValue("code")),
		}
		for k, v := range extra {
			data[k] = v
		}
		s.renderDrinkPage(w, r, status, d, data)
	}

	if s.Limiter != nil && !s.Limiter.Allow("barcode:"+u.ID, barcodeContributeLimit, time.Hour) {
		fail(http.StatusTooManyRequests, "You have added a lot of barcodes today. Try again later.", nil)
		return
	}

	gtin, created, err := s.Catalog.ContributeBarcode(r.Context(), u.ID, d.Drink.Slug,
		r.FormValue("code"), r.FormValue("symbology"))
	if err != nil {
		var conflict *catalog.BarcodeConflictError
		switch {
		case errors.Is(err, catalog.ErrInvalidBarcode):
			fail(http.StatusBadRequest, "That is not a valid barcode. Check the digits under the lines and try again.", nil)
		case errors.As(err, &conflict):
			fail(http.StatusConflict, "That barcode already belongs to another soda.", map[string]any{
				"BarcodeConflict": barcodeConflictView{Slug: conflict.DrinkSlug, Name: conflict.DrinkName},
			})
		case errors.Is(err, catalog.ErrNotFound):
			s.drinkNotFound(w, r)
		default:
			s.fail(w, r, err)
		}
		return
	}
	if !created {
		// The code already pointed at this drink: say so rather than claiming a
		// new contribution.
		http.Redirect(w, r, "/drinks/"+url.PathEscape(d.Drink.Slug)+"?barcode=present#barcodes", http.StatusFound)
		return
	}
	s.Log.Info("barcode contributed", "user_id", u.ID, "drink_id", d.Drink.ID, "gtin14", gtin)
	http.Redirect(w, r, "/drinks/"+url.PathEscape(d.Drink.Slug)+"?barcode=added#barcodes", http.StatusFound)
}
