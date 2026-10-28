package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"sodapop/internal/ai"
	"sodapop/internal/auth"
	"sodapop/internal/backup"
	"sodapop/internal/badges"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/email"
	"sodapop/internal/integrations"
	"sodapop/internal/media"
	"sodapop/internal/mix"
	"sodapop/internal/ratelimit"
	"sodapop/internal/saves"
	"sodapop/internal/settings"
	"sodapop/internal/setup"
	"sodapop/internal/social"
	"sodapop/internal/taste"
	"sodapop/internal/telemetry"
	"sodapop/internal/venues"
)

type userKey struct{}
type sessionKey struct{}

// Server renders the server-side HTML application.
type Server struct {
	Cfg          *config.Config
	Auth         *auth.Service
	Catalog      *catalog.Service
	Checkins     *checkins.Service
	Saves        *saves.Service
	Social       *social.Service
	Venues       *venues.Service
	Badges       *badges.Service
	Taste        *taste.Service
	Mix          *mix.Service
	Media        *media.Service
	AI           *ai.Service
	Integrations *integrations.Service
	DB           *db.DB
	Backup       *backup.Service
	Mailer       *email.Mailer
	Log          *slog.Logger
	Limiter      *ratelimit.Limiter
	Render       *Renderer
	Setup        *setup.Service
	Settings     *settings.Service
}

// Register wires HTML routes and the OIDC redirect endpoints.
func (s *Server) Register(mux *http.ServeMux) {
	mux.Handle("GET /static/", http.StripPrefix("/static/", StaticHandler()))
	s.RegisterGalleryRoutes(mux)

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("GET /register", s.registerPage)
	mux.HandleFunc("POST /register", s.registerSubmit)
	mux.HandleFunc("GET /setup", s.setupPage)
	mux.HandleFunc("POST /setup", s.setupSubmit)
	mux.HandleFunc("GET /verify", s.verify)
	mux.HandleFunc("POST /logout", s.logout)

	mux.HandleFunc("GET /auth/oidc/login", s.Auth.BeginLogin)
	mux.HandleFunc("GET /auth/oidc/callback", s.oidcCallback)
	mux.HandleFunc("POST /auth/oidc/logout", s.logout)

	mux.HandleFunc("GET /search", s.requireUser(s.searchPage))
	mux.HandleFunc("GET /drinks/{slug}", s.requireUser(s.drinkPage))
	mux.HandleFunc("GET /scan", s.requireVerified(s.scanPage))
	mux.HandleFunc("GET /checkins/new", s.requireVerified(s.checkinPage))
	mux.HandleFunc("POST /checkins", s.requireVerified(s.checkinSubmit))
	mux.HandleFunc("POST /checkins/{id}/toast", s.requireVerified(s.checkinToast))
	mux.HandleFunc("POST /checkins/{id}/untoast", s.requireVerified(s.untoast))
	mux.HandleFunc("POST /checkins/{id}/save", s.requireVerified(s.saveCheckin))
	mux.HandleFunc("POST /checkins/{id}/unsave", s.requireVerified(s.unsaveCheckin))
	mux.HandleFunc("GET /saved", s.requireUser(s.savedPage))
	mux.HandleFunc("POST /checkins/{id}/comments", s.requireVerified(s.commentCreate))
	mux.HandleFunc("POST /comments/{id}/delete", s.requireUser(s.commentDelete))
	mux.HandleFunc("GET /compare", s.requireUser(s.comparePage))
	mux.HandleFunc("GET /new", s.requireVerified(s.newDrinkPage))
	mux.HandleFunc("POST /new", s.requireVerified(s.newDrinkSubmit))
	mux.HandleFunc("GET /venues", s.requireUser(s.venuesPage))
	mux.HandleFunc("GET /venues/saved", s.requireUser(s.venuesSavedPage))
	mux.HandleFunc("POST /venues", s.requireVerified(s.venueCreate))
	mux.HandleFunc("GET /venues/{id}", s.requireUser(s.venuePage))
	mux.HandleFunc("POST /venues/{id}/save", s.requireVerified(s.venueSave))
	mux.HandleFunc("POST /venues/{id}/unsave", s.requireVerified(s.venueUnsave))
	mux.HandleFunc("POST /venues/{id}/media", s.requireVerified(s.venueMediaUpload))
	mux.HandleFunc("POST /venues/media/{id}/delete", s.requireVerified(s.venueMediaDelete))
	mux.HandleFunc("GET /sw.js", s.serviceWorker)
	mux.HandleFunc("GET /manifest.webmanifest", s.webManifest)
	mux.HandleFunc("GET /friends", s.requireUser(s.friendsPage))
	mux.HandleFunc("POST /friends/request", s.requireUser(s.friendRequest))
	mux.HandleFunc("POST /friends/{id}/accept", s.requireUser(s.friendAccept))
	mux.HandleFunc("POST /friends/{id}/remove", s.requireUser(s.friendRemove))
	mux.HandleFunc("GET /settings/tokens", s.requireUser(s.tokensPage))
	mux.HandleFunc("GET /settings/blocked", s.requireUser(s.settingsBlockedPage))
	mux.HandleFunc("POST /settings/tokens", s.requireUser(s.tokensCreate))
	mux.HandleFunc("GET /you", s.requireUser(s.youPage))
	mux.HandleFunc("GET /lists", s.requireUser(s.listsPage))
	mux.HandleFunc("POST /lists", s.requireUser(s.listCreate))
	mux.HandleFunc("POST /lists/add", s.requireUser(s.listAdd))
	mux.HandleFunc("GET /lists/{id}", s.requireUser(s.listPage))
	mux.HandleFunc("POST /lists/{id}/edit", s.requireUser(s.listEdit))
	mux.HandleFunc("POST /lists/{id}/delete", s.requireUser(s.listDelete))
	mux.HandleFunc("POST /lists/{id}/add", s.requireUser(s.listAdd))
	mux.HandleFunc("POST /lists/{id}/items/{drink}/remove", s.requireUser(s.listRemove))
	mux.HandleFunc("GET /passport", s.requireUser(s.passportPage))
	mux.HandleFunc("GET /taste", s.requireUser(s.tastePage))
	mux.HandleFunc("GET /mix", s.requireUser(s.mixPage))
	mux.HandleFunc("POST /mix", s.requireVerified(s.mixCreate))
	mux.HandleFunc("GET /mix/{slug}", s.requireUser(s.mixDetailPage))
	mux.HandleFunc("POST /reports", s.requireUser(s.reportSubmit))

	mux.HandleFunc("GET /media/{id}", s.mediaServe)
	mux.HandleFunc("GET /media/{id}/thumb", s.mediaThumb)
	mux.HandleFunc("GET /media/{id}/poster", s.mediaThumb)
	mux.HandleFunc("POST /ai/polish", s.requireVerified(s.aiPolish))
	mux.HandleFunc("POST /ai/draft", s.requireVerified(s.aiDraft))

	mux.HandleFunc("GET /integrations", s.requireUser(s.integrationsPage))
	mux.HandleFunc("POST /integrations", s.requireUser(s.integrationSave))
	mux.HandleFunc("POST /integrations/{kind}/test", s.requireUser(s.integrationTest))
	mux.HandleFunc("POST /push/subscribe", s.requireUser(s.pushSubscribe))
	mux.HandleFunc("POST /push/unsubscribe", s.requireUser(s.pushUnsubscribe))
	mux.HandleFunc("GET /export.json", s.requireUser(s.exportJSON))
	mux.HandleFunc("POST /import.csv", s.requireUser(s.importCSV))
	mux.HandleFunc("GET /feeds/{token}", s.rssFeed)

	mux.HandleFunc("GET /notifications", s.requireUser(s.notificationsPage))
	mux.HandleFunc("POST /notifications/read", s.requireUser(s.notificationsRead))
	mux.HandleFunc("GET /checkins/{id}", s.checkinDetailPage)
	mux.HandleFunc("POST /checkins/{id}/delete", s.requireVerified(s.checkinDelete))
	mux.HandleFunc("GET /settings", s.requireUser(s.settingsPage))
	mux.HandleFunc("POST /settings/profile", s.requireUser(s.settingsProfile))
	mux.HandleFunc("POST /settings/preferences", s.requireUser(s.settingsPreferences))
	mux.HandleFunc("POST /settings/privacy", s.requireUser(s.settingsPrivacy))
	mux.HandleFunc("POST /settings/password", s.requireUser(s.settingsPassword))
	mux.HandleFunc("POST /settings/avatar", s.requireUser(s.settingsAvatar))
	mux.HandleFunc("POST /settings/sessions/{id}/revoke", s.requireUser(s.settingsSessionRevoke))
	mux.HandleFunc("POST /settings/logout-all", s.requireUser(s.settingsLogoutAll))
	mux.HandleFunc("POST /settings/delete", s.requireUser(s.settingsDelete))
	mux.HandleFunc("GET /forgot", s.forgotPage)
	mux.HandleFunc("POST /forgot", s.forgotSubmit)
	mux.HandleFunc("GET /reset/{token}", s.resetPage)
	mux.HandleFunc("POST /reset/{token}", s.resetSubmit)
	mux.HandleFunc("GET /drinks", s.requireUser(s.browseDrinksPage))
	mux.HandleFunc("GET /brands", s.requireUser(s.browseBrandsPage))
	mux.HandleFunc("GET /brands/{slug}", s.requireUser(s.brandPage))
	mux.HandleFunc("GET /styles", s.requireUser(s.browseStylesPage))
	mux.HandleFunc("GET /styles/{slug}", s.requireUser(s.stylePage))
	mux.HandleFunc("GET /drops", s.requireUser(s.dropsPage))
	mux.HandleFunc("GET /u/{handle}", s.profilePage)
	mux.HandleFunc("POST /u/{handle}/follow", s.requireVerified(s.followUser))
	mux.HandleFunc("POST /u/{handle}/unfollow", s.requireVerified(s.unfollowUser))
	mux.HandleFunc("POST /u/{handle}/block", s.requireUser(s.blockUser))
	mux.HandleFunc("POST /u/{handle}/unblock", s.requireUser(s.unblockUser))
	mux.HandleFunc("GET /insights", s.requireUser(s.insightsPage))
	mux.HandleFunc("POST /admin/drinks/{id}/status", s.requireAdmin(s.adminDrinkStatus))
	mux.HandleFunc("POST /integrations/ha-token", s.requireUser(s.haTokenCreate))
	mux.HandleFunc("POST /integrations/ha-token/{id}/revoke", s.requireUser(s.haTokenRevoke))
	mux.HandleFunc("POST /hooks/ha/{token}", s.haInbound)

	mux.HandleFunc("GET /admin", s.requireAdmin(s.adminPage))
	mux.HandleFunc("GET /admin/settings", s.requireAdminStrict(s.adminSettingsPage))
	mux.HandleFunc("POST /admin/settings/general", s.requireAdminStrict(s.adminSettingsGeneral))
	mux.HandleFunc("POST /admin/settings/email", s.requireAdminStrict(s.adminSettingsEmail))
	mux.HandleFunc("POST /admin/settings/email/test", s.requireAdminStrict(s.adminSettingsEmailTest))
	mux.HandleFunc("POST /admin/settings/analytics", s.requireAdminStrict(s.adminSettingsAnalytics))
	mux.HandleFunc("POST /admin/settings/access", s.requireAdminStrict(s.adminSettingsAccess))
	mux.HandleFunc("POST /admin/settings/ai", s.requireAdminStrict(s.adminSettingsAI))
	mux.HandleFunc("POST /admin/settings/observability", s.requireAdminStrict(s.adminSettingsObservability))
	mux.HandleFunc("POST /admin/ai", s.requireAdmin(s.adminAISave))
	mux.HandleFunc("POST /admin/ai/{id}/toggle", s.requireAdmin(s.adminAIToggle))
	mux.HandleFunc("POST /admin/ai/{id}/test", s.requireAdmin(s.adminAITest))
	mux.HandleFunc("POST /admin/reports/{id}/resolve", s.requireAdmin(s.adminReportResolve))
	mux.HandleFunc("POST /admin/reports/{id}/dismiss", s.requireAdmin(s.adminReportDismiss))
	mux.HandleFunc("POST /admin/reports/{id}/remove", s.requireAdmin(s.adminReportRemove))
	mux.HandleFunc("POST /admin/reports/{id}/notes", s.requireAdmin(s.adminReportNoteCreate))
	mux.HandleFunc("POST /admin/notes/{id}/delete", s.requireAdmin(s.adminNoteDelete))
	mux.HandleFunc("POST /admin/drinks/{id}/activate", s.requireAdmin(s.adminDrinkActivate))
	mux.HandleFunc("POST /admin/digest", s.requireAdmin(s.adminDigest))
	mux.HandleFunc("GET /admin/digest", s.requireAdminStrict(s.adminDigestPage))
	mux.HandleFunc("GET /admin/system", s.requireAdminStrict(s.adminSystemPage))
	mux.HandleFunc("POST /admin/backup", s.requireAdminStrict(s.adminBackupCreate))
	mux.HandleFunc("GET /admin/badges", s.requireAdminStrict(s.adminBadgesPage))
	mux.HandleFunc("POST /admin/badges", s.requireAdminStrict(s.adminBadgeCreate))
	mux.HandleFunc("POST /admin/badges/{id}/edit", s.requireAdminStrict(s.adminBadgeUpdate))
	mux.HandleFunc("POST /admin/badges/{id}/delete", s.requireAdminStrict(s.adminBadgeDelete))
	mux.HandleFunc("GET /admin/venues", s.requireAdminStrict(s.adminVenuesPage))
	mux.HandleFunc("POST /admin/venues/merge", s.requireAdminStrict(s.adminVenueMerge))
}

// Middleware attaches the session and applies security headers. It also wraps
// the shared web/API mux with HTTP metrics exactly once.
func (s *Server) Middleware(next http.Handler) http.Handler {
	return telemetry.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if sess, u, err := s.Auth.CurrentSession(ctx, r); err == nil {
			ctx = context.WithValue(ctx, userKey{}, u)
			ctx = context.WithValue(ctx, sessionKey{}, sess)
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		} else {
			csp := "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; worker-src 'self' blob:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
			if umami := s.umami(); umami.URL != "" {
				csp = strings.Replace(csp, "script-src 'self'", "script-src 'self' "+umami.URL, 1)
				csp = strings.Replace(csp, "connect-src 'self'", "connect-src 'self' "+umami.URL, 1)
			}
			w.Header().Set("Content-Security-Policy", csp)
		}
		if !indexablePath(r.URL.Path) {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}

// indexablePath reports whether a route may be indexed. Public catalog pages and
// opt-in profiles opt in; everything else stays noindex.
func indexablePath(p string) bool {
	return p == "/" || strings.HasPrefix(p, "/u/")
}

func (s *Server) base(r *http.Request) map[string]any {
	umami := s.umami()
	d := map[string]any{
		"AppName":          s.siteName(),
		"Env":              s.Cfg.Env,
		"Version":          s.Cfg.Version,
		"UmamiURL":         umami.URL,
		"UmamiID":          umami.WebsiteID,
		"OIDC":             s.Auth.OIDCStatus(),
		"AssetScript":      uiAssets.Script,
		"Year":             time.Now().Year(),
		"NavUnread":        0,
		"Theme":            "system",
		"RegistrationOpen": s.registrationOpen(),
	}
	if u := UserFromContext(r); u != nil {
		d["User"] = u
		n, _ := s.Social.UnreadCount(r.Context(), u.ID)
		d["NavUnread"] = n
		if prefs, err := s.Auth.GetOrCreateSettings(r.Context(), u.ID); err == nil {
			if prefs.Theme != "" {
				d["Theme"] = prefs.Theme
			}
			d["Units"] = string(prefs.Units)
		}
		if u.AnalyticsOptOut {
			// The opt-out removes the analytics script entirely.
			d["UmamiURL"] = ""
			d["UmamiID"] = ""
		}
	}
	if sess := SessionFromContext(r); sess != nil {
		d["CSRF"] = sess.CsrfToken
	}
	return d
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page string, extra map[string]any) {
	d := s.base(r)
	for k, v := range extra {
		d[k] = v
	}
	s.Render.Render(w, status, page, d)
}

func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r) == nil {
			http.Redirect(w, r, "/login?next="+r.URL.RequestURI(), http.StatusFound)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireVerified(next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r)
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if u.EmailVerifiedAt == nil {
			s.render(w, r, http.StatusForbidden, "verify", map[string]any{
				"Title":   "Verify your email",
				"Pending": false,
				"Error":   "Confirm your email address before logging sodas.",
			})
			return
		}
		next(w, r)
	})
}

func (s *Server) csrfOK(w http.ResponseWriter, r *http.Request) bool {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return false
	}
	if !auth.ValidCSRF(r, SessionFromContext(r)) {
		http.Error(w, "CSRF check failed", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if err := s.Auth.CompleteLogin(r.Context(), w, r); err != nil {
		s.Log.Warn("oidc login failed", "err", err)
		s.render(w, r, http.StatusUnauthorized, "login", map[string]any{
			"Title": "Sign in",
			"Error": "Single sign-on did not complete. Please try again.",
		})
	}
}
