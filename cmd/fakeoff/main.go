// Command fakeoff is a tiny Open Food Facts stand-in for local development and
// E2E tests. It serves the small v3 product shape Sodapop consumes.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

type product struct {
	Code    string         `json:"code"`
	Product map[string]any `json:"product"`
	Status  int            `json:"status"`
}

var fixtures = map[string]map[string]any{
	"5449000000996": {
		"product_name": "Coca-Cola",
		"brands":       "Coca-Cola",
		"quantity":     "330 ml",
		"categories_tags": []string{
			"en:beverages", "en:soft-drinks", "en:colas",
		},
		"nutriments": map[string]any{
			"sugars_100g":      10.6,
			"energy-kcal_100g": 42,
			"caffeine_100g":    0.01,
		},
		"image_front_url": "https://images.openfoodfacts.org/images/products/544/900/000/0996/front_en.1.400.jpg",
	},
	"5060337500128": {
		"product_name": "Fentimans Curiosity Cola",
		"brands":       "Fentimans",
		"quantity":     "275 ml",
		"categories_tags": []string{
			"en:beverages", "en:soft-drinks", "en:colas",
		},
		"nutriments": map[string]any{
			"sugars_100g":      9.8,
			"energy-kcal_100g": 40,
		},
	},
}

func main() {
	addr := os.Getenv("FAKEOFF_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8099"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3.6/product/{code}.json", func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSuffix(r.PathValue("code"), ".json")
		p, ok := fixtures[code]
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": code, "status": 0,
				"status_verbose": "product not found",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(product{Code: code, Product: p, Status: 1})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	log.Printf("fakeoff listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
