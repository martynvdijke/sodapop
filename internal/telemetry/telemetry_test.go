package telemetry

import (
	"net/http/httptest"
	"testing"
)

func TestTraceableSkipsCapabilityPaths(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/healthz", false},
		{"/readyz", false},
		{"/metrics", false},
		{"/share/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", false},
		{"/share/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/card.png", false},
		{"/share", true},
		{"/passport", true},
		{"/shareholder", true},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		if got := traceable(req); got != tt.want {
			t.Errorf("traceable(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
