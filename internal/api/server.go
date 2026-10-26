package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/swaggest/swgui/v5emb"

	"sodapop/ent"
	"sodapop/internal/ai"
	"sodapop/internal/auth"
	"sodapop/internal/badges"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/comments"
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

// Server exposes the authenticated, deliberately non-enumerable JSON API.
type Server struct {
	Cfg          *config.Config
	Auth         *auth.Service
	Catalog      *catalog.Service
	Checkins     *checkins.Service
	Comments     *comments.Service
	Social       *social.Service
	Venues       *venues.Service
	Mailer       *email.Mailer
	Log          *slog.Logger
	Limiter      *ratelimit.Limiter
	DB           *db.DB
	Media        *media.Service
	AI           *ai.Service
	Integrations *integrations.Service
	Badges       *badges.Service
	Taste        *taste.Service
	Mix          *mix.Service

	routes []string
}

// route registers one method+pattern with the mux and records it for the
// OpenAPI contract test. Only /api/v1 routes are recorded.
func (s *Server) route(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	mux.HandleFunc(pattern, h)
	if strings.Contains(pattern, " /api/v1/") {
		s.routes = append(s.routes, pattern)
	}
}

// APIRoutes returns the registered /api/v1 method+pattern pairs in source
// order. It exists for the contract test, not for request handling.
func (s *Server) APIRoutes() []string {
	return append([]string(nil), s.routes...)
}

type apiFunc func(http.ResponseWriter, *http.Request, *auth.APIToken) error

// Register wires every /api route. There is intentionally no route that lists
// the full catalog, all users or all venues.
func (s *Server) Register(mux *http.ServeMux) {
	s.route(mux, "GET /api/openapi.json", s.openapiJSON)
	s.RegisterGalleryRoutes(mux)
	s.RegisterAIAPI(mux)
	docs := v5emb.New("Sodapop API", "/api/openapi.json", "/api/docs/")
	mux.Handle("GET /api/docs/", docs)
	s.route(mux, "GET /api/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/docs/", http.StatusMovedPermanently)
	})

	s.route(mux, "GET /api/v1/auth/oidc/status", s.oidcStatus)
	s.route(mux, "POST /api/v1/auth/token", s.passwordToken)
	s.route(mux, "POST /api/v1/auth/mobile-token", s.mobileToken)

	s.route(mux, "GET /api/v1/me", s.protect("read", false, s.me))
	s.route(mux, "GET /api/v1/me/checkins", s.protect("read", false, s.myCheckins))
	s.route(mux, "GET /api/v1/stats/me", s.protect("read", false, s.myStats))
	s.route(mux, "GET /api/v1/notifications", s.protect("read", false, s.notifications))
	s.route(mux, "POST /api/v1/notifications/read", s.protect("write", false, s.notificationsRead))
	s.route(mux, "GET /api/v1/checkins/{id}", s.protect("read", false, s.getCheckin))
	s.route(mux, "GET /api/v1/media/{id}", s.protect("read", false, s.mediaFull))
	s.route(mux, "GET /api/v1/media/{id}/thumb", s.protect("read", false, s.mediaThumb))
	s.route(mux, "GET /api/v1/stats/me/insights", s.protect("read", false, s.myInsights))
	s.route(mux, "GET /api/v1/integrations/home-assistant", s.protect("read", false, s.integrationHAStatus))

	s.route(mux, "GET /api/v1/feed", s.protect("feed", false, s.feed))
	s.route(mux, "GET /api/v1/friends", s.protect("feed", false, s.friends))
	s.route(mux, "POST /api/v1/friends/requests", s.protect("write", true, s.requestFriend))
	s.route(mux, "POST /api/v1/friends/requests/{id}/accept", s.protect("write", true, s.acceptFriend))
	s.route(mux, "DELETE /api/v1/friends/{id}", s.protect("write", true, s.removeFriend))

	s.route(mux, "POST /api/v1/checkins", s.protect("write", true, s.createCheckin))
	s.route(mux, "DELETE /api/v1/checkins/{id}", s.protect("write", true, s.deleteCheckin))
	s.route(mux, "POST /api/v1/checkins/{id}/toast", s.protect("write", true, s.toast))
	s.route(mux, "DELETE /api/v1/checkins/{id}/toast", s.protect("write", true, s.untoast))
	s.route(mux, "GET /api/v1/checkins/{id}/comments", s.protect("read", false, s.listComments))
	s.route(mux, "POST /api/v1/checkins/{id}/comments", s.protect("write", true, s.createComment))
	s.route(mux, "DELETE /api/v1/comments/{id}", s.protect("write", true, s.deleteComment))

	s.route(mux, "POST /api/v1/users/{handle}/block", s.protect("write", true, s.blockUser))
	s.route(mux, "DELETE /api/v1/users/{handle}/block", s.protect("write", true, s.unblockUser))
	s.route(mux, "POST /api/v1/users/{handle}/follow", s.protect("write", true, s.followUser))
	s.route(mux, "DELETE /api/v1/users/{handle}/follow", s.protect("write", true, s.unfollowUser))
	s.route(mux, "GET /api/v1/users/{handle}", s.protect("read", false, s.userProfile))
	s.route(mux, "GET /api/v1/users/{handle}/checkins", s.protect("feed", false, s.userCheckins))

	s.route(mux, "GET /api/v1/me/lists", s.protect("read", false, s.myLists))
	s.route(mux, "GET /api/v1/lists/{id}", s.protect("read", false, s.getList))
	s.route(mux, "GET /api/v1/mix", s.protect("read", false, s.myMixes))
	s.route(mux, "POST /api/v1/mix", s.protect("write", true, s.createMix))
	s.route(mux, "GET /api/v1/mix/{id}", s.protect("read", false, s.getMix))
	s.route(mux, "GET /api/v1/taste/twins", s.protect("read", false, s.tasteTwins))
	s.route(mux, "GET /api/v1/taste/recommendations", s.protect("read", false, s.tasteRecommendations))
	s.route(mux, "GET /api/v1/badges", s.protect("read", false, s.badges))
	s.route(mux, "GET /api/v1/passport", s.protect("read", false, s.passport))

	s.route(mux, "GET /api/v1/drinks/{slug}", s.protect("read", false, s.getDrink))
	s.route(mux, "GET /api/v1/search", s.protectStrict("read", false, s.search))
	s.route(mux, "GET /api/v1/barcodes/{gtin}", s.protectStrict("read", false, s.barcode))
	s.route(mux, "POST /api/v1/ai/drafts", s.protectStrict("write", true, s.aiDraft))
	s.route(mux, "POST /api/v1/ai/tasting-note", s.protectStrict("write", true, s.aiTastingNote))
	s.route(mux, "POST /api/v1/ai/recommend", s.protectStrict("read", false, s.aiRecommend))
	s.route(mux, "POST /api/v1/drinks", s.protectStrict("write", true, s.createDrink))

	s.route(mux, "GET /api/v1/venues/search", s.protectStrict("read", false, s.searchVenues))
	s.route(mux, "GET /api/v1/venues/saved", s.protect("read", false, s.savedVenues))
	s.route(mux, "GET /api/v1/venues/{id}", s.protect("read", false, s.getVenue))
	s.route(mux, "POST /api/v1/venues", s.protect("write", true, s.createVenue))
	s.route(mux, "POST /api/v1/venues/{id}/save", s.protect("write", true, s.saveVenue))
	// DELETE /venues/{id}/save and DELETE /venues/media/{id} would conflict as
	// literal patterns, so the shared shape dispatches on the second segment.
	// Keeping both public paths is part of the v1.2 contract.
	s.route(mux, "DELETE /api/v1/venues/{venueID}/{target}", s.protect("write", true, s.venueDelete))
	s.route(mux, "GET /api/v1/venues/{id}/media", s.protect("read", false, s.listVenueMedia))
	s.route(mux, "POST /api/v1/venues/{id}/media", s.protect("write", true, s.uploadVenueMedia))
	s.route(mux, "GET /api/v1/admin/reports/{id}/notes", s.protect("read", false, s.listReportNotes))
	s.route(mux, "POST /api/v1/admin/reports/{id}/notes", s.protect("write", true, s.createReportNote))
	s.route(mux, "DELETE /api/v1/admin/notes/{id}", s.protect("write", true, s.deleteAdminNote))
}

func (s *Server) protect(scope string, verified bool, fn apiFunc) http.HandlerFunc {
	return s.protectWithLimit(scope, verified, "", fn)
}

// protectStrict applies the tighter bucket used for search, barcode lookup and
// community creation. It makes crawling the catalog measurably expensive and
// caps it well below a bulk export.
func (s *Server) protectStrict(scope string, verified bool, fn apiFunc) http.HandlerFunc {
	return s.protectWithLimit(scope, verified, "strict:", fn)
}

func (s *Server) protectWithLimit(scope string, verified bool, prefix string, fn apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, err := s.Auth.AuthenticateToken(r.Context(), r)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || ent.IsNotFound(err) {
				unauthorized(w, "invalid or missing personal API token")
				return
			}
			s.internal(w, err)
			return
		}
		if !tok.HasScope(scope) {
			writeError(w, http.StatusForbidden, "insufficient_scope", "this token does not grant the required scope")
			return
		}
		if verified && tok.User.EmailVerifiedAt == nil {
			writeError(w, http.StatusForbidden, "email_not_verified", "verify your email address before writing")
			return
		}
		key := prefix + tok.Row.ID
		limit, window := s.Cfg.RateLimitPerMin, time.Minute
		if prefix != "" {
			limit = 30
		}
		if !s.Limiter.Allow(key, limit, window) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests; try again shortly")
			return
		}
		if err := fn(w, r, tok); err != nil {
			s.mapError(w, err)
		}
	}
}

func (s *Server) mapError(w http.ResponseWriter, err error) {
	var bre badRequestError
	switch {
	case errors.As(err, &bre):
		writeError(w, http.StatusBadRequest, "invalid_request", bre.msg)
	case errors.Is(err, catalog.ErrQueryTooShort):
		writeError(w, http.StatusBadRequest, "query_too_short", "query must be at least 2 characters")
	case errors.Is(err, catalog.ErrNotFound), errors.Is(err, checkins.ErrNotFound), errors.Is(err, venues.ErrNotFound),
		errors.Is(err, comments.ErrNotFound), errors.Is(err, social.ErrNotFound), errors.Is(err, mix.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, catalog.ErrInvalidBarcode):
		writeError(w, http.StatusBadRequest, "invalid_barcode", "barcode must be a valid GTIN-8/12/13/14")
	case errors.Is(err, catalog.ErrBarcodeTaken):
		writeError(w, http.StatusConflict, "barcode_taken", "barcode already belongs to another drink")
	case errors.Is(err, catalog.ErrOFFRateLimited):
		writeError(w, http.StatusTooManyRequests, "upstream_rate_limited", "product lookup is temporarily rate limited")
	case errors.Is(err, checkins.ErrForbidden), errors.Is(err, social.ErrNotFriend):
		writeError(w, http.StatusForbidden, "forbidden", "you do not have access to this resource")
	case errors.Is(err, checkins.ErrInvalidRating), errors.Is(err, checkins.ErrInvalidInput), errors.Is(err, venues.ErrInvalid),
		errors.Is(err, comments.ErrInvalid), errors.Is(err, venues.ErrInvalidKind), errors.Is(err, venues.ErrInvalidLabel),
		errors.Is(err, mix.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, comments.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "you are commenting too quickly; try again later")
	case errors.Is(err, venues.ErrInvalidMedia):
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "that file must be a JPEG, PNG, WebP or GIF image")
	case errors.Is(err, venues.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "image is too large")
	case errors.Is(err, venues.ErrMediaLimit):
		writeError(w, http.StatusConflict, "media_limit", "this venue has reached its photo and menu limit")
	case errors.Is(err, social.ErrSelfAction):
		writeError(w, http.StatusBadRequest, "self_action", err.Error())
	case errors.Is(err, checkins.ErrIdempotencyUse):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key already used")
	default:
		s.internal(w, err)
	}
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	s.Log.Error("api error", "err", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "something went wrong")
}

func (s *Server) openapiJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	_, _ = w.Write(openAPISpec)
}

func (s *Server) oidcStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": s.Auth.OIDCStatus()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="sodapop"`)
	writeError(w, http.StatusUnauthorized, "unauthorized", message)
}

func queryInt(r *http.Request, key string, def, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

type badRequestError struct{ msg string }

func (e badRequestError) Error() string { return e.msg }

func bad(msg string) error { return badRequestError{msg: msg} }

// isAdmin reports whether the token holder may moderate content. Moderators
// share the removal powers the web layer grants them.
func isAdmin(tok *auth.APIToken) bool {
	return tok != nil && (string(tok.User.Role) == "admin" || string(tok.User.Role) == "moderator")
}
