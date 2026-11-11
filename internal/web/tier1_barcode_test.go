package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"sodapop/ent"
	"sodapop/ent/drinkbarcode"
)

const barcodeProbe = `E={{.BarcodeError}}|N={{.BarcodeNotice}}|V={{.BarcodeValue}}|C={{with .BarcodeConflict}}{{.Slug}}/{{.Name}}{{end}}`

func (h *v12Harness) barcodeDrink(t *testing.T, name, slug string) *ent.Drink {
	t.Helper()
	d, err := h.db.Client.Drink.Create().SetName(name).SetSlug(slug).Save(context.Background())
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	return d
}

func TestDrinkBarcodeContributeSuccess(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "alice")
	d := h.barcodeDrink(t, "Cola", "cola")

	form := url.Values{"csrf_token": {"test-csrf"}, "code": {"5449 0000 00996"}, "symbology": {"ean13"}}
	rec := h.renderWith(t, "drink", barcodeProbe)
	req := h.request(http.MethodPost, "/drinks/cola/barcodes", u, form)
	req.SetPathValue("slug", d.Slug)
	h.srv.drinkBarcodeContribute(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/drinks/cola?barcode=added#barcodes" {
		t.Fatalf("location = %q", loc)
	}
	row, err := h.db.Client.DrinkBarcode.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("barcode: %v", err)
	}
	if row.Gtin14 != "05449000000996" || row.DrinkID != d.ID {
		t.Fatalf("row = %+v", row)
	}
	if row.Source != drinkbarcode.SourceUser {
		t.Fatalf("source = %q", row.Source)
	}
}

func TestDrinkBarcodeContributeAlreadyPresent(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "alice")
	h.barcodeDrink(t, "Cola", "cola")

	for i := 0; i < 2; i++ {
		form := url.Values{"csrf_token": {"test-csrf"}, "code": {"5449000000996"}}
		rec := h.renderWith(t, "drink", barcodeProbe)
		req := h.request(http.MethodPost, "/drinks/cola/barcodes", u, form)
		req.SetPathValue("slug", "cola")
		h.srv.drinkBarcodeContribute(rec, req)
		want := "/drinks/cola?barcode=added#barcodes"
		if i == 1 {
			want = "/drinks/cola?barcode=present#barcodes"
		}
		if loc := rec.Header().Get("Location"); loc != want {
			t.Fatalf("post %d location = %q, want %q", i, loc, want)
		}
	}
	if n, _ := h.db.Client.DrinkBarcode.Query().Count(context.Background()); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
}

func TestDrinkBarcodeContributeInvalid(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "alice")
	h.barcodeDrink(t, "Cola", "cola")

	form := url.Values{"csrf_token": {"test-csrf"}, "code": {"5449000000995"}}
	rec := h.renderWith(t, "drink", barcodeProbe)
	req := h.request(http.MethodPost, "/drinks/cola/barcodes", u, form)
	req.SetPathValue("slug", "cola")
	h.srv.drinkBarcodeContribute(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "not a valid barcode") {
		t.Fatalf("missing message: %s", body)
	}
	// The submitted value comes back so the member can fix one digit.
	if !strings.Contains(body, "V=5449000000995") {
		t.Fatalf("submitted code was not kept: %s", body)
	}
}

func TestDrinkBarcodeContributeConflict(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "alice")
	owner := h.barcodeDrink(t, "Owner Cola", "owner-cola")
	h.barcodeDrink(t, "Other Cola", "other-cola")

	// The code belongs to owner-cola; the member is looking at other-cola.
	if _, err := h.db.Client.DrinkBarcode.Create().
		SetDrinkID(owner.ID).SetGtin14("05449000000996").SetSource(drinkbarcode.SourceOff).
		Save(context.Background()); err != nil {
		t.Fatalf("seed barcode: %v", err)
	}

	form := url.Values{"csrf_token": {"test-csrf"}, "code": {"5449000000996"}}
	rec := h.renderWith(t, "drink", barcodeProbe)
	req := h.request(http.MethodPost, "/drinks/other-cola/barcodes", u, form)
	req.SetPathValue("slug", "other-cola")
	h.srv.drinkBarcodeContribute(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "already belongs to another soda") {
		t.Fatalf("missing conflict message: %s", body)
	}
	if !strings.Contains(body, "owner-cola/Owner Cola") {
		t.Fatalf("conflict target missing: %s", body)
	}
	if n, _ := h.db.Client.DrinkBarcode.Query().Count(context.Background()); n != 1 {
		t.Fatalf("conflict stored a row: %d", n)
	}
}

func TestDrinkBarcodeContributeGuards(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "alice")
	h.barcodeDrink(t, "Cola", "cola")

	t.Run("bad CSRF is refused", func(t *testing.T) {
		form := url.Values{"csrf_token": {"wrong"}, "code": {"5449000000996"}}
		rec := h.renderWith(t, "drink", barcodeProbe)
		req := h.request(http.MethodPost, "/drinks/cola/barcodes", u, form)
		req.SetPathValue("slug", "cola")
		h.srv.drinkBarcodeContribute(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("anonymous is sent to login", func(t *testing.T) {
		form := url.Values{"csrf_token": {"test-csrf"}, "code": {"5449000000996"}}
		rec := h.renderWith(t, "drink", barcodeProbe)
		req := h.request(http.MethodPost, "/drinks/cola/barcodes", nil, form)
		req.SetPathValue("slug", "cola")
		h.srv.drinkBarcodeContribute(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want redirect", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Fatalf("location = %q", loc)
		}
	})

	t.Run("unknown drink is 404", func(t *testing.T) {
		form := url.Values{"csrf_token": {"test-csrf"}, "code": {"5449000000996"}}
		rec := h.renderWith(t, "drink", barcodeProbe)
		req := h.request(http.MethodPost, "/drinks/nope/barcodes", u, form)
		req.SetPathValue("slug", "nope")
		h.srv.drinkBarcodeContribute(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// The drink page must list the codes it has and offer the contribution form.
func TestDrinkPageShowsBarcodesAndForm(t *testing.T) {
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = renderer
	u := h.user(t, "alice")
	// Verified, so the contribution form renders.
	if err := h.db.Client.User.UpdateOneID(u.ID).SetEmailVerifiedAt("2026-01-01T00:00:00Z").Exec(context.Background()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	u, err = h.db.Client.User.Get(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	d := h.barcodeDrink(t, "Cola", "cola")
	if _, err := h.db.Client.DrinkBarcode.Create().
		SetDrinkID(d.ID).SetGtin14("05449000000996").SetSymbology("ean13").
		SetSource(drinkbarcode.SourceUser).Save(context.Background()); err != nil {
		t.Fatalf("barcode: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/drinks/cola", nil)
	ctx := context.WithValue(req.Context(), userKey{}, u)
	req = req.WithContext(ctx)
	req.SetPathValue("slug", "cola")
	rec := httptest.NewRecorder()
	h.srv.drinkPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"05449000000996",
		`action="/drinks/cola/barcodes"`,
		"Add a barcode",
		"ean13",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("drink page missing %q", want)
		}
	}
}
