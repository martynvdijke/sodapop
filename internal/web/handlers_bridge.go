package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/catalogrevision"
	"sodapop/ent/drink"
	"sodapop/ent/report"
	"sodapop/internal/ai"
	"sodapop/internal/auth"
)

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r)
		if u.Role != "admin" && u.Role != "moderator" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (s *Server) mediaServe(w http.ResponseWriter, r *http.Request) {
	s.serveMedia(w, r, false)
}

func (s *Server) mediaThumb(w http.ResponseWriter, r *http.Request) {
	s.serveMedia(w, r, true)
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request, thumb bool) {
	id := r.PathValue("id")
	viewer := ""
	if u := UserFromContext(r); u != nil {
		viewer = u.ID
	}
	ok, err := s.Media.CanView(r.Context(), viewer, id)
	if err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	var rc io.ReadCloser
	var row *mediaRow
	if thumb {
		f, m, err := s.Media.OpenThumb(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		rc, row = f, &mediaRow{mime: m.Mime, path: m.ThumbPath}
	} else {
		f, m, err := s.Media.Open(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		rc, row = f, &mediaRow{mime: m.Mime, path: m.Path}
	}
	defer rc.Close()
	if row.mime != "" {
		w.Header().Set("Content-Type", row.mime)
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Length", "")
	_, _ = io.Copy(w, rc)
}

type mediaRow struct {
	mime string
	path string
}

func (s *Server) aiPolish(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.AI == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "ai_unavailable")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeJSONError(w, http.StatusBadRequest, "empty_text")
		return
	}
	out, err := s.AI.TastingNote(r.Context(), text)
	if err != nil {
		s.Log.Warn("ai polish failed", "err", err)
		writeJSONError(w, http.StatusServiceUnavailable, "ai_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"note": out})
}

func (s *Server) aiDraft(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.AI == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "ai_unavailable")
		return
	}
	var body struct {
		Notes     string `json:"notes"`
		Barcode   string `json:"barcode"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	draft, message, sessionID, err := s.AI.DraftDrink(r.Context(), body.Notes, body.Barcode, body.SessionID)
	if err != nil {
		s.Log.Warn("ai draft failed", "err", err)
		writeJSONError(w, http.StatusServiceUnavailable, "ai_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft, "message": message, "session_id": sessionID})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": code}})
}

// ---- Integrations -------------------------------------------------------

func (s *Server) integrationsPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	rows, _ := s.Integrations.List(r.Context(), u.ID)
	byKind := map[string]bool{}
	targets := map[string]string{}
	for _, row := range rows {
		byKind[string(row.Kind)] = row.Enabled
		targets[string(row.Kind)] = row.Target
	}
	feedURL := ""
	if byKind["trmnl"] {
		if u, err := s.Integrations.RSSURL(r.Context(), u.ID); err == nil {
			feedURL = u
		}
	}
	inboundTokens := []map[string]any{}
	if rows, err := s.Integrations.ListInboundTokens(r.Context(), u.ID); err == nil {
		for _, row := range rows {
			lastUsed := ""
			if row.LastUsedAt != nil {
				lastUsed = *row.LastUsedAt
			}
			inboundTokens = append(inboundTokens, map[string]any{
				"ID":         row.ID,
				"Label":      row.Label,
				"CreatedAt":  row.CreatedAt,
				"LastUsedAt": lastUsed,
				"Revoked":    row.RevokedAt != nil,
			})
		}
	}
	newToken := ""
	if c, err := r.Cookie("sd_ha_token"); err == nil && c.Value != "" {
		newToken = c.Value
		http.SetCookie(w, &http.Cookie{
			Name:     "sd_ha_token",
			Value:    "",
			Path:     "/integrations",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   s.Cfg.Env == "production",
		})
	}
	s.render(w, r, http.StatusOK, "integrations", map[string]any{
		"Title":           "Integrations",
		"HomeAssistant":   map[string]any{"Enabled": byKind["home_assistant"], "Target": targets["home_assistant"]},
		"Matrix":          map[string]any{"Enabled": byKind["matrix"], "Target": targets["matrix"]},
		"TRMNL":           map[string]any{"Enabled": byKind["trmnl"], "Target": targets["trmnl"]},
		"FeedURL":         feedURL,
		"FeedToken":       strings.TrimPrefix(feedURL, s.Cfg.BaseURL+"/feeds/"),
		"BaseURL":         s.Cfg.BaseURL,
		"VAPIDPublicKey":  os.Getenv("VAPID_PUBLIC_KEY"),
		"InboundTokens":   inboundTokens,
		"NewInboundToken": newToken,
	})
}

func (s *Server) integrationSave(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	kind := r.FormValue("kind")
	enabled := r.FormValue("enabled") != "" && r.FormValue("enabled") != "0"
	if err := s.Integrations.Save(r.Context(), u.ID, kind, strings.TrimSpace(r.FormValue("target")), strings.TrimSpace(r.FormValue("secret")), enabled); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/integrations", http.StatusFound)
}

func (s *Server) integrationTest(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	var err error
	switch r.PathValue("kind") {
	case "home_assistant":
		err = s.Integrations.HomeAssistant(r.Context(), u.ID)
	case "matrix":
		err = s.Integrations.Matrix(r.Context(), u.ID)
	case "trmnl":
		err = s.Integrations.TRMNL(r.Context(), u.ID)
	default:
		err = errors.New("unsupported integration")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, "/integrations", http.StatusFound)
}

func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.Integrations == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "push_unavailable")
		return
	}
	if !auth.ValidCSRF(r, SessionFromContext(r)) {
		writeJSONError(w, http.StatusForbidden, "csrf")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		P256DH   string `json:"p256dh"`
		Auth     string `json:"auth"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	u := UserFromContext(r)
	if err := s.Integrations.SubscribePush(r.Context(), u.ID, body.Endpoint, body.P256DH, body.Auth); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_subscription")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if !auth.ValidCSRF(r, SessionFromContext(r)) {
		writeJSONError(w, http.StatusForbidden, "csrf")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&body)
	if err := s.Integrations.UnsubscribePush(r.Context(), UserFromContext(r).ID, body.Endpoint); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_subscription")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) exportJSON(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	raw, err := s.Integrations.ExportJSON(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="sodapop-export.json"`)
	_, _ = w.Write(raw)
}

func (s *Server) importCSV(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "choose a CSV file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	n, err := s.Integrations.ImportCSV(r.Context(), u.ID, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.render(w, r, http.StatusOK, "integrations", map[string]any{
		"Title": "Integrations", "Notice": strconv.Itoa(n) + " check-ins imported.",
		"BaseURL": s.Cfg.BaseURL, "VAPIDPublicKey": os.Getenv("VAPID_PUBLIC_KEY"),
	})
}

func (s *Server) rssFeed(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(r.PathValue("token"), ".xml")
	raw, err := s.Integrations.RSS(r.Context(), token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(raw)
}

// ---- Admin --------------------------------------------------------------

type adminReportView struct {
	ID         int
	Entity     string
	EntityID   int
	Reason     string
	Details    string
	Status     string
	CreatedAt  string
	Reporter   string
	EntitySlug string
}

type adminDrinkView struct {
	ID        int
	Slug      string
	Name      string
	Brand     string
	CreatedAt string
}

type adminRevisionView struct {
	Entity    string
	EntityID  int
	Action    string
	User      string
	CreatedAt string
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	reports, _ := s.Auth.Client.Report.Query().
		Where(report.StatusEQ(report.StatusOpen)).
		Order(report.ByCreatedAt()).Limit(50).All(ctx)
	reportViews := make([]adminReportView, 0, len(reports))
	for _, row := range reports {
		v := adminReportView{ID: row.ID, Entity: string(row.Entity), EntityID: row.EntityID,
			Reason: row.Reason, Details: row.Details, Status: string(row.Status), CreatedAt: row.CreatedAt, Reporter: "deleted user"}
		if row.UserID != nil {
			if u, err := s.Auth.Client.User.Get(ctx, *row.UserID); err == nil {
				v.Reporter = "@" + u.Handle
			}
		}
		if row.Entity == "drink" {
			if d, err := s.Auth.Client.Drink.Get(ctx, row.EntityID); err == nil {
				v.EntitySlug = d.Slug
			}
		}
		reportViews = append(reportViews, v)
	}
	provisional, _ := s.Auth.Client.Drink.Query().
		Where(drink.StatusEQ(drink.StatusProvisional)).WithBrand().
		Order(drink.ByCreatedAt()).Limit(50).All(ctx)
	drinkViews := make([]adminDrinkView, 0, len(provisional))
	for _, row := range provisional {
		brand := ""
		if row.Edges.Brand != nil {
			brand = row.Edges.Brand.Name
		}
		drinkViews = append(drinkViews, adminDrinkView{ID: row.ID, Slug: row.Slug, Name: row.Name, Brand: brand, CreatedAt: row.CreatedAt})
	}
	users, _ := s.Auth.Client.User.Query().Count(ctx)
	endpoints, _ := s.AI.ListEndpoints(ctx)
	revisions, _ := s.Auth.Client.CatalogRevision.Query().
		Order(ent.Desc(catalogrevision.FieldCreatedAt)).Limit(20).All(ctx)
	revisionViews := make([]adminRevisionView, 0, len(revisions))
	for _, row := range revisions {
		user := "system"
		if row.UserID != nil {
			if u, err := s.Auth.Client.User.Get(ctx, *row.UserID); err == nil {
				user = "@" + u.Handle
			}
		}
		revisionViews = append(revisionViews, adminRevisionView{Entity: string(row.Entity), EntityID: row.EntityID, Action: row.Action, User: user, CreatedAt: row.CreatedAt})
	}
	data := map[string]any{
		"Title": "Admin", "Reports": reportViews, "ProvisionalDrinks": drinkViews,
		"Users": users, "AIEndpoints": endpoints, "Revisions": revisionViews,
		"OpenReports": len(reportViews), "Provisional": len(drinkViews),
	}
	s.render(w, r, http.StatusOK, "admin", data)
}

func (s *Server) adminReportResolve(w http.ResponseWriter, r *http.Request) {
	s.setReportStatus(w, r, "resolved")
}
func (s *Server) adminReportDismiss(w http.ResponseWriter, r *http.Request) {
	s.setReportStatus(w, r, "dismissed")
}

func (s *Server) setReportStatus(w http.ResponseWriter, r *http.Request, status string) {
	if !s.csrfOK(w, r) {
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	if _, err := s.Auth.Client.Report.UpdateOneID(id).
		SetStatus(report.Status(status)).SetResolvedAt(time.Now().UTC().Format(time.RFC3339)).Save(r.Context()); err != nil {
		http.Error(w, "could not update report", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (s *Server) adminDrinkActivate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	if _, err := s.Auth.Client.Drink.UpdateOneID(id).SetStatus(drink.StatusActive).Save(r.Context()); err != nil {
		http.Error(w, "could not activate drink", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (s *Server) adminAISave(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, _ := strconv.Atoi(r.FormValue("id"))
	var temp *float64
	if v := r.FormValue("temperature"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			temp = &f
		}
	}
	var tokens *int
	if v := r.FormValue("max_tokens"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tokens = &n
		}
	}
	tags := []string{}
	for _, tag := range strings.Split(r.FormValue("tags"), ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	enabled := r.FormValue("enabled") != ""
	_, err := s.AI.SaveEndpoint(r.Context(), id, ai.EndpointInput{
		Name: r.FormValue("name"), Type: r.FormValue("type"), BaseURL: r.FormValue("base_url"),
		APIKey: r.FormValue("api_key"), ClearAPIKey: r.FormValue("clear_api_key") == "1",
		Model: r.FormValue("model"), Tags: tags, Enabled: &enabled,
		Temperature: temp, MaxTokens: tokens, ImageSize: r.FormValue("image_size"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (s *Server) adminAIToggle(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	row, err := s.AI.ListEndpoints(r.Context())
	if err != nil {
		http.Error(w, "ai unavailable", http.StatusBadRequest)
		return
	}
	for _, e := range row {
		if e.ID == id {
			tags := []string{}
			for _, tag := range strings.Split(e.Tags, ",") {
				if tag = strings.TrimSpace(tag); tag != "" {
					tags = append(tags, tag)
				}
			}
			enabled := !e.Enabled
			_, err = s.AI.SaveEndpoint(r.Context(), id, ai.EndpointInput{
				Name: e.Name, Type: string(e.Type), BaseURL: e.BaseURL, Model: e.Model,
				Tags: tags, Enabled: &enabled, Temperature: e.Temperature, MaxTokens: e.MaxTokens,
				ImageSize: e.ImageSize,
			})
			break
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (s *Server) adminAITest(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	status, preview, err := s.AI.TestEndpoint(r.Context(), id)
	out := map[string]any{"status": status, "preview": preview}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminDigest(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if err := s.Integrations.WeeklyDigest(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (s *Server) serviceWorker(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func (s *Server) webManifest(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/manifest.webmanifest")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write(data)
}
