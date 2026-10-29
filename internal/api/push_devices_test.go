package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestPushDeviceRoutesRequireAuth(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/me/push/devices"},
		{http.MethodDelete, "/api/v1/me/push/devices"},
	} {
		rec := h.do(t, tc.method, tc.path, `{"token":"`+strings.Repeat("a", 40)+`"}`, false)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without auth = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestPushDeviceRegisterAndUnregister(t *testing.T) {
	h := newHarness(t)
	token := strings.Repeat("device-token-", 4)

	rec := h.do(t, http.MethodPost, "/api/v1/me/push/devices",
		`{"token":"`+token+`","platform":"android"}`, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("register = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	// Re-registering is idempotent.
	rec = h.do(t, http.MethodPost, "/api/v1/me/push/devices",
		`{"token":"`+token+`","platform":"android"}`, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("re-register = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	rec = h.do(t, http.MethodDelete, "/api/v1/me/push/devices", `{"token":"`+token+`"}`, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unregister = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodDelete, "/api/v1/me/push/devices", `{"token":"`+token+`"}`, true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("repeat unregister = %d, want 404", rec.Code)
	}
}

func TestPushDeviceRegisterValidation(t *testing.T) {
	h := newHarness(t)
	for _, body := range []string{
		`{"token":"short","platform":"android"}`,
		`{"token":"` + strings.Repeat("a", 40) + `","platform":"ios"}`,
		`{"token":"` + strings.Repeat("a", 40) + `"}`,
		`{"token":`,
		`{"unknown":"` + strings.Repeat("a", 40) + `"}`,
	} {
		rec := h.do(t, http.MethodPost, "/api/v1/me/push/devices", body, true)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("register %q = %d, want 400", body, rec.Code)
		}
	}
}

func TestPushDeviceValidationDoesNotEchoInternalErrors(t *testing.T) {
	h := newHarness(t)
	for _, body := range []string{
		`{"token":"junk!token","platform":"android"}`,
		`{"token":"` + strings.Repeat("a", 40) + `","platform":"ios"}`,
	} {
		rec := h.do(t, http.MethodPost, "/api/v1/me/push/devices", body, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("register %q = %d, want 400", body, rec.Code)
		}
		errBody := decodeBody(t, rec)
		detail, _ := errBody["error"].(map[string]any)
		message, _ := detail["message"].(string)
		if message != invalidPushDeviceMessage {
			t.Errorf("message = %q, want %q", message, invalidPushDeviceMessage)
		}
		if strings.Contains(message, "device token") || strings.Contains(message, "platform") {
			t.Errorf("message leaked internal validation text: %q", message)
		}
	}
}

func TestPushDeviceUnregisterIsScopedToCaller(t *testing.T) {
	h := newHarness(t)
	token := strings.Repeat("device-token-", 4)
	if rec := h.do(t, http.MethodPost, "/api/v1/me/push/devices",
		`{"token":"`+token+`","platform":"android"}`, true); rec.Code != http.StatusNoContent {
		t.Fatalf("register = %d", rec.Code)
	}

	bobToken := h.tokenFor(t, h.bob)
	rec := h.doWith(t, bobToken, http.MethodDelete, "/api/v1/me/push/devices", `{"token":"`+token+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unregister another user's device = %d, want 404", rec.Code)
	}
}
