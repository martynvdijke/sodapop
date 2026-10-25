package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"sodapop/internal/auth"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/config"
	"sodapop/internal/email"
	"sodapop/internal/ratelimit"
	"sodapop/internal/social"
	"sodapop/internal/venues"
)

type userKey struct{}
type sessionKey struct{}

// Server renders the server-side HTML application.
type Server struct {
	Cfg      *config.Config
	Auth     *auth.Service
	Catalog  *catalog.Service
	Checkins *checkins.Service
	Social   *social.Service
	Venues   *venues.Service
	Mailer   *email.Mailer
	Log      *slog.Logger
	Limiter  *ratelimit.Limiter
	Render   *Renderer
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
	mux.HandleFunc("GET /friends", s.requireUser(s.friendsPage))
	mux.HandleFunc("POST /friends/request", s.requireUser(s.friendRequest))
	mux.HandleFunc("POST /friends/{id}/accept", s.requireUser(s.friendAccept))
	mux.HandleFunc("POST /friends/{id}/remove", s.requireUser(s.friendRemove))
	mux.HandleFunc("GET /settings/tokens", s.requireUser(s.tokensPage))
	mux.HandleFunc("POST /settings/tokens", s.requireUser(s.tokensCreate))
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
			csp := "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
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
		"AppName":  s.Cfg.AppName,
		"Env":      s.Cfg.Env,
		"Version":  s.Cfg.Version,
		"UmamiURL": s.Cfg.UmamiURL,
		"UmamiID":  s.Cfg.UmamiWebsiteID,
		"OIDC":     s.Auth.OIDCStatus(),
		"Year":     time.Now().Year(),
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
