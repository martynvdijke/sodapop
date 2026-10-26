package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestOpenAPIContract keeps the served document honest: every registered
// /api/v1 method+pattern has a matching documented operation, every documented
// operation is registered (except the intentional shared venue-delete shape),
// and every $ref resolves.
func TestOpenAPIContract(t *testing.T) {
	h := newHarness(t)

	var doc any
	if err := json.Unmarshal(openAPISpec, &doc); err != nil {
		t.Fatalf("openapi: %v", err)
	}
	root, _ := doc.(map[string]any)
	paths, _ := root["paths"].(map[string]any)

	documented := map[string]bool{}
	aliases := map[string][]string{
		"DELETE /api/v1/venues/{venueID}/{target}": {
			"DELETE /api/v1/venues/{id}/save",
			"DELETE /api/v1/venues/media/{id}",
		},
	}
	aliasTargets := map[string]bool{}
	for _, targets := range aliases {
		for _, target := range targets {
			aliasTargets[target] = true
		}
	}
	httpMethods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true, "head": true, "options": true}

	for path, raw := range paths {
		if !strings.HasPrefix(path, "/api/v1/") {
			continue
		}
		ops, _ := raw.(map[string]any)
		for method, opRaw := range ops {
			if !httpMethods[method] {
				continue
			}
			key := strings.ToUpper(method) + " " + path
			documented[key] = true
			op, _ := opRaw.(map[string]any)
			tags, _ := op["tags"].([]any)
			if len(tags) == 0 {
				t.Errorf("%s: operation has no tags", key)
			}
		}
	}

	registered := map[string]bool{}
	for _, route := range h.server.APIRoutes() {
		registered[route] = true
		keys := []string{route}
		if mapped, ok := aliases[route]; ok {
			keys = mapped
		}
		for _, key := range keys {
			if !documented[key] {
				t.Errorf("registered route %s is not documented as %s", route, key)
			}
		}
	}
	for key := range documented {
		if registered[key] || aliasTargets[key] {
			continue
		}
		t.Errorf("documented operation %s is not registered", key)
	}

	walkRefs(t, doc, root, "#")
}

func walkRefs(t *testing.T, node any, root map[string]any, where string) {
	t.Helper()
	switch v := node.(type) {
	case map[string]any:
		if raw, ok := v["$ref"].(string); ok {
			if !strings.HasPrefix(raw, "#/") {
				t.Errorf("%s: unsupported external $ref %q", where, raw)
			} else if _, ok := resolvePointer(root, raw); !ok {
				t.Errorf("%s: unresolved $ref %q", where, raw)
			}
		}
		for key, child := range v {
			walkRefs(t, child, root, where+"/"+key)
		}
	case []any:
		for i, child := range v {
			walkRefs(t, child, root, where+"/"+string(rune('0'+i)))
		}
	}
}

func resolvePointer(root map[string]any, ref string) (any, bool) {
	var cur any = root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func TestNewRoutesRequireAuth(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/me/lists"},
		{http.MethodGet, "/api/v1/lists/abc"},
		{http.MethodGet, "/api/v1/mix"},
		{http.MethodGet, "/api/v1/mix/1"},
		{http.MethodPost, "/api/v1/mix"},
		{http.MethodGet, "/api/v1/taste/twins"},
		{http.MethodGet, "/api/v1/taste/recommendations"},
		{http.MethodGet, "/api/v1/badges"},
		{http.MethodGet, "/api/v1/passport"},
		{http.MethodGet, "/api/v1/venues/saved"},
		{http.MethodPost, "/api/v1/users/bob/follow"},
		{http.MethodDelete, "/api/v1/users/bob/follow"},
	} {
		rec := h.do(t, tc.method, tc.path, "", false)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}
