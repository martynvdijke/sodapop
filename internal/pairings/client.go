// Package pairings reads sandwich pairings from the sibling Sandwitches app.
//
// The integration is deliberately one-way and optional: Sodapop calls a single
// read-only endpoint over HTTP and never exposes the result as an API route.
package pairings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"sodapop/internal/telemetry"
)

const (
	// positiveTTL is how long a successful lookup is reused. Pairings change
	// slowly, and a drink page should not fan out to Sandwitches on every view.
	positiveTTL = 5 * time.Minute
	// negativeTTL keeps a down or slow peer from stalling every page load.
	negativeTTL = 60 * time.Second
	maxItems    = 6
	// maxBody bounds the upstream response so a misbehaving peer cannot
	// exhaust memory on a page render.
	maxBody = 1 << 20
)

// Sandwich is one pairing card, self-contained so the template needs no URL
// assembly or formatting helpers.
type Sandwich struct {
	Title       string
	Slug        string
	Description string
	ImageURL    string
	URL         string
	Summary     string
}

type cacheEntry struct {
	items   []Sandwich
	expires time.Time
}

// Client fetches sandwich pairings for a drink slug. A nil *Client is valid
// and reports the feature as disabled.
type Client struct {
	base  string
	http  *http.Client
	mu    sync.Mutex
	cache map[string]cacheEntry
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,119}$`)

// New returns a client for baseURL, or nil when baseURL is empty. A trailing
// slash is trimmed so paths are joined consistently.
func New(baseURL string) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil
	}
	return &Client{
		base:  base,
		http:  telemetry.HTTPClient(&http.Client{Timeout: 3 * time.Second}, "sandwitches"),
		cache: make(map[string]cacheEntry),
	}
}

// Enabled reports whether the feature is configured.
func (c *Client) Enabled() bool { return c != nil && c.base != "" }

// Base returns the Sandwitches origin, or an empty string when disabled. It is
// used for the attribution link on the drink page.
func (c *Client) Base() string {
	if c == nil {
		return ""
	}
	return c.base
}

// ForDrink returns up to six pairings for slug. Errors, an invalid slug and a
// disabled client all yield nil; the drink page simply omits the section.
func (c *Client) ForDrink(ctx context.Context, slug string) []Sandwich {
	if c == nil || slug == "" || !slugPattern.MatchString(slug) {
		return nil
	}

	now := time.Now()
	c.mu.Lock()
	if entry, ok := c.cache[slug]; ok && now.Before(entry.expires) {
		items := cloneItems(entry.items)
		c.mu.Unlock()
		return items
	}
	c.mu.Unlock()

	items := c.fetch(ctx, slug)
	ttl := positiveTTL
	if items == nil {
		ttl = negativeTTL
	}
	c.mu.Lock()
	if c.cache == nil {
		c.cache = make(map[string]cacheEntry)
	}
	c.cache[slug] = cacheEntry{items: cloneItems(items), expires: time.Now().Add(ttl)}
	c.mu.Unlock()

	return items
}

func (c *Client) fetch(ctx context.Context, slug string) []Sandwich {
	u := c.base + "/api/v1/recipes?drink=" + url.QueryEscape(slug) + "&limit=6"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var envelope struct {
		Items []struct {
			ID          int    `json:"id"`
			Title       string `json:"title"`
			Slug        string `json:"slug"`
			Description string `json:"description"`
			Image       string `json:"image"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&envelope); err != nil {
		return nil
	}

	items := make([]Sandwich, 0, len(envelope.Items))
	for _, it := range envelope.Items {
		if len(items) >= maxItems {
			break
		}
		items = append(items, Sandwich{
			Title:       strings.TrimSpace(it.Title),
			Slug:        it.Slug,
			Description: strings.TrimSpace(it.Description),
			ImageURL:    absoluteImage(c.base, it.Image),
			URL:         c.base + "/recipes/" + url.PathEscape(it.Slug),
			Summary:     summarise(it.Description),
		})
	}
	return items
}

// absoluteImage turns a root-relative Sandwitches media path into a full URL.
// Anything empty stays empty so the template can skip the thumbnail.
func absoluteImage(base, image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	if strings.HasPrefix(image, "/") {
		return base + image
	}
	return base + "/" + image
}

// summarise trims a description to roughly 120 characters at a word boundary.
func summarise(description string) string {
	description = strings.TrimSpace(description)
	const limit = 120
	runes := []rune(description)
	if len(runes) <= limit {
		return description
	}
	cut := string(runes[:limit-1])
	if i := strings.LastIndexByte(cut, ' '); i > limit/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \t\n,.;:") + "…"
}

func cloneItems(items []Sandwich) []Sandwich {
	if items == nil {
		return nil
	}
	out := make([]Sandwich, len(items))
	copy(out, items)
	return out
}
