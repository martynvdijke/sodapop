package web

import (
	"net/http"
	"strings"

	"sodapop/internal/settings"
)

// settingsSnapshot returns the current immutable runtime settings snapshot, or
// nil when no settings service is wired (tests and legacy construction).
func (s *Server) settingsSnapshot() *settings.Snapshot {
	if s == nil || s.Settings == nil {
		return nil
	}
	return s.Settings.Snapshot()
}

func (s *Server) siteName() string {
	if snap := s.settingsSnapshot(); snap != nil {
		return snap.SiteName()
	}
	if s.Cfg != nil && s.Cfg.AppName != "" {
		return s.Cfg.AppName
	}
	return "Sodapop"
}

func (s *Server) baseURL() string {
	if snap := s.settingsSnapshot(); snap != nil {
		return snap.BaseURL()
	}
	if s.Cfg != nil {
		return strings.TrimRight(s.Cfg.BaseURL, "/")
	}
	return ""
}

func (s *Server) umami() settings.UmamiSettings {
	if snap := s.settingsSnapshot(); snap != nil {
		return snap.Umami()
	}
	if s.Cfg != nil {
		return settings.UmamiSettings{URL: s.Cfg.UmamiURL, WebsiteID: s.Cfg.UmamiWebsiteID}
	}
	return settings.UmamiSettings{}
}

func (s *Server) passwordFallback() bool {
	if snap := s.settingsSnapshot(); snap != nil {
		return snap.AllowPasswordFallback()
	}
	if s.Cfg != nil {
		return s.Cfg.PasswordFallback
	}
	return true
}

func (s *Server) registrationOpen() bool {
	if snap := s.settingsSnapshot(); snap != nil {
		return snap.RegistrationOpen()
	}
	return true
}

// emailAllowed applies the effective ALLOWED_EMAILS / ALLOWED_DOMAINS lists.
func (s *Server) emailAllowed(email string) bool {
	if snap := s.settingsSnapshot(); snap != nil {
		emails, domains := snap.AllowedEmails(), snap.AllowedDomains()
		if len(emails) == 0 && len(domains) == 0 {
			return true
		}
		email = strings.ToLower(strings.TrimSpace(email))
		for _, allowed := range emails {
			if strings.EqualFold(allowed, email) {
				return true
			}
		}
		domain := ""
		if i := strings.LastIndexByte(email, '@'); i >= 0 {
			domain = email[i+1:]
		}
		for _, allowed := range domains {
			if strings.EqualFold(strings.TrimPrefix(allowed, "@"), domain) {
				return true
			}
		}
		return false
	}
	if s.Cfg != nil {
		return s.Cfg.EmailAllowed(email)
	}
	return true
}

var settingsNotices = map[string]string{
	"general_saved":   "General settings saved.",
	"email_saved":     "Email settings saved.",
	"test_sent":       "Test email sent.",
	"test_sink":       "No SMTP host is configured, so the test message was written to the development mail sink.",
	"analytics_saved": "Analytics settings saved.",
	"access_saved":    "Access settings saved.",
	"ai_saved":        "AI settings saved.",
	"otel_saved":      "Observability settings saved. Restart the server to apply them.",
}

// adminSettingsPage renders the admin settings panel from the current runtime
// snapshot. It never reads database rows directly and never reveals secrets.
func (s *Server) adminSettingsPage(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	if s.Settings.Snapshot() == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	notice := ""
	if code := r.URL.Query().Get("notice"); code != "" {
		notice = settingsNotices[code]
	}
	s.renderAdminSettings(w, r, http.StatusOK, "", notice)
}

func (s *Server) renderAdminSettings(w http.ResponseWriter, r *http.Request, status int, errMsg, notice string) {
	snap := s.Settings.Snapshot()
	if snap == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	smtp := snap.SMTP()
	umami := snap.Umami()
	passwordSet, passwordLast4 := snap.SecretState(settings.KeySMTPPassword)
	s.render(w, r, status, "admin_settings", map[string]any{
		"Title":             "Settings",
		"SiteName":          snap.SiteName(),
		"BaseURL":           snap.BaseURL(),
		"BaseURLSource":     snap.Source(settings.KeyBaseURL),
		"SMTPHost":          smtp.Host,
		"SMTPPort":          smtp.Port,
		"SMTPUsername":      smtp.Username,
		"SMTPFrom":          smtp.From,
		"SMTPTLS":           smtp.TLS,
		"SMTPSource":        snap.Source(settings.KeySMTPHost),
		"SMTPPasswordSet":   passwordSet,
		"SMTPPasswordLast4": passwordLast4,
		"UmamiURL":          umami.URL,
		"UmamiWebsiteID":    umami.WebsiteID,
		"UmamiSource":       snap.Source(settings.KeyUmamiURL),
		"RegistrationOpen":  snap.RegistrationOpen(),
		"PasswordFallback":  snap.AllowPasswordFallback(),
		"AllowedEmails":     strings.Join(snap.AllowedEmails(), ", "),
		"AllowedDomains":    strings.Join(snap.AllowedDomains(), ", "),
		"AccessSource":      snap.Source(settings.KeyRegistrationOpen),
		"AIEnabled":         snap.AIEnabled(),
		"AISource":          snap.Source(settings.KeyAIEnabled),
		"OTELDisabled":      snap.OTELDisabled(),
		"OTELEndpoint":      snap.OTELEndpoint(),
		"OTELServiceName":   snap.OTELServiceName(),
		"OTELSampler":       snap.OTELSampler(),
		"OTELSource":        snap.Source(settings.KeyOTELEndpoint),
		// OpenTelemetry is configured at startup, so any saved change only
		// applies after a restart.
		"OTELRestartRequired": true,
		"Notice":              notice,
		"Error":               errMsg,
	})
}

// validateSettings checks every non-secret form field before the first write so
// a bad value cannot leave a multi-key form half-saved.
func validateSettings(fields map[string]string) error {
	for key, value := range fields {
		if err := settings.Validate(key, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) settingsUpdatedBy(r *http.Request) string {
	if u := UserFromContext(r); u != nil {
		return u.ID
	}
	return ""
}

func (s *Server) adminSettingsGeneral(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.Settings == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	fields := map[string]string{
		settings.KeyAppName: strings.TrimSpace(r.FormValue("site_name")),
		settings.KeyBaseURL: strings.TrimSpace(r.FormValue("base_url")),
	}
	if err := validateSettings(fields); err != nil {
		s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	actor := s.settingsUpdatedBy(r)
	for _, key := range []string{settings.KeyAppName, settings.KeyBaseURL} {
		if err := s.Settings.Set(r.Context(), key, fields[key], false, actor); err != nil {
			s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
	}
	http.Redirect(w, r, "/admin/settings?notice=general_saved", http.StatusSeeOther)
}

func (s *Server) adminSettingsEmail(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.Settings == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	tls := strings.TrimSpace(r.FormValue("smtp_tls"))
	if tls == "" {
		// A form that omits the select keeps the currently saved mode.
		tls = "starttls"
		if snap := s.Settings.Snapshot(); snap != nil {
			tls = snap.SMTP().TLS
		}
	}
	fields := map[string]string{
		settings.KeySMTPHost:     strings.TrimSpace(r.FormValue("smtp_host")),
		settings.KeySMTPPort:     strings.TrimSpace(r.FormValue("smtp_port")),
		settings.KeySMTPUsername: strings.TrimSpace(r.FormValue("smtp_username")),
		settings.KeySMTPFrom:     strings.TrimSpace(r.FormValue("smtp_from")),
		settings.KeySMTPTLS:      tls,
	}
	if err := validateSettings(fields); err != nil {
		s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	actor := s.settingsUpdatedBy(r)
	for _, key := range []string{settings.KeySMTPHost, settings.KeySMTPPort, settings.KeySMTPUsername, settings.KeySMTPFrom, settings.KeySMTPTLS} {
		if err := s.Settings.Set(r.Context(), key, fields[key], false, actor); err != nil {
			s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
	}
	if r.FormValue("clear_smtp_password") == "1" {
		if err := s.Settings.Delete(r.Context(), settings.KeySMTPPassword, actor); err != nil {
			s.renderAdminSettings(w, r, http.StatusBadRequest, "The SMTP password could not be cleared.", "")
			return
		}
	}
	if password := r.FormValue("smtp_password"); strings.TrimSpace(password) != "" {
		if err := s.Settings.Set(r.Context(), settings.KeySMTPPassword, password, true, actor); err != nil {
			s.renderAdminSettings(w, r, http.StatusBadRequest, "The SMTP password could not be saved.", "")
			return
		}
	}
	http.Redirect(w, r, "/admin/settings?notice=email_saved", http.StatusSeeOther)
}

func (s *Server) adminSettingsEmailTest(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.Settings == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	u := UserFromContext(r)
	if u == nil || strings.TrimSpace(u.Email) == "" {
		s.renderAdminSettings(w, r, http.StatusBadRequest, "Your account has no email address to send to.", "")
		return
	}
	if s.Mailer == nil {
		s.renderAdminSettings(w, r, http.StatusServiceUnavailable, "Email is not configured.", "")
		return
	}
	if err := s.Mailer.SendTest(r.Context(), u.Email); err != nil {
		s.Log.Warn("admin test email failed", "err", err)
		s.renderAdminSettings(w, r, http.StatusBadGateway, "The test email could not be sent. Check the SMTP settings.", "")
		return
	}
	notice := "test_sent"
	if snap := s.Settings.Snapshot(); snap != nil && strings.TrimSpace(snap.SMTP().Host) == "" {
		notice = "test_sink"
	}
	http.Redirect(w, r, "/admin/settings?notice="+notice, http.StatusSeeOther)
}

func (s *Server) adminSettingsAnalytics(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.Settings == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	fields := map[string]string{
		settings.KeyUmamiURL:       strings.TrimSpace(r.FormValue("umami_url")),
		settings.KeyUmamiWebsiteID: strings.TrimSpace(r.FormValue("umami_website_id")),
	}
	if err := validateSettings(fields); err != nil {
		s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	actor := s.settingsUpdatedBy(r)
	for _, key := range []string{settings.KeyUmamiURL, settings.KeyUmamiWebsiteID} {
		if err := s.Settings.Set(r.Context(), key, fields[key], false, actor); err != nil {
			s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
	}
	http.Redirect(w, r, "/admin/settings?notice=analytics_saved", http.StatusSeeOther)
}

func (s *Server) adminSettingsAccess(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.Settings == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	fields := map[string]string{
		settings.KeyRegistrationOpen:      boolField(r.FormValue("registration_open") != ""),
		settings.KeyAllowPasswordFallback: boolField(r.FormValue("password_fallback") != ""),
		settings.KeyAllowedEmails:         strings.TrimSpace(r.FormValue("allowed_emails")),
		settings.KeyAllowedDomains:        strings.TrimSpace(r.FormValue("allowed_domains")),
	}
	if err := validateSettings(fields); err != nil {
		s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	actor := s.settingsUpdatedBy(r)
	for _, key := range []string{settings.KeyRegistrationOpen, settings.KeyAllowPasswordFallback, settings.KeyAllowedEmails, settings.KeyAllowedDomains} {
		if err := s.Settings.Set(r.Context(), key, fields[key], false, actor); err != nil {
			s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
	}
	http.Redirect(w, r, "/admin/settings?notice=access_saved", http.StatusSeeOther)
}

func (s *Server) adminSettingsAI(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.Settings == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	value := boolField(r.FormValue("ai_enabled") != "")
	if err := validateSettings(map[string]string{settings.KeyAIEnabled: value}); err != nil {
		s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	if err := s.Settings.Set(r.Context(), settings.KeyAIEnabled, value, false, s.settingsUpdatedBy(r)); err != nil {
		s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	http.Redirect(w, r, "/admin/settings?notice=ai_saved", http.StatusSeeOther)
}

func (s *Server) adminSettingsObservability(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.Settings == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	fields := map[string]string{
		settings.KeyOTELEndpoint:    strings.TrimSpace(r.FormValue("otel_endpoint")),
		settings.KeyOTELServiceName: strings.TrimSpace(r.FormValue("otel_service_name")),
		settings.KeyOTELSampler:     strings.TrimSpace(r.FormValue("otel_sampler")),
		settings.KeyOTELDisabled:    boolField(r.FormValue("otel_disabled") != ""),
	}
	if err := validateSettings(fields); err != nil {
		s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	actor := s.settingsUpdatedBy(r)
	for _, key := range []string{settings.KeyOTELEndpoint, settings.KeyOTELServiceName, settings.KeyOTELSampler, settings.KeyOTELDisabled} {
		if err := s.Settings.Set(r.Context(), key, fields[key], false, actor); err != nil {
			s.renderAdminSettings(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
	}
	http.Redirect(w, r, "/admin/settings?notice=otel_saved", http.StatusSeeOther)
}

func boolField(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
