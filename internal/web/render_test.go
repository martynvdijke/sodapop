package web

import "testing"

// TestTemplatesParse guards every page template at build time. A malformed
// template otherwise only surfaces as a 500 on the first request.
func TestTemplatesParse(t *testing.T) {
	if _, err := NewRenderer(); err != nil {
		t.Fatalf("parse templates: %v", err)
	}
}

func TestRendererKnowsEveryPage(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	pages := []string{
		"home", "login", "register", "verify", "search", "drink", "checkin",
		"friends", "tokens", "newdrink", "error", "you", "lists", "list",
		"passport", "taste", "mix", "mix_detail", "integrations", "admin",
		"venues", "venue", "notifications", "checkin_detail", "settings",
		"forgot", "reset", "browse", "brand", "style", "profile", "insights", "drops", "venues_saved", "compare",
		"settings_blocked", "setup", "admin_settings", "admin_deliveries", "share",
	}
	for _, page := range pages {
		if _, ok := r.pages[page]; !ok {
			t.Errorf("renderer missing page %q", page)
		}
	}
}
