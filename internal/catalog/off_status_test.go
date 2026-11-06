package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"sodapop/internal/config"
)

// Open Food Facts v3.6 switched the envelope status from a number to strings
// such as "success" and "success_with_warnings". Decoding it as an int made
// every fresh barcode lookup fail with a 500.
func TestResolveBarcodeAcceptsOFFStringStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3.6/product/09002490100070.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": "9002490100070",
			"errors": [],
			"product": {
				"code": "9002490100070",
				"product_name": "Red Bull",
				"brands": "Red Bull GmbH",
				"quantity": "250 ml",
				"categories_tags": ["en:beverages", "en:energy-drinks"],
				"last_modified_t": 1700000000
			},
			"result": {"id": "product_found"},
			"status": "success_with_warnings",
			"warnings": []
		}`))
	}))
	defer upstream.Close()

	database := openMemory(t)
	svc := New(&config.Config{
		OFFEnabled:   true,
		OFFBaseURL:   upstream.URL,
		OFFUserAgent: "Sodapop/test",
	}, database)

	res, err := svc.ResolveBarcode(context.Background(), "9002490100070")
	if err != nil {
		t.Fatalf("resolve barcode: %v", err)
	}
	if res.Status != "off" {
		t.Fatalf("status = %q, want off", res.Status)
	}
	if res.Suggested == nil || res.Suggested.Name != "Red Bull" || res.Suggested.Brand != "Red Bull GmbH" {
		t.Fatalf("suggested = %+v, want Red Bull", res.Suggested)
	}
}

// Any upstream failure must degrade to "unknown" so the app still offers manual
// entry instead of showing a generic 500.
func TestResolveBarcodeDegradesWhenOFFFails(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden", http.StatusForbidden, `{"error":"blocked"}`},
		{"server error", http.StatusInternalServerError, `{"error":"boom"}`},
		{"bad json", http.StatusOK, `{"status":`},
		{"schema drift", http.StatusOK, `{"status": 1, "product": {"product_name": {"oops": true}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()

			svc := New(&config.Config{
				OFFEnabled:   true,
				OFFBaseURL:   upstream.URL,
				OFFUserAgent: "Sodapop/test",
			}, openMemory(t))

			res, err := svc.ResolveBarcode(context.Background(), "5449000000996")
			if err != nil {
				t.Fatalf("resolve barcode: %v", err)
			}
			if res.Status != "unknown" {
				t.Fatalf("status = %q, want unknown", res.Status)
			}
		})
	}
}
