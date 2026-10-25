package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"sodapop/internal/ai"
	"sodapop/internal/auth"
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
	"sodapop/internal/social"
	"sodapop/internal/taste"
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
	Social       *social.Service
	Venues       *venues.Service
	Badges       *badges.Service
	Taste        *taste.Service
	Mix          *mix.Service
	Media        *media.Service
	AI           *ai.Service
	Integrations *integrations.Service
	DB           *db.DB
	Mailer       *email.Mailer
	Log          *slog.Logger
	Limiter      *ratelimit.Limiter
	Render       *Renderer
}

// Register wires HTML routes and the OIDC redirect endpoints.
func (s *Server) Register(mux *http.ServeMux) {
	mux.Handle("GET /static/", http.StripPrefix("/static/", StaticHandler()))

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("GET /register", s.registerPage)
	mux.HandleFunc("POST /register", s.registerSubmit)
	mux.HandleFunc("GET /verify", s.verify)
	mux.HandleFunc("POST /logout", s.logout)

	mux.HandleFunc("GET /auth/oidc/login", s.Auth.BeginLogin)
	mux.HandleFunc("GET /auth/oidc/callback", s.oidcCallback)
	mux.HandleFunc("POST /auth/oidc/logout", s.logout)

	mux.HandleFunc("GET /search", s.requireUser(s.searchPage))
	mux.HandleFunc("GET /drinks/{slug}", s.requireUser(s.drinkPage))
	mux.HandleFunc("GET /checkins/new", s.requireVerified(s.checkinPage))
	mux.HandleFunc("POST /checkins", s.requireVerified(s.checkinSubmit))
	mux.HandleFunc("POST /checkins/{id}/toast", s.requireVerified(s.checkinToast))
	mux.HandleFunc("GET /new", s.requireVerified(s.newDrinkPage))
	mux.HandleFunc("POST /new", s.requireVerified(s.newDrinkSubmit))
	mux.HandleFunc("GET /venues", s.requireUser(s.venuesPage))
	mux.HandleFunc("POST /venues", s.requireVerified(s.venueCreate))
	mux.HandleFunc("GET /venues/{id}", s.requireUser(s.venuePage))
	mux.HandleFunc("POST /venues/{id}/save", s.requireVerified(s.venueSave))
	mux.HandleFunc("GET /sw.js", s.serviceWorker)
	mux.HandleFunc("GET /manifest.webmanifest", s.webManifest)
	mux.HandleFunc("GET /friends", s.requireUser(s.friendsPage))
	mux.HandleFunc("POST /friends/request", s.requireUser(s.friendRequest))
	mux.HandleFunc("POST /friends/{id}/accept", s.requireUser(s.friendAccept))
	mux.HandleFunc("POST /friends/{id}/remove", s.requireUser(s.friendRemove))
	mux.HandleFunc("GET /settings/tokens", s.requireUser(s.tokensPage))
	mux.HandleFunc("POST /settings/tokens", s.requireUser(s.tokensCreate))
	mux.HandleFunc("GET /you", s.requireUser(s.youPage))
	mux.HandleFunc("GET /lists", s.requireUser(s.listsPage))
	mux.HandleFunc("POST /lists", s.requireUser(s.listCreate))
	mux.HandleFunc("POST /lists/add", s.requireUser(s.listAdd))
	mux.HandleFunc("GET /lists/{id}", s.requireUser(s.listPage))
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

	mux.HandleFunc("GET /admin", s.requireAdmin(s.adminPage))
	mux.HandleFunc("POST /admin/ai", s.requireAdmin(s.adminAISave))
	mux.HandleFunc("POST /admin/ai/{id}/toggle", s.requireAdmin(s.adminAIToggle))
	mux.HandleFunc("POST /admin/ai/{id}/test", s.requireAdmin(s.adminAITest))
	mux.HandleFunc("POST /admin/reports/{id}/resolve", s.requireAdmin(s.adminReportResolve))
	mux.HandleFunc("POST /admin/reports/{id}/dismiss", s.requireAdmin(s.adminReportDismiss))
	mux.HandleFunc("POST /admin/drinks/{id}/activate", s.requireAdmin(s.adminDrinkActivate))
	mux.HandleFunc("POST /admin/digest", s.requireAdmin(s.adminDigest))
}

// Middleware attaches the session and applies security headers.
func (s *Server) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			csp := "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; worker-src 'self' blob:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
			if s.Cfg.UmamiURL != "" {
				csp = strings.Replace(csp, "script-src 'self'", "script-src 'self' "+s.Cfg.UmamiURL, 1)
				csp = strings.Replace(csp, "connect-src 'self'", "connect-src 'self' "+s.Cfg.UmamiURL, 1)
			}
			w.Header().Set("Content-Security-Policy", csp)
		}
		if r.URL.Path != "/" {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) base(r *http.Request) map[string]any {
	d := map[string]any{
		"AppName":     s.Cfg.AppName,
		"Env":         s.Cfg.Env,
		"Version":     s.Cfg.Version,
		"UmamiURL":    s.Cfg.UmamiURL,
		"UmamiID":     s.Cfg.UmamiWebsiteID,
		"OIDC":        s.Auth.OIDCStatus(),
		"AssetScript": uiAssets.Script,
		"Year":        time.Now().Year(),
	}
	if u := UserFromContext(r); u != nil {
		d["User"] = u
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
