package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/auth"
	"sodapop/internal/checkins"
	"sodapop/internal/overview"
)

// The TRMNL endpoints return the exact shapes four e-ink Liquid views render.
// They intentionally return a full object with zero values instead of null
// arrays, so a screen never has to guard against missing keys.

const trmnlNoStore = "private, no-store"

func setNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", trmnlNoStore)
}

// trmnlCheckins handles GET /api/v1/trmnl/checkins. Public, no auth; it shares
// the preview feed's IP bucket.
func (s *Server) trmnlCheckins(w http.ResponseWriter, r *http.Request) {
	if s.publicPreviewLimited(w, r, "feed") {
		return
	}
	limit := previewLimit(r, 10, 20)
	handle := strings.TrimSpace(r.URL.Query().Get("handle"))

	scope := "feed"
	heading := "Recent Pours"
	var items []checkins.ProfileItem
	var err error
	if handle != "" {
		scope = "handle"
		heading = "@" + handle
		items, err = s.Checkins.PublicUserCheckins(r.Context(), handle, limit, "")
		if err != nil {
			if errors.Is(err, checkins.ErrNotFound) {
				setPreviewCache(w)
				writeJSON(w, http.StatusOK, map[string]any{
					"error":              "profile not available",
					"scope":              scope,
					"heading":            heading,
					"items":              []map[string]any{},
					"item_count":         0,
					"latest":             "",
					"total_toasts":       0,
					"total_toasts_label": trmnlToastsLabel(0),
					"best_rating":        "—",
				})
				return
			}
			s.mapError(w, err)
			return
		}
	} else {
		items, err = s.Checkins.PublicFeed(r.Context(), checkins.PublicFeedFilter{Limit: limit})
		if err != nil {
			s.mapError(w, err)
			return
		}
	}

	out := make([]map[string]any, 0, len(items))
	totalToasts := 0
	var best *int
	for _, it := range items {
		when, clock := trmnlRelative(it.CreatedAt)
		var photo any
		if it.PhotoID != "" {
			photo = s.trmnlAbsolute("/api/v1/public/media/" + it.PhotoID + "/thumb")
		}
		totalToasts += it.Toasts
		if it.Rating != nil && (best == nil || *it.Rating > *best) {
			v := *it.Rating
			best = &v
		}
		out = append(out, map[string]any{
			"handle":         it.Handle,
			"display_name":   it.DisplayName,
			"drink":          it.DrinkName,
			"brand":          it.Brand,
			"rating_display": trmnlRatingDisplay(it.Rating),
			"rating_pct":     trmnlRatingPct(it.Rating),
			"note":           trmnlOneLine(it.Note, 140),
			"toasts":         it.Toasts,
			"toasts_label":   trmnlToastsLabel(it.Toasts),
			"venue":          it.VenueName,
			"when":           when,
			"time":           clock,
			"photo_url":      photo,
			"href":           s.trmnlAbsolute("/checkins/" + it.ID),
		})
	}

	latest := ""
	if len(items) > 0 {
		when, clock := trmnlRelative(items[0].CreatedAt)
		latest = strings.TrimSpace(when + " · " + clock)
	}

	setPreviewCache(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"scope":              scope,
		"heading":            heading,
		"items":              out,
		"item_count":         len(out),
		"latest":             latest,
		"total_toasts":       totalToasts,
		"total_toasts_label": trmnlToastsLabel(totalToasts),
		"best_rating":        trmnlRatingDisplay(best),
	})
}

// trmnlNewDrinks handles GET /api/v1/trmnl/new-drinks. Admin/moderator only.
func (s *Server) trmnlNewDrinks(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !isAdmin(tok) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	setNoStore(w)

	scope := trmnlScope(r.URL.Query().Get("scope"))
	ov := overview.New(s.DB.SQL)
	counts, err := ov.Counts(r.Context())
	if err != nil {
		return err
	}
	events, _, err := ov.List(r.Context(), overview.Filter{Limit: 24})
	if err != nil {
		return err
	}

	added := 0
	pending := 0
	switch scope {
	case "drinks":
		added = counts.Drinks24h
		pending = counts.PendingDrinks
	case "users":
		added = counts.Users7d
	case "all":
		added = counts.Checkins24h
	}

	items := make([]map[string]any, 0, len(events))
	for _, e := range events {
		if !trmnlWantedKind(e.Kind, scope) {
			continue
		}
		when, _ := trmnlRelative(e.CreatedAt)
		items = append(items, map[string]any{
			"title":        e.Title,
			"detail":       trmnlOneLine(e.Detail, 120),
			"status":       e.Status,
			"actor_handle": e.ActorHandle,
			"when":         when,
			"href":         s.trmnlAbsolute(e.Href),
		})
	}

	latest := ""
	if len(items) > 0 {
		latest = items[0]["when"].(string)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"scope":        scope,
		"scope_label":  trmnlScopeLabel(scope),
		"window_label": trmnlWindowLabel(scope),
		"added_24h":    added,
		"pending":      pending,
		"items":        items,
		"item_count":   len(items),
		"latest":       latest,
	})
	return nil
}

// trmnlAdmin handles GET /api/v1/trmnl/admin. Admin/moderator only.
func (s *Server) trmnlAdmin(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	if !isAdmin(tok) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return nil
	}
	setNoStore(w)

	ov := overview.New(s.DB.SQL)
	counts, err := ov.Counts(r.Context())
	if err != nil {
		return err
	}
	reports, _, err := ov.List(r.Context(), overview.Filter{Kinds: []string{"report"}, Limit: 6})
	if err != nil {
		return err
	}
	events, _, err := ov.List(r.Context(), overview.Filter{Limit: 6})
	if err != nil {
		return err
	}

	reportItems := make([]map[string]any, 0, len(reports))
	for _, e := range reports {
		reportItems = append(reportItems, trmnlEventItem(s, e))
	}
	eventItems := make([]map[string]any, 0, len(events))
	for _, e := range events {
		eventItems = append(eventItems, trmnlEventItem(s, e))
	}

	needsAttention := counts.OpenReports > 0 || counts.PendingDrinks > 0
	parts := make([]string, 0, 2)
	if counts.OpenReports > 0 {
		parts = append(parts, fmt.Sprintf("%d %s open", counts.OpenReports, plural(counts.OpenReports, "report", "reports")))
	}
	if counts.PendingDrinks > 0 {
		parts = append(parts, fmt.Sprintf("%d %s pending", counts.PendingDrinks, plural(counts.PendingDrinks, "drink", "drinks")))
	}
	banner := "All clear"
	if len(parts) > 0 {
		banner = strings.Join(parts, " · ")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"counts": map[string]any{
			"checkins_24h":   counts.Checkins24h,
			"drinks_24h":     counts.Drinks24h,
			"users_7d":       counts.Users7d,
			"open_reports":   counts.OpenReports,
			"pending_drinks": counts.PendingDrinks,
		},
		"reports":         reportItems,
		"report_count":    len(reportItems),
		"events":          eventItems,
		"event_count":     len(eventItems),
		"needs_attention": needsAttention,
		"banner":          banner,
	})
	return nil
}

// trmnlMe handles GET /api/v1/trmnl/me. Token owner only.
func (s *Server) trmnlMe(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	setNoStore(w)

	ctx := r.Context()
	stats, err := s.Checkins.Stats(ctx, tok.User.ID)
	if err != nil {
		return err
	}
	u, err := s.Auth.Client.User.Get(ctx, tok.User.ID)
	if err != nil {
		return err
	}
	recent, err := s.Checkins.ListForUser(ctx, tok.User.ID, tok.User.ID, 4, "")
	if err != nil {
		return err
	}
	notifs, err := s.Social.Notifications(ctx, tok.User.ID, 4)
	if err != nil {
		return err
	}
	unread, err := s.Social.UnreadCount(ctx, tok.User.ID)
	if err != nil {
		return err
	}

	actorHandles := s.trmnlActorHandles(ctx, notifs)

	recentItems := make([]map[string]any, 0, len(recent))
	for _, it := range recent {
		when, _ := trmnlRelative(it.CreatedAt)
		recentItems = append(recentItems, map[string]any{
			"drink_name":     it.DrinkName,
			"brand":          it.Brand,
			"rating_display": trmnlRatingDisplay(it.Rating),
			"when":           when,
			"venue_name":     it.VenueName,
			// Member media needs the bearer token, which an e-ink screen
			// cannot send. Keep the key, never the URL.
			"photo_url": nil,
			"href":      s.trmnlAbsolute("/checkins/" + it.ID),
		})
	}

	notifItems := make([]map[string]any, 0, len(notifs))
	for _, n := range notifs {
		when, _ := trmnlRelative(n.CreatedAt)
		handle := ""
		if n.ActorID != nil {
			handle = actorHandles[*n.ActorID]
		}
		notifItems = append(notifItems, map[string]any{
			"kind":         string(n.Kind),
			"text":         trmnlOneLine(n.Text, 90),
			"when":         when,
			"actor_handle": handle,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"handle":             u.Handle,
		"display_name":       u.DisplayName,
		"pours":              stats.Checkins,
		"unique_drinks":      stats.UniqueDrinks,
		"badges":             stats.Badges,
		"toasts_received":    stats.Toasts,
		"avg_rating_display": trmnlRatingDisplayFloat(stats.AvgRating),
		"current_streak":     stats.CurrentStreak,
		"longest_streak":     stats.LongestStreak,
		"unread":             unread,
		"has_notifications":  unread > 0 || len(notifItems) > 0,
		"notifications":      notifItems,
		"recent":             recentItems,
	})
	return nil
}

func (s *Server) trmnlActorHandles(ctx context.Context, notifs []*ent.Notification) map[string]string {
	out := map[string]string{}
	ids := make([]string, 0, len(notifs))
	seen := map[string]bool{}
	for _, n := range notifs {
		if n.ActorID != nil && *n.ActorID != "" && !seen[*n.ActorID] {
			seen[*n.ActorID] = true
			ids = append(ids, *n.ActorID)
		}
	}
	if len(ids) == 0 {
		return out
	}
	users, err := s.Auth.Client.User.Query().Where(user.IDIn(ids...)).All(ctx)
	if err != nil {
		return out
	}
	for _, u := range users {
		out[u.ID] = u.Handle
	}
	return out
}

func trmnlEventItem(s *Server, e overview.Event) map[string]any {
	when, _ := trmnlRelative(e.CreatedAt)
	return map[string]any{
		"title":        e.Title,
		"detail":       trmnlOneLine(e.Detail, 120),
		"status":       e.Status,
		"actor_handle": e.ActorHandle,
		"when":         when,
		"href":         s.trmnlAbsolute(e.Href),
		"kind":         e.Kind,
	}
}

func trmnlScope(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "drink", "drinks":
		return "drinks"
	case "venue", "venues":
		return "venues"
	case "user", "users", "member", "members":
		return "users"
	case "all", "everything":
		return "all"
	default:
		return "drinks"
	}
}

func trmnlToastsLabel(n int) string {
	return fmt.Sprintf("%d %s", n, plural(n, "toast", "toasts"))
}

// trmnlWindowLabel names the counting window that added_24h covers: members
// are counted over 7 days, every other scope over 24 hours.
func trmnlWindowLabel(scope string) string {
	if scope == "users" {
		return "7d"
	}
	return "24h"
}

func trmnlScopeLabel(scope string) string {
	switch scope {
	case "venues":
		return "Venues"
	case "users":
		return "Members"
	case "all":
		return "Everything"
	default:
		return "Drinks"
	}
}

func trmnlWantedKind(kind, scope string) bool {
	switch scope {
	case "drinks":
		return kind == "drink"
	case "venues":
		return kind == "venue"
	case "users":
		return kind == "user"
	default:
		return kind != "toast"
	}
}

func (s *Server) trmnlBaseURL() string {
	if s.Integrations != nil {
		if b := s.Integrations.BaseURL(); b != "" {
			return b
		}
	}
	if s.Cfg != nil {
		return strings.TrimRight(s.Cfg.BaseURL, "/")
	}
	return ""
}

func (s *Server) trmnlAbsolute(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if strings.HasPrefix(path, "/") {
		return s.trmnlBaseURL() + path
	}
	return ""
}

// trmnlRelative formats an RFC3339 UTC timestamp as a short relative label and
// an HH:MM clock. An unparseable or zero timestamp becomes an em dash.
func trmnlRelative(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—", "—"
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "—", "—"
	}
	t = t.UTC()
	delta := time.Since(t).Seconds()
	if delta < 0 {
		delta = 0
	}
	var when string
	switch {
	case delta < 60:
		when = "now"
	case delta < 3600:
		when = strconv.Itoa(int(delta/60)) + "m ago"
	case delta < 86400:
		when = strconv.Itoa(int(delta/3600)) + "h ago"
	case delta < 7*86400:
		when = strconv.Itoa(int(delta/86400)) + "d ago"
	default:
		when = t.Format("Jan 2")
	}
	return when, t.Format("15:04")
}

// trmnlOneLine collapses whitespace and truncates to limit runes.
func trmnlOneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) > limit {
		r = r[:limit]
	}
	return strings.TrimSpace(string(r))
}

func trmnlRatingDisplay(rating *int) string {
	if rating == nil {
		return "—"
	}
	return trmnlRatingDisplayFloat(ptrFloat(*rating))
}

func trmnlRatingDisplayFloat(rating *float64) string {
	if rating == nil {
		return "—"
	}
	text := strconv.FormatFloat(*rating/4, 'f', 2, 64)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" {
		return "0"
	}
	return text
}

func trmnlRatingPct(rating *int) int {
	if rating == nil {
		return 0
	}
	pct := int(math.Round(float64(*rating) / 20 * 100))
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func ptrFloat(v int) *float64 {
	f := float64(v)
	return &f
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
