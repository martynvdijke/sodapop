package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

// otelSettingsJSON is the public, unauthenticated view of the observability
// settings the mobile app needs to start exporting traces. It exposes only the
// endpoint and the on/off flags: never credentials, headers or sampler detail,
// because anyone can read it.
func (s *Server) otelSettingsJSON(w http.ResponseWriter, r *http.Request) {
	endpoint := ""
	enabled := false
	if snap := s.settingsSnapshot(); snap != nil {
		endpoint = strings.TrimSpace(snap.OTELEndpoint())
		enabled = !snap.OTELDisabled() && endpoint != ""
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"endpoint":        endpoint,
		"enabled":         enabled,
		"traces_enabled":  enabled,
		"metrics_enabled": false,
		"logs_enabled":    false,
	})
}
