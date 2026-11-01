package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/catalogrevision"
	"sodapop/ent/comment"
	"sodapop/ent/drink"
	"sodapop/ent/drinkimage"
	"sodapop/ent/pushdevice"
	"sodapop/ent/report"
	"sodapop/ent/session"
	"sodapop/ent/user"
	"sodapop/ent/venue"
	"sodapop/ent/venuemedia"
	"sodapop/internal/ai"
	"sodapop/internal/auth"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/comments"
	"sodapop/internal/db"
	"sodapop/internal/venues"
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

// requireAdminStrict limits a route to full administrators. Moderators keep
// their existing moderation tools but cannot change instance-wide settings.
func (s *Server) requireAdminStrict(next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r)
		if u == nil || u.Role != "admin" {
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
		// A takedown must not be masked by a previously cached 200 response.
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	var rc io.ReadCloser
	var row *mediaRow
	if thumb {
		f, m, err := s.Media.OpenThumb(r.Context(), id)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		rc, row = f, &mediaRow{mime: m.Mime, path: m.ThumbPath}
	} else {
		f, m, err := s.Media.Open(r.Context(), id)
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
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
		"FeedToken":       strings.TrimPrefix(feedURL, s.baseURL()+"/feeds/"),
		"BaseURL":         s.baseURL(),
		"VAPIDPublicKey":  s.vapidPublicKey(),
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
		"BaseURL": s.baseURL(), "VAPIDPublicKey": s.vapidPublicKey(),
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
	ID          int
	Entity      string
	EntityID    int
	EntityRef   string
	Reason      string
	Details     string
	Context     string
	Status      string
	CreatedAt   string
	Reporter    string
	EntitySlug  string
	CanRemove   bool
	RemoveLabel string
	CanEdit     bool
	EditBody    string
	EditLabel   string
	CanSuspend  bool
	SuspendName string
	Notes       []adminNoteView
}

// adminNoteView is the template contract for report notes. Keep these field
// names stable: admin.html renders them directly.
type adminNoteView struct {
	ID           int
	AuthorHandle string
	Body         string
	CreatedAt    string
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

	// Batch-load every string-ref target kind once so the queue stays O(kinds)
	// queries instead of one lookup per report.
	var commentRefs, mediaRefs, imageRefs []string
	for _, row := range reports {
		if row.EntityRef == "" {
			continue
		}
		switch row.Entity {
		case "comment":
			commentRefs = append(commentRefs, row.EntityRef)
		case "venue_media":
			mediaRefs = append(mediaRefs, row.EntityRef)
		case "drink_image":
			imageRefs = append(imageRefs, row.EntityRef)
		}
	}
	commentBodies := map[string]string{}
	if len(commentRefs) > 0 {
		if rows, err := s.Auth.Client.Comment.Query().
			Where(comment.IDIn(commentRefs...)).All(ctx); err == nil {
			for _, row := range rows {
				commentBodies[row.ID] = row.Body
			}
		}
	}
	type mediaContext struct {
		venueName string
		kind      string
		label     string
	}
	mediaContexts := map[string]mediaContext{}
	if len(mediaRefs) > 0 {
		if rows, err := s.Auth.Client.VenueMedia.Query().
			Where(venuemedia.IDIn(mediaRefs...)).All(ctx); err == nil {
			seen := map[int]bool{}
			venueIDs := make([]int, 0, len(rows))
			for _, row := range rows {
				if !seen[row.VenueID] {
					seen[row.VenueID] = true
					venueIDs = append(venueIDs, row.VenueID)
				}
			}
			venueNames := map[int]string{}
			if len(venueIDs) > 0 {
				if vs, err := s.Auth.Client.Venue.Query().
					Where(venue.IDIn(venueIDs...)).All(ctx); err == nil {
					for _, v := range vs {
						venueNames[v.ID] = v.Name
					}
				}
			}
			for _, row := range rows {
				mediaContexts[row.ID] = mediaContext{
					venueName: venueNames[row.VenueID], kind: string(row.Kind), label: row.Label,
				}
			}
		}
	}

	type imageContext struct {
		drinkName string
		drinkSlug string
	}
	imageContexts := map[string]imageContext{}
	if len(imageRefs) > 0 {
		ids := make([]int, 0, len(imageRefs))
		for _, ref := range imageRefs {
			if id, err := strconv.Atoi(ref); err == nil && id > 0 {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			if rows, err := s.Auth.Client.DrinkImage.Query().
				Where(drinkimage.IDIn(ids...)).WithDrink().All(ctx); err == nil {
				for _, row := range rows {
					ic := imageContext{}
					if row.Edges.Drink != nil {
						ic.drinkName = row.Edges.Drink.Name
						ic.drinkSlug = row.Edges.Drink.Slug
					}
					imageContexts[strconv.Itoa(row.ID)] = ic
				}
			}
		}
	}

	reportViews := make([]adminReportView, 0, len(reports))
	reportIDs := make([]int, 0, len(reports))
	for _, row := range reports {
		reportIDs = append(reportIDs, row.ID)
	}
	notesByReport := s.reportNotes(r, reportIDs)
	for _, row := range reports {
		v := adminReportView{ID: row.ID, Entity: string(row.Entity), EntityID: row.EntityID,
			EntityRef: row.EntityRef, Reason: row.Reason, Details: row.Details,
			Status: string(row.Status), CreatedAt: row.CreatedAt, Reporter: "deleted user",
			Notes: notesByReport[row.ID]}
		if row.UserID != nil {
			if u, err := s.Auth.Client.User.Get(ctx, *row.UserID); err == nil {
				v.Reporter = "@" + u.Handle
			}
		}
		switch row.Entity {
		case "drink":
			if d, err := s.Auth.Client.Drink.Get(ctx, row.EntityID); err == nil {
				v.EntitySlug = d.Slug
			}
		case "comment":
			v.EntityRef = shortenRef(row.EntityRef)
			v.Context = "Comment: " + excerptText(commentBodies[row.EntityRef], 140)
			v.CanRemove = true
			v.RemoveLabel = "Remove comment"
			v.CanEdit = true
			v.EditBody = commentBodies[row.EntityRef]
			v.EditLabel = "Edit comment"
		case "checkin":
			v.EntityRef = shortenRef(row.EntityRef)
			if c, err := s.Auth.Client.Checkin.Get(ctx, row.EntityRef); err == nil {
				author := "deleted user"
				if u, err := s.Auth.Client.User.Get(ctx, c.UserID); err == nil {
					author = "@" + u.Handle
				}
				v.Context = "Check-in by " + author
				if note := excerptText(c.Note, 140); note != "" {
					v.Context += ": " + note
				} else {
					v.Context += " · no note"
				}
				v.EditBody = c.Note
			}
			v.CanRemove = true
			v.RemoveLabel = "Remove check-in"
			v.CanEdit = true
			v.EditLabel = "Edit note"
		case "user":
			handle := strings.TrimPrefix(row.EntityRef, "@")
			if u, err := s.Auth.Client.User.Query().
				Where(user.HandleEqualFold(handle)).Only(ctx); err == nil {
				name := u.DisplayName
				if name == "" {
					name = "@" + u.Handle
				}
				v.Context = "Account " + name + " (@" + u.Handle + ") · " + string(u.Status)
				v.CanSuspend = u.Status == user.StatusActive
				v.SuspendName = "@" + u.Handle
			}
		case "venue_media":
			v.EntityRef = shortenRef(row.EntityRef)
			if mc, ok := mediaContexts[row.EntityRef]; ok {
				label := mc.kind
				if mc.label != "" {
					label += " · " + mc.label
				}
				if mc.venueName != "" {
					label = mc.venueName + " · " + label
				}
				v.Context = "Venue " + label
			}
			v.CanRemove = true
			v.RemoveLabel = "Remove media"
		case "drink_image":
			v.EntityRef = shortenRef(row.EntityRef)
			if ic, ok := imageContexts[row.EntityRef]; ok {
				v.Context = "Drink image"
				if ic.drinkName != "" {
					v.Context = "Drink " + ic.drinkName
				}
				v.EntitySlug = ic.drinkSlug
			}
			v.CanRemove = true
			v.RemoveLabel = "Remove image"
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
	data["Notice"], data["Error"] = adminReportQueryMessages(r)
	if u := UserFromContext(r); u != nil {
		data["Role"] = string(u.Role)
	}
	s.render(w, r, http.StatusOK, "admin", data)
}

// shortenRef trims a user-content ID for the moderation queue while keeping
// enough prefix to disambiguate it.
func shortenRef(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}

// excerptText trims a free-text body to at most n runes for queue context.
func excerptText(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return strings.TrimSpace(string(runes[:n])) + "…"
}

func (s *Server) adminReportResolve(w http.ResponseWriter, r *http.Request) {
	s.setReportStatus(w, r, "resolved")
}
func (s *Server) adminReportDismiss(w http.ResponseWriter, r *http.Request) {
	s.setReportStatus(w, r, "dismissed")
}

// adminReportRemove soft-deletes the reported comment or venue media and then
// resolves the report. An already-deleted target is idempotent: the report is
// still resolved. An unknown report or target is a 404.
func (s *Server) adminReportRemove(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	ctx := r.Context()
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	row, err := s.Auth.Client.Report.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	if row.EntityRef == "" {
		http.NotFound(w, r)
		return
	}
	u := UserFromContext(r)
	switch row.Entity {
	case "comment":
		if _, err := s.Auth.Client.Comment.Get(ctx, row.EntityRef); err != nil {
			if ent.IsNotFound(err) {
				http.NotFound(w, r)
				return
			}
			s.fail(w, r, err)
			return
		}
		if err := s.commentService().Delete(ctx, u.ID, true, row.EntityRef); err != nil &&
			!errors.Is(err, comments.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
	case "venue_media":
		if _, err := s.Auth.Client.VenueMedia.Get(ctx, row.EntityRef); err != nil {
			if ent.IsNotFound(err) {
				http.NotFound(w, r)
				return
			}
			s.fail(w, r, err)
			return
		}
		if err := s.Venues.RemoveVenueMedia(ctx, u.ID, true, row.EntityRef); err != nil &&
			!errors.Is(err, venues.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
	case "checkin":
		if err := s.Checkins.DeleteAsModerator(ctx, row.EntityRef); err != nil &&
			!errors.Is(err, checkins.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
	case "drink_image":
		imageID, err := strconv.Atoi(row.EntityRef)
		if err != nil || imageID <= 0 {
			http.NotFound(w, r)
			return
		}
		if _, err := s.Auth.Client.DrinkImage.Get(ctx, imageID); err != nil {
			if ent.IsNotFound(err) {
				http.NotFound(w, r)
				return
			}
			s.fail(w, r, err)
			return
		}
		// Delete only the image and its media row; the drink is untouched.
		if err := s.Catalog.DeleteImage(ctx, true, imageID); err != nil &&
			!errors.Is(err, catalog.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
	default:
		http.Error(w, "report target cannot be removed", http.StatusBadRequest)
		return
	}
	if _, err := s.Auth.Client.Report.UpdateOneID(id).
		SetStatus(report.StatusResolved).
		SetResolvedAt(time.Now().UTC().Format(time.RFC3339)).Save(ctx); err != nil {
		http.Error(w, "could not update report", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin?done=removed", http.StatusFound)
}

// adminReportQueryMessages turns the redirect flags the moderation actions set
// into the banner text the queue renders.
func adminReportQueryMessages(r *http.Request) (notice, errMsg string) {
	switch r.URL.Query().Get("done") {
	case "edited":
		notice = "Content updated."
	case "removed":
		notice = "Content removed."
	case "suspended":
		notice = "Account suspended. Their sessions, tokens and push devices are revoked."
	}
	switch r.URL.Query().Get("error") {
	case "invalid":
		errMsg = "That change was not valid. Comments and notes are capped at 1,000 characters."
	case "missing":
		errMsg = "That content no longer exists."
	}
	return notice, errMsg
}

// adminReportEdit rewrites the text a report is about, so an admin can fix a
// note or comment without deleting the whole check-in. The report stays open:
// editing is not a verdict, and the queue still needs a resolve or dismiss.
func (s *Server) adminReportEdit(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	ctx := r.Context()
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	row, err := s.Auth.Client.Report.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	body := r.FormValue("body")
	switch row.Entity {
	case "comment":
		err = s.commentService().EditAsModerator(ctx, row.EntityRef, body)
		if errors.Is(err, comments.ErrInvalid) {
			http.Redirect(w, r, "/admin?error=invalid", http.StatusFound)
			return
		}
	case "checkin":
		err = s.Checkins.SetNoteAsModerator(ctx, row.EntityRef, body)
		if errors.Is(err, checkins.ErrInvalidInput) {
			http.Redirect(w, r, "/admin?error=invalid", http.StatusFound)
			return
		}
	default:
		http.Error(w, "report target cannot be edited", http.StatusBadRequest)
		return
	}
	if err != nil {
		if errors.Is(err, comments.ErrNotFound) || errors.Is(err, checkins.ErrNotFound) {
			http.Redirect(w, r, "/admin?error=missing", http.StatusFound)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin?done=edited", http.StatusFound)
}

// adminReportSuspend disables a reported account. Suspension is the account
// equivalent of removal: rows stay so other people's feeds do not unravel, but
// the status check in session and token authentication locks the user out
// immediately, and dropping sessions and push devices makes that stick.
func (s *Server) adminReportSuspend(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	ctx := r.Context()
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	row, err := s.Auth.Client.Report.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	if row.Entity != "user" {
		http.Error(w, "only accounts can be suspended", http.StatusBadRequest)
		return
	}
	handle := strings.TrimPrefix(row.EntityRef, "@")
	target, err := s.Auth.Client.User.Query().
		Where(user.HandleEqualFold(handle), user.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			http.Redirect(w, r, "/admin?error=missing", http.StatusFound)
			return
		}
		s.fail(w, r, err)
		return
	}
	if row.UserID != nil && *row.UserID == target.ID {
		http.Error(w, "you cannot suspend yourself", http.StatusBadRequest)
		return
	}
	if _, err := target.Update().SetStatus(user.StatusSuspended).SetUpdatedAt(db.Now()).Save(ctx); err != nil {
		s.fail(w, r, err)
		return
	}
	_, _ = s.Auth.Client.Session.Delete().Where(session.UserID(target.ID)).Exec(ctx)
	_, _ = s.Auth.Client.PushDevice.Delete().Where(pushdevice.UserID(target.ID)).Exec(ctx)
	_, _ = s.Auth.Client.Report.UpdateOneID(id).
		SetStatus(report.StatusResolved).
		SetResolvedAt(time.Now().UTC().Format(time.RFC3339)).Save(ctx)
	http.Redirect(w, r, "/admin?done=suspended", http.StatusFound)
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
	u := UserFromContext(r)
	if _, err := s.Catalog.ActivateDrink(r.Context(), &u.ID, id); err != nil {
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
		http.Redirect(w, r, "/admin/digest?error=send", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/digest?sent=1", http.StatusFound)
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
