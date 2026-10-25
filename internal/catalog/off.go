package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"sodapop/ent"
	"sodapop/ent/drinkbarcode"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

// ErrOFFNotFound means the product is not in Open Food Facts.
var ErrOFFNotFound = errors.New("product not found")
var ErrOFFRateLimited = errors.New("open food facts rate limit reached")

// OFFProduct is the subset of an Open Food Facts product Sodapop uses.
type OFFProduct struct {
	GTIN         string          `json:"code"`
	Name         string          `json:"product_name"`
	Brands       string          `json:"brands"`
	Quantity     string          `json:"quantity"`
	ImageFront   string          `json:"image_front_url"`
	Categories   string          `json:"categories_tags"`
	LastModified int64           `json:"last_modified_t"`
	Raw          json.RawMessage `json:"-"`
}

const offFields = "code,product_name,brands,quantity,product_quantity,image_front_url,categories_tags,packaging_tags,countries_tags,nutriments,ingredients_text,last_modified_t"

// OFFClient fetches Open Food Facts products with polite pacing and a hard
// ceiling of 15 product reads per minute (the documented limit).
type OFFClient struct {
	base      string
	userAgent string
	client    *http.Client
	sql       *sql.DB
	enabled   bool

	mu          sync.Mutex
	windowStart time.Time
	count       int
	lastRequest time.Time
}

func NewOFFClient(cfg *config.Config, database *db.DB) *OFFClient {
	return &OFFClient{
		base:      cfg.OFFBaseURL,
		userAgent: cfg.OFFUserAgent,
		enabled:   cfg.OFFEnabled,
		client:    &http.Client{Timeout: 15 * time.Second},
		sql:       database.SQL,
	}
}

// Get returns a product, using the local off_products cache when possible.
// The cache is negative for 7 days, which keeps unknown/retired barcodes from
// hammering the upstream API.
func (c *OFFClient) Get(ctx context.Context, gtin string) (*OFFProduct, error) {
	gtin, err := NormalizeGTIN(gtin)
	if err != nil {
		return nil, err
	}
	cached, cachedAt, found, err := c.cached(ctx, gtin)
	if err != nil {
		return nil, err
	}
	if found {
		if cached.Name != "" {
			return cached, nil
		}
		if ts, err := time.Parse(time.RFC3339, cachedAt); err == nil && time.Since(ts) < 7*24*time.Hour {
			return nil, ErrOFFNotFound
		}
	}
	if !c.enabled {
		return nil, ErrOFFNotFound
	}
	if err := c.wait(ctx); err != nil {
		return nil, err
	}

	u := fmt.Sprintf("%s/api/v3.6/product/%s.json?fields=%s",
		c.base, url.PathEscape(gtin), url.QueryEscape(offFields))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		c.storeNegative(ctx, gtin)
		return nil, ErrOFFNotFound
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrOFFRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open food facts status %d", resp.StatusCode)
	}

	var envelope struct {
		Status  int             `json:"status"`
		Product json.RawMessage `json:"product"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	if envelope.Status == 0 || len(envelope.Product) == 0 || string(envelope.Product) == "null" {
		c.storeNegative(ctx, gtin)
		return nil, ErrOFFNotFound
	}
	var product OFFProduct
	if err := json.Unmarshal(envelope.Product, &product); err != nil {
		return nil, err
	}
	if product.GTIN == "" {
		product.GTIN = gtin
	}
	product.Raw = envelope.Product
	if strings.TrimSpace(product.Name) == "" {
		c.storeNegative(ctx, gtin)
		return nil, ErrOFFNotFound
	}
	if err := c.store(ctx, gtin, product, envelope.Product); err != nil {
		// Cache write failure must not break a successful lookup.
		_ = err
	}
	return &product, nil
}

func (c *OFFClient) cached(ctx context.Context, gtin string) (*OFFProduct, string, bool, error) {
	var payload, name, brands, quantity, imageURL, nutriments, fetchedAt string
	err := c.sql.QueryRowContext(ctx, `
		SELECT payload_json, product_name, brands, quantity, COALESCE(image_front_url,''), nutriments_json, fetched_at
		FROM off_products WHERE gtin14 = ?`, gtin).
		Scan(&payload, &name, &brands, &quantity, &imageURL, &nutriments, &fetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	p := &OFFProduct{GTIN: gtin, Name: name, Brands: brands, Quantity: quantity, ImageFront: imageURL}
	if payload != "" && payload != "{}" {
		_ = json.Unmarshal([]byte(payload), p)
		p.GTIN = gtin
		p.Raw = json.RawMessage(payload)
	}
	_ = nutriments
	return p, fetchedAt, true, nil
}

func (c *OFFClient) store(ctx context.Context, gtin string, p OFFProduct, raw json.RawMessage) error {
	cached, _, _, _ := c.cached(ctx, gtin)
	if cached != nil {
		_, err := c.sql.ExecContext(ctx, `UPDATE off_products SET payload_json=?, product_name=?, brands=?, quantity=?, image_front_url=?, fetched_at=? WHERE gtin14=?`,
			string(raw), p.Name, p.Brands, p.Quantity, nullIfEmpty(p.ImageFront), db.Now(), gtin)
		return err
	}
	_, err := c.sql.ExecContext(ctx, `INSERT INTO off_products (gtin14, payload_json, product_name, brands, quantity, nutriments_json, image_front_url, license, fetched_at)
		VALUES (?, ?, ?, ?, ?, '{}', ?, 'ODbL-1.0', ?)`,
		gtin, string(raw), p.Name, p.Brands, p.Quantity, nullIfEmpty(p.ImageFront), db.Now())
	return err
}

func (c *OFFClient) storeNegative(ctx context.Context, gtin string) {
	cached, _, found, _ := c.cached(ctx, gtin)
	if found && cached != nil && cached.Name != "" {
		return
	}
	if found {
		_, _ = c.sql.ExecContext(ctx, `UPDATE off_products SET fetched_at=? WHERE gtin14=?`, db.Now(), gtin)
		return
	}
	_, _ = c.sql.ExecContext(ctx, `INSERT INTO off_products (gtin14, payload_json, product_name, brands, quantity, nutriments_json, license, fetched_at)
		VALUES (?, '{}', '', '', '', '{}', 'ODbL-1.0', ?)`, gtin, db.Now())
}

// wait enforces 1 request/second and 15 requests/minute.
func (c *OFFClient) wait(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.windowStart.IsZero() || now.Sub(c.windowStart) >= time.Minute {
		c.windowStart = now
		c.count = 0
	}
	if c.count >= 15 {
		return ErrOFFRateLimited
	}
	if since := now.Sub(c.lastRequest); since < time.Second {
		timer := time.NewTimer(time.Second - since)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	c.lastRequest = time.Now()
	c.count++
	return nil
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// BarcodeResult is the resolution of a scanned barcode.
type BarcodeResult struct {
	Status      string         `json:"status"` // matched | off | unknown
	GTIN        string         `json:"gtin14"`
	Drink       *DrinkDetail   `json:"drink,omitempty"`
	Product     *OFFProduct    `json:"product,omitempty"`
	Suggested   *SuggestedSoda `json:"suggested,omitempty"`
	Attribution string         `json:"attribution,omitempty"`
}

type SuggestedSoda struct {
	Name      string `json:"name"`
	Brand     string `json:"brand"`
	Quantity  string `json:"quantity,omitempty"`
	ImageURL  string `json:"image_url,omitempty"`
	Barcode   string `json:"barcode"`
	Source    string `json:"source"`
	License   string `json:"license"`
	Important string `json:"note"`
}

// ResolveBarcode maps a scanned code to a local drink or an OFF suggestion.
func (s *Service) ResolveBarcode(ctx context.Context, raw string) (*BarcodeResult, error) {
	gtin, err := NormalizeGTIN(raw)
	if err != nil {
		return nil, err
	}
	bc, err := s.Client.DrinkBarcode.Query().Where(drinkbarcode.Gtin14(gtin)).Only(ctx)
	if err == nil {
		detail, derr := s.GetByID(ctx, bc.DrinkID)
		if derr == nil {
			return &BarcodeResult{Status: "matched", GTIN: gtin, Drink: detail}, nil
		}
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	product, err := s.OFF.Get(ctx, gtin)
	if err == nil {
		return &BarcodeResult{
			Status:  "off",
			GTIN:    gtin,
			Product: product,
			Suggested: &SuggestedSoda{
				Name:      product.Name,
				Brand:     firstBrand(product.Brands),
				Quantity:  product.Quantity,
				ImageURL:  product.ImageFront,
				Barcode:   gtin,
				Source:    "openfoodfacts",
				License:   "ODbL-1.0",
				Important: "Open Food Facts data is ODbL licensed and kept separate from Sodapop catalog edits.",
			},
			Attribution: "Product data from Open Food Facts (ODbL 1.0).",
		}, nil
	}
	if errors.Is(err, ErrOFFNotFound) || errors.Is(err, ErrOFFRateLimited) || !s.Cfg.OFFEnabled {
		return &BarcodeResult{Status: "unknown", GTIN: gtin}, nil
	}
	return nil, err
}

func firstBrand(brands string) string {
	if i := strings.IndexByte(brands, ','); i >= 0 {
		return strings.TrimSpace(brands[:i])
	}
	return strings.TrimSpace(brands)
}
