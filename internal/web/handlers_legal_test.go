package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newLegalServer(t *testing.T) *Server {
	t.Helper()
	h := newV12Harness(t)
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	h.srv.Render = renderer
	return h.srv
}

// The policy and the deletion page are what a store reviewer and a signed-out
// user both open first, so they must render without a session and without any
// request-scoped user.
func TestLegalPagesRenderSignedOut(t *testing.T) {
	srv := newLegalServer(t)
	srv.Cfg.LegalControllerName = "Example Soda Cooperative"
	srv.Cfg.LegalContactEmail = "privacy@example.test"
	srv.Cfg.LegalContactAddress = "1 Fizz Street, Amsterdam"
	srv.Cfg.LegalHostingRegion = "the Netherlands"

	cases := []struct {
		name    string
		handler http.HandlerFunc
		path    string
		want    []string
	}{
		{
			name:    "privacy",
			handler: srv.privacyPage,
			path:    "/privacy",
			want: []string{
				"Example Soda Cooperative",
				"privacy@example.test",
				"1 Fizz Street, Amsterdam",
				"the Netherlands",
				"sd_session",
				`href="/account/delete"`,
			},
		},
		{
			name:    "terms",
			handler: srv.termsPage,
			path:    "/terms",
			want:    []string{"Terms of use", "Open Food Facts", `href="/privacy"`},
		},
		{
			name:    "account delete",
			handler: srv.accountDeletePage,
			path:    "/account/delete",
			want:    []string{"Delete your account", "privacy@example.test", "deleted user"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handler(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("body is missing %q", want)
				}
			}
			if strings.Contains(body, "template error") {
				t.Error("template failed to render")
			}
		})
	}
}

// An instance that never configured a controller must still publish a policy
// that reads sensibly, and must not invent a contact address.
func TestPrivacyFallsBackWithoutLegalConfig(t *testing.T) {
	srv := newLegalServer(t)
	rec := httptest.NewRecorder()
	srv.privacyPage(rec, httptest.NewRequest(http.MethodGet, "/privacy", nil))
	body := rec.Body.String()

	if !strings.Contains(body, "The operator of this Sodapop instance") {
		t.Error("missing fallback controller line")
	}
	if strings.Contains(body, "mailto:") {
		t.Error("invented a contact address that was never configured")
	}
	if !strings.Contains(body, "has not published a dedicated privacy address") {
		t.Error("missing the no-contact explanation")
	}
}

// The optional processors are described only when they are actually running, so
// the published policy cannot claim analytics on an instance that has none.
func TestPrivacyOnlyDescribesEnabledProcessors(t *testing.T) {
	srv := newLegalServer(t)
	rec := httptest.NewRecorder()
	srv.privacyPage(rec, httptest.NewRequest(http.MethodGet, "/privacy", nil))
	body := rec.Body.String()

	if !strings.Contains(body, "No analytics service is enabled on this instance") {
		t.Error("analytics section should say analytics is off by default")
	}
	if strings.Contains(body, "OpenTelemetry tracing is enabled") {
		t.Error("claimed tracing without an OTLP endpoint")
	}
}
