// Package geocode talks to a Photon (OpenStreetMap) geocoder for nearby place
// suggestions. It is only ever called server-side: the browser sends a coarse
// fix to our own API, never to the provider.
package geocode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrDisabled means the feature is switched off by configuration.
var ErrDisabled = errors.New("nearby geocoder disabled")

// ErrBusy means the global in-flight cap is saturated. It is not a provider
// failure; callers should still answer with local results.
var ErrBusy = errors.New("nearby geocoder busy")

const (
	cacheTTL     = time.Hour
	cacheEntries = 512
	// maxInflight caps concurrent provider requests so one slow Photon cannot
	// tie up every handler.
	maxInflight = 4
)

// Place is one normalized Photon feature.
type Place struct {
	ProviderID string
	Name       string
	Kind       string
	City       string
	Country    string
	Lat        float64
	Lng        float64
}

type cacheEntry struct {
	places  []Place
	expires time.Time
}

// Client is a small in-memory, server-proxied Photon client. Results are keyed
// by geohash-7, never by raw coordinates, so the cache cannot reconstruct a
// precise fix.
type Client struct {
	Enabled   bool
	BaseURL   string
	UserAgent string
	HTTP      *http.Client

	mu      sync.Mutex
	cache   map[string]cacheEntry
	order   []string
	sem     chan struct{}
	nowFunc func() time.Time
}

// New builds a client from resolved configuration.
func New(enabled bool, baseURL, userAgent string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 2500 * time.Millisecond
	}
	return &Client{
		Enabled:   enabled,
		BaseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: timeout},
		cache:     make(map[string]cacheEntry),
		sem:       make(chan struct{}, maxInflight),
		nowFunc:   time.Now,
	}
}

// Nearby returns places near a fix. Failures and a saturated in-flight cap
// return an error; the caller must treat that as "no external results", never
// as a request failure.
func (c *Client) Nearby(ctx context.Context, lat, lng float64) ([]Place, error) {
	if c == nil || !c.Enabled || c.BaseURL == "" {
		return nil, ErrDisabled
	}
	key := geohash(lat, lng, 7)
	if places, ok := c.cached(key); ok {
		return places, nil
	}

	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	default:
		return nil, ErrBusy
	}

	places, err := c.fetch(ctx, lat, lng)
	if err != nil {
		return nil, err
	}
	c.store(key, places)
	return places, nil
}

func (c *Client) cached(key string) ([]Place, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok {
		return nil, false
	}
	if c.nowFunc().After(entry.expires) {
		delete(c.cache, key)
		for i, k := range c.order {
			if k == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				break
			}
		}
		return nil, false
	}
	return entry.places, true
}

func (c *Client) store(key string, places []Place) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.cache[key]; !ok {
		c.order = append(c.order, key)
	}
	c.cache[key] = cacheEntry{places: places, expires: c.nowFunc().Add(cacheTTL)}
	for len(c.order) > cacheEntries {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.cache, oldest)
	}
}

// fetch queries Photon and normalizes the GeoJSON response.
func (c *Client) fetch(ctx context.Context, lat, lng float64) ([]Place, error) {
	q := url.Values{}
	q.Set("lat", fmt.Sprintf("%.3f", lat))
	q.Set("lon", fmt.Sprintf("%.3f", lng))
	q.Set("limit", "15")
	endpoint := c.BaseURL + "/api?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("photon status %d", resp.StatusCode)
	}

	var payload struct {
		Features []struct {
			Geometry struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				OSMType  string `json:"osm_type"`
				OSMID    int64  `json:"osm_id"`
				Name     string `json:"name"`
				City     string `json:"city"`
				Country  string `json:"country"`
				OSMValue string `json:"osm_value"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}

	out := make([]Place, 0, len(payload.Features))
	for _, f := range payload.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		lngValue, latValue := f.Geometry.Coordinates[0], f.Geometry.Coordinates[1]
		if math.IsNaN(latValue) || math.IsNaN(lngValue) || math.Abs(latValue) > 90 || math.Abs(lngValue) > 180 {
			continue
		}
		name := strings.TrimSpace(f.Properties.Name)
		if name == "" {
			continue
		}
		providerID := strings.TrimSpace(f.Properties.OSMType)
		if providerID != "" && f.Properties.OSMID != 0 {
			providerID = fmt.Sprintf("%s%d", providerID, f.Properties.OSMID)
		}
		if providerID == "" {
			continue
		}
		out = append(out, Place{
			ProviderID: providerID,
			Name:       name,
			Kind:       kindFromOSM(f.Properties.OSMValue),
			City:       strings.TrimSpace(f.Properties.City),
			Country:    strings.TrimSpace(f.Properties.Country),
			Lat:        latValue,
			Lng:        lngValue,
		})
	}
	return out, nil
}

// kindFromOSM maps an OpenStreetMap value onto Sodapop's venue kinds.
func kindFromOSM(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bar", "pub", "biergarten":
		return "bar"
	case "restaurant", "fast_food", "food_court":
		return "restaurant"
	case "cafe", "ice_cream":
		return "cafe"
	case "hotel", "hostel", "guest_house", "motel":
		return "hotel"
	case "vending_machine":
		return "vending"
	case "shop", "supermarket", "convenience", "kiosk", "beverages", "greengrocer":
		return "shop"
	case "festival":
		return "festival"
	default:
		return "other"
	}
}

const geohashBase32 = "0123456789bcdefghjkmnpqrstuvwxyz"

// geohash encodes a coordinate into a standard base-32 geohash. Only the hash
// is used as a cache key; the coordinate itself is never stored.
func geohash(lat, lng float64, precision int) string {
	if precision <= 0 {
		return ""
	}
	latMin, latMax := -90.0, 90.0
	lngMin, lngMax := -180.0, 180.0
	var out strings.Builder
	out.Grow(precision)
	bit := 0
	ch := 0
	even := true
	for out.Len() < precision {
		if even {
			mid := (lngMin + lngMax) / 2
			if lng >= mid {
				ch = ch*2 + 1
				lngMin = mid
			} else {
				ch = ch * 2
				lngMax = mid
			}
		} else {
			mid := (latMin + latMax) / 2
			if lat >= mid {
				ch = ch*2 + 1
				latMin = mid
			} else {
				ch = ch * 2
				latMax = mid
			}
		}
		even = !even
		bit++
		if bit == 5 {
			out.WriteByte(geohashBase32[ch])
			bit = 0
			ch = 0
		}
	}
	return out.String()
}
