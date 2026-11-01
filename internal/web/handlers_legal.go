package web

import (
	"net/http"
	"strings"

	"sodapop/internal/auth"
)

// LegalData is the identity of the instance operator plus the deployment facts
// the published policy is allowed to assert. Every field is either
// configuration or read from the live settings snapshot, so the text cannot
// claim something the running server does not do.
type LegalData struct {
	Controller     string
	ContactEmail   string
	ContactAddress string
	HostingRegion  string
	BaseURL        string
	SessionCookie  string
	Analytics      bool
	AnalyticsURL   string
	Traces         bool
	Mail           bool
	Push           bool
	OIDC           bool
}

// legal collects the policy facts once per request.
func (s *Server) legal() LegalData {
	name := s.siteName()
	d := LegalData{
		Controller:    "The operator of this " + name + " instance",
		HostingRegion: "the European Union",
		SessionCookie: auth.SessionCookie,
		AnalyticsURL:  s.umami().URL,
		Analytics:     s.umami().URL != "",
		Traces:        false,
		Mail:          false,
		Push:          false,
		OIDC:          false,
	}
	if s.Cfg != nil {
		d.BaseURL = s.Cfg.BaseURL
		if v := strings.TrimSpace(s.Cfg.LegalControllerName); v != "" {
			d.Controller = v
		}
		d.ContactEmail = s.Cfg.LegalContactEmail
		d.ContactAddress = s.Cfg.LegalContactAddress
		if v := strings.TrimSpace(s.Cfg.LegalHostingRegion); v != "" {
			d.HostingRegion = v
		}
		if s.Cfg.OIDCEnabled {
			d.OIDC = true
		}
		if s.Cfg.FCMServiceAccountJSON != "" {
			d.Push = true
		}
	}
	if snap := s.settingsSnapshot(); snap != nil {
		d.Traces = snap.OTELEndpoint() != ""
		d.Mail = snap.SMTP().Host != ""
		if snap.VAPID().Enabled() {
			d.Push = true
		}
		if s.Auth != nil {
			if st := s.Auth.OIDCStatus(); st["enabled"] == true {
				d.OIDC = true
			}
		}
	}
	return d
}

// privacyPage serves the public privacy policy. It stays reachable without a
// session because both app stores and data-protection law require the policy to
// be readable before anyone signs up.
func (s *Server) privacyPage(w http.ResponseWriter, r *http.Request) {
	d := s.base(r)
	d["Title"] = "Privacy"
	d["MetaDescription"] = "What this instance stores, why, who it is shared with, and how to get it back or delete it."
	d["Legal"] = s.legal()
	s.Render.Render(w, http.StatusOK, "privacy", d)
}

// termsPage serves the acceptable-use terms for the same reason.
func (s *Server) termsPage(w http.ResponseWriter, r *http.Request) {
	d := s.base(r)
	d["Title"] = "Terms"
	d["MetaDescription"] = "The rules for using this instance, and what you keep ownership of."
	d["Legal"] = s.legal()
	s.Render.Render(w, http.StatusOK, "terms", d)
}

// accountDeletePage is the deletion route Google Play asks every app with
// accounts to publish. It is readable while signed out, because the store
// review and a user who cannot get back into their account both need to reach
// it without a session.
func (s *Server) accountDeletePage(w http.ResponseWriter, r *http.Request) {
	d := s.base(r)
	d["Title"] = "Delete your account"
	d["MetaDescription"] = "How to delete a Sodapop account and what happens to the data afterwards."
	d["Legal"] = s.legal()
	s.Render.Render(w, http.StatusOK, "account_delete", d)
}
