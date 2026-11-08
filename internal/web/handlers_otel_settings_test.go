package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sodapop/internal/settings"
)

// TestOTELSettingsRouteIsPublicAndReflectsSettings checks the route the mobile
// app calls before sign-in: no session, no token, just the endpoint and flags.
func TestOTELSettingsRouteIsPublicAndReflectsSettings(t *testing.T) {
	h := newSettingsHarness(t)
	mux := http.NewServeMux()
	h.srv.Register(mux)

	get := func() (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/otel-settings", nil))
		var body map[string]any
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", rec.Body.String(), err)
			}
		}
		return rec, body
	}

	// With no endpoint configured everything stays off.
	rec, body := get()
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if body["endpoint"] != "" || body["enabled"] != false || body["traces_enabled"] != false {
		t.Fatalf("default payload = %v", body)
	}
	if body["metrics_enabled"] != false || body["logs_enabled"] != false {
		t.Fatalf("metrics/logs must stay false: %v", body)
	}
	if len(body) != 5 {
		t.Fatalf("payload keys = %v, want exactly endpoint/enabled/traces/metrics/logs", body)
	}

	// A saved endpoint enables traces without any authentication.
	h.set(t, settings.KeyOTELEndpoint, "https://otel.example.test/v1/traces", false, "")
	h.set(t, settings.KeyOTELServiceName, "sodapop-web", false, "")

	rec, body = get()
	if rec.Code != http.StatusOK {
		t.Fatalf("configured status = %d, want 200", rec.Code)
	}
	if body["endpoint"] != "https://otel.example.test/v1/traces" {
		t.Fatalf("endpoint = %v", body["endpoint"])
	}
	if body["enabled"] != true || body["traces_enabled"] != true {
		t.Fatalf("enabled/traces = %v/%v, want true/true", body["enabled"], body["traces_enabled"])
	}
	if _, leaked := body["service_name"]; leaked {
		t.Fatal("service name should not be published on the public route")
	}

	// The global kill switch wins even with an endpoint configured.
	h.set(t, settings.KeyOTELDisabled, "1", false, "")
	_, body = get()
	if body["enabled"] != false || body["traces_enabled"] != false {
		t.Fatalf("disabled payload = %v, want enabled=false", body)
	}
	if body["endpoint"] != "https://otel.example.test/v1/traces" {
		t.Fatalf("disabled payload dropped the endpoint: %v", body["endpoint"])
	}
}

// TestOTELSettingsRouteWithoutSettingsServiceDegradesToDisabled keeps the
// route safe during legacy construction and tests, when no snapshot exists.
func TestOTELSettingsRouteWithoutSettingsServiceDegradesToDisabled(t *testing.T) {
	h := newV12Harness(t)
	mux := http.NewServeMux()
	h.srv.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/otel-settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["endpoint"] != "" || body["enabled"] != false || body["traces_enabled"] != false {
		t.Fatalf("unconfigured payload = %v", body)
	}
}
