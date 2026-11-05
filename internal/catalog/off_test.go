package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sodapop/internal/config"
)

// Off product payloads use a JSON array for categories_tags. Treating it as a
// string makes json.Unmarshal fail, which used to turn every fresh barcode
// lookup into a 500 on the API and app.
func TestOFFProductDecodesCategoryTagsArray(t *testing.T) {
	raw := []byte(`{
		"code": "5449000000996",
		"product_name": "Coca-Cola",
		"brands": "Coca-Cola",
		"quantity": "330 ml",
		"image_front_url": "https://images.openfoodfacts.org/cola.jpg",
		"categories_tags": ["en:beverages", "en:sodas"],
		"last_modified_t": 1700000000
	}`)

	var product OFFProduct
	if err := json.Unmarshal(raw, &product); err != nil {
		t.Fatalf("unmarshal product: %v", err)
	}
	if product.GTIN != "5449000000996" || product.Name != "Coca-Cola" {
		t.Fatalf("unexpected product: %+v", product)
	}
	if len(product.Categories) != 2 || product.Categories[0] != "en:beverages" {
		t.Fatalf("categories = %v, want [en:beverages en:sodas]", product.Categories)
	}
}

// Older cached payloads sometimes omit categories_tags or carry null. Neither
// should stop the cached product from loading.
func TestOFFProductDecodesMissingCategoryTags(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"code":"5449000000996","product_name":"Coca-Cola"}`),
		[]byte(`{"code":"5449000000996","product_name":"Coca-Cola","categories_tags":null}`),
	} {
		var product OFFProduct
		if err := json.Unmarshal(raw, &product); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if product.Name != "Coca-Cola" || product.Categories != nil {
			t.Fatalf("unexpected product for %s: %+v", raw, product)
		}
	}
}

// A real Open Food Facts response must resolve to an OFF suggestion instead of
// surfacing a 500. This is the regression that broke the mobile scanner.
func TestResolveBarcodeAcceptsOFFCategoryArray(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3.6/product/05449000000996.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": 1,
			"product": {
				"code": "5449000000996",
				"product_name": "Coca-Cola",
				"brands": "Coca-Cola, The Coca-Cola Company",
				"quantity": "330 ml",
				"image_front_url": "https://images.openfoodfacts.org/cola.jpg",
				"categories_tags": ["en:beverages", "en:sodas", "en:colas"],
				"last_modified_t": 1700000000
			}
		}`))
	}))
	defer upstream.Close()

	database := openMemory(t)
	svc := New(&config.Config{
		OFFEnabled:   true,
		OFFBaseURL:   upstream.URL,
		OFFUserAgent: "Sodapop/test",
	}, database)

	res, err := svc.ResolveBarcode(context.Background(), "5449000000996")
	if err != nil {
		t.Fatalf("resolve barcode: %v", err)
	}
	if res.Status != "off" {
		t.Fatalf("status = %q, want off", res.Status)
	}
	if res.Suggested == nil || res.Suggested.Name != "Coca-Cola" || res.Suggested.Brand != "Coca-Cola" {
		t.Fatalf("suggested = %+v, want Coca-Cola", res.Suggested)
	}
}
