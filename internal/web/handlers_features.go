package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"sodapop/ent"
	"sodapop/ent/comment"
	"sodapop/ent/list"
	"sodapop/ent/listitem"
	"sodapop/ent/report"
	"sodapop/ent/venuemedia"
	"sodapop/internal/db"
	"sodapop/internal/mix"
	"sodapop/internal/venues"
)

type listView struct {
	ID          string
	Name        string
	Description string
	Visibility  string
	IsSystem    bool
	Count       int
}

type listItemView struct {
	DrinkID  int
	Name     string
	Slug     string
	Brand    string
	Style    string
	VolumeML int
	Note     string
	CanEdit  bool
}

func (s *Server) listsPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	rows, err := s.Auth.Client.List.Query().
		Where(list.UserID(u.ID)).
		Order(ent.Asc(list.FieldIsSystem), ent.Desc(list.FieldUpdatedAt)).
		All(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	views := make([]listView, 0, len(rows))
	for _, l := range rows {
		n, _ := s.Auth.Client.ListItem.Query().Where(listitem.ListID(l.ID)).Count(r.Context())
		views = append(views, listView{ID: l.ID, Name: l.Name, Description: l.Description, Visibility: string(l.Visibility), IsSystem: l.IsSystem, Count: n})
	}
	s.render(w, r, http.StatusOK, "lists", map[string]any{"Title": "Lists", "Lists": views})
}

func (s *Server) listCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	vis := r.FormValue("visibility")
	if vis != "public" && vis != "friends" && vis != "private" {
		vis = "private"
	}
	_, err := s.Auth.Client.List.Create().
		SetUserID(u.ID).SetName(name).SetDescription(strings.TrimSpace(r.FormValue("description"))).
		SetVisibility(list.Visibility(vis)).SetSlug(slugifyFeature(name)).Save(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/lists", http.StatusFound)
}

func (s *Server) listPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	l, err := s.Auth.Client.List.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	canEdit := l.UserID == u.ID
	if !canEdit && l.Visibility == list.VisibilityPrivate {
		http.NotFound(w, r)
		return
	}
	if !canEdit && l.Visibility == list.VisibilityFriends && !s.Social.AreFriends(r.Context(), u.ID, l.UserID) {
		http.NotFound(w, r)
		return
	}
	items, err := s.listItems(r, l.ID, canEdit && !l.IsSystem)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "list", map[string]any{
		"Title": l.Name, "List": l, "Items": items, "CanEdit": canEdit && !l.IsSystem,
	})
}

func (s *Server) listEdit(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	l, err := s.Auth.Client.List.Get(r.Context(), r.PathValue("id"))
	if err != nil || l.UserID != u.ID {
		http.NotFound(w, r)
		return
	}
	if l.IsSystem {
		http.Error(w, "built-in lists cannot be modified", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if n := len([]rune(name)); n < 1 || n > 80 {
		http.Error(w, "name must be 1–80 characters", http.StatusBadRequest)
		return
	}
	description := strings.TrimSpace(r.FormValue("description"))
	if len([]rune(description)) > 500 {
		http.Error(w, "description must be 500 characters or fewer", http.StatusBadRequest)
		return
	}
	vis := r.FormValue("visibility")
	if vis != "public" && vis != "friends" && vis != "private" {
		http.Error(w, "choose a valid visibility", http.StatusBadRequest)
		return
	}
	if _, err := l.Update().
		SetName(name).
		SetDescription(description).
		SetVisibility(list.Visibility(vis)).
		SetUpdatedAt(db.Now()).
		Save(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/lists/"+l.ID, http.StatusFound)
}

func (s *Server) listDelete(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	l, err := s.Auth.Client.List.Get(r.Context(), r.PathValue("id"))
	if err != nil || l.UserID != u.ID {
		http.NotFound(w, r)
		return
	}
	if l.IsSystem {
		http.Error(w, "built-in lists cannot be deleted", http.StatusBadRequest)
		return
	}
	tx, err := s.Auth.Client.Tx(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := tx.ListItem.Delete().Where(listitem.ListID(l.ID)).Exec(r.Context()); err != nil {
		_ = tx.Rollback()
		s.fail(w, r, err)
		return
	}
	if err := tx.List.DeleteOneID(l.ID).Exec(r.Context()); err != nil {
		_ = tx.Rollback()
		s.fail(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/lists", http.StatusFound)
}

func (s *Server) listItems(r *http.Request, listID string, canEdit bool) ([]listItemView, error) {
	rows, err := s.Auth.SQL.QueryContext(r.Context(), `
		SELECT li.drink_id, d.name, d.slug, COALESCE(b.name,''), COALESCE(st.name,''), d.volume_ml, li.note
		FROM list_items li
		JOIN drinks d ON d.id = li.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN styles st ON st.id = d.style_id
		WHERE li.list_id = ? AND d.status != 'merged'
		ORDER BY li.added_at DESC`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []listItemView{}
	for rows.Next() {
		var it listItemView
		if err := rows.Scan(&it.DrinkID, &it.Name, &it.Slug, &it.Brand, &it.Style, &it.VolumeML, &it.Note); err != nil {
			return nil, err
		}
		it.CanEdit = canEdit
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Server) listAdd(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	id := r.FormValue("list_id")
	l, err := s.Auth.Client.List.Get(r.Context(), id)
	if err != nil || l.UserID != u.ID {
		http.NotFound(w, r)
		return
	}
	drinkID, _ := strconv.Atoi(r.FormValue("drink_id"))
	if drinkID <= 0 {
		http.Error(w, "drink required", http.StatusBadRequest)
		return
	}
	exists, _ := s.Auth.Client.ListItem.Query().
		Where(listitem.ListID(id), listitem.DrinkID(drinkID)).Exist(r.Context())
	if !exists {
		_, err = s.Auth.Client.ListItem.Create().SetListID(id).SetDrinkID(drinkID).
			SetNote(strings.TrimSpace(r.FormValue("note"))).Save(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		_, _ = l.Update().SetUpdatedAt(db.Now()).Save(r.Context())
	}
	http.Redirect(w, r, "/lists/"+id, http.StatusFound)
}

func (s *Server) listRemove(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	id := r.PathValue("id")
	l, err := s.Auth.Client.List.Get(r.Context(), id)
	if err != nil || l.UserID != u.ID {
		http.NotFound(w, r)
		return
	}
	drinkID, _ := strconv.Atoi(r.PathValue("drink"))
	_, err = s.Auth.Client.ListItem.Delete().
		Where(listitem.ListID(id), listitem.DrinkID(drinkID)).Exec(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/lists/"+id, http.StatusFound)
}

type passportStyleRow struct {
	Name  string
	Count int
	Last  string
}
type passportCountRow struct {
	Label string
	Count int
}
type passportRecent struct {
	Name, Slug, Style, CreatedAt, Rating string
}

type passportData struct {
	Checkins, Styles, VenueKinds, Countries int
	StyleRows                               []passportStyleRow
	VenueRows, CountryRows                  []passportCountRow
	Recent                                  []passportRecent
}

func (s *Server) passportPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	p := passportData{StyleRows: []passportStyleRow{}, VenueRows: []passportCountRow{}, CountryRows: []passportCountRow{}, Recent: []passportRecent{}}
	_ = s.Auth.SQL.QueryRowContext(r.Context(), `SELECT count(*) FROM checkins WHERE user_id=? AND deleted_at IS NULL`, u.ID).Scan(&p.Checkins)
	_ = s.Auth.SQL.QueryRowContext(r.Context(), `SELECT count(DISTINCT d.style_id) FROM checkins c JOIN drinks d ON d.id=c.drink_id WHERE c.user_id=? AND c.deleted_at IS NULL AND d.style_id IS NOT NULL`, u.ID).Scan(&p.Styles)
	_ = s.Auth.SQL.QueryRowContext(r.Context(), `SELECT count(DISTINCT v.kind) FROM checkins c JOIN venues v ON v.id=c.venue_id WHERE c.user_id=? AND c.deleted_at IS NULL`, u.ID).Scan(&p.VenueKinds)
	_ = s.Auth.SQL.QueryRowContext(r.Context(), `SELECT count(DISTINCT b.country) FROM checkins c JOIN drinks d ON d.id=c.drink_id JOIN brands b ON b.id=d.brand_id WHERE c.user_id=? AND c.deleted_at IS NULL AND b.country IS NOT NULL AND b.country != ''`, u.ID).Scan(&p.Countries)

	if rows, err := s.Auth.SQL.QueryContext(r.Context(), `
		SELECT st.name, count(*), max(c.created_at)
		FROM checkins c JOIN drinks d ON d.id=c.drink_id JOIN styles st ON st.id=d.style_id
		WHERE c.user_id=? AND c.deleted_at IS NULL
		GROUP BY st.name ORDER BY count(*) DESC, st.name`, u.ID); err == nil {
		for rows.Next() {
			var row passportStyleRow
			if rows.Scan(&row.Name, &row.Count, &row.Last) == nil {
				p.StyleRows = append(p.StyleRows, row)
			}
		}
		rows.Close()
	}
	if rows, err := s.Auth.SQL.QueryContext(r.Context(), `
		SELECT v.kind, count(*) FROM checkins c JOIN venues v ON v.id=c.venue_id
		WHERE c.user_id=? AND c.deleted_at IS NULL GROUP BY v.kind ORDER BY count(*) DESC`, u.ID); err == nil {
		for rows.Next() {
			var row passportCountRow
			if rows.Scan(&row.Label, &row.Count) == nil {
				p.VenueRows = append(p.VenueRows, row)
			}
		}
		rows.Close()
	}
	if rows, err := s.Auth.SQL.QueryContext(r.Context(), `
		SELECT b.country, count(*) FROM checkins c JOIN drinks d ON d.id=c.drink_id JOIN brands b ON b.id=d.brand_id
		WHERE c.user_id=? AND c.deleted_at IS NULL AND b.country IS NOT NULL AND b.country != ''
		GROUP BY b.country ORDER BY count(*) DESC`, u.ID); err == nil {
		for rows.Next() {
			var row passportCountRow
			if rows.Scan(&row.Label, &row.Count) == nil {
				p.CountryRows = append(p.CountryRows, row)
			}
		}
		rows.Close()
	}
	if rows, err := s.Auth.SQL.QueryContext(r.Context(), `
		SELECT d.name, d.slug, COALESCE(st.name,''), c.created_at, c.rating
		FROM checkins c JOIN drinks d ON d.id=c.drink_id LEFT JOIN styles st ON st.id=d.style_id
		WHERE c.user_id=? AND c.deleted_at IS NULL ORDER BY c.created_at DESC LIMIT 8`, u.ID); err == nil {
		for rows.Next() {
			var row passportRecent
			var rating sql.NullInt64
			if rows.Scan(&row.Name, &row.Slug, &row.Style, &row.CreatedAt, &rating) == nil {
				if rating.Valid {
					row.Rating = quarterLabel(int(rating.Int64))
				} else {
					row.Rating = "Not rated"
				}
				p.Recent = append(p.Recent, row)
			}
		}
		rows.Close()
	}
	s.render(w, r, http.StatusOK, "passport", map[string]any{"Title": "Soda passport", "Passport": p})
}

func (s *Server) reportSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u.EmailVerifiedAt == nil {
		http.Error(w, "verify your email first", http.StatusForbidden)
		return
	}
	kind := r.FormValue("entity")
	entityRef := strings.TrimSpace(r.FormValue("entity_ref"))
	entityID, _ := strconv.Atoi(r.FormValue("entity_id"))
	switch kind {
	case "comment":
		if entityRef == "" {
			http.Error(w, "invalid entity", http.StatusBadRequest)
			return
		}
		if !s.reportableComment(r.Context(), u.ID, entityRef) {
			http.NotFound(w, r)
			return
		}
		entityID = 0
	case "venue_media":
		if entityRef == "" {
			http.Error(w, "invalid entity", http.StatusBadRequest)
			return
		}
		if !s.reportableVenueMedia(r.Context(), entityRef) {
			http.NotFound(w, r)
			return
		}
		entityID = 0
	case "drink", "brand", "venue", "checkin", "user":
		entityRef = ""
		if entityID <= 0 {
			http.Error(w, "invalid entity", http.StatusBadRequest)
			return
		}
	default:
		kind = "drink"
		entityRef = ""
		if entityID <= 0 {
			http.Error(w, "invalid entity", http.StatusBadRequest)
			return
		}
	}
	create := s.Auth.Client.Report.Create().
		SetEntity(report.Entity(kind)).SetEntityID(entityID).SetUserID(u.ID).
		SetReason(strings.TrimSpace(r.FormValue("reason"))).
		SetDetails(strings.TrimSpace(r.FormValue("details")))
	if entityRef != "" {
		create.SetEntityRef(entityRef)
	}
	if _, err := create.Save(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	target := "/"
	if ref := r.Header.Get("Referer"); ref != "" {
		if parsed, err := url.Parse(ref); err == nil &&
			(parsed.Host == "" || strings.EqualFold(parsed.Host, r.Host)) &&
			strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "//") {
			target = parsed.EscapedPath()
			if parsed.RawQuery != "" {
				target += "?" + parsed.RawQuery
			}
		}
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// reportableComment reports whether a comment exists, is not soft-deleted, and
// sits on a check-in the viewer can see. Reporting never grants extra access.
func (s *Server) reportableComment(ctx context.Context, viewerID, id string) bool {
	c, err := s.Auth.Client.Comment.Query().
		Where(comment.ID(id), comment.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		return false
	}
	_, err = s.Checkins.Get(ctx, viewerID, c.CheckinID)
	return err == nil
}

// reportableVenueMedia reports whether venue media exists, is not soft-deleted,
// and belongs to a venue that is still visible.
func (s *Server) reportableVenueMedia(ctx context.Context, id string) bool {
	row, err := s.Auth.Client.VenueMedia.Query().
		Where(venuemedia.ID(id), venuemedia.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		return false
	}
	views, err := s.Venues.VenueMedia(ctx, row.VenueID)
	if err != nil {
		return false
	}
	for _, v := range views {
		if v.ID == id {
			return true
		}
	}
	return false
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugifyFeature(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func quarterLabel(v int) string {
	if v == 0 {
		return "Not rated"
	}
	return strconv.FormatFloat(float64(v)/4, 'f', 2, 64)
}

type badgeView struct {
	Slug        string
	Name        string
	Description string
	Earned      bool
	AwardedAt   string
	Progress    int
	Target      int
	Percent     int
}

func (s *Server) youPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	stats, _ := s.Checkins.Stats(r.Context(), u.ID)
	statuses, err := s.Badges.ListForUser(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	progress, _ := s.Badges.Progress(r.Context(), u.ID)
	badgeViews := make([]badgeView, 0, len(statuses))
	for _, st := range statuses {
		key, target := ruleTarget(st.Rule)
		have := progress[key]
		pct := 0
		if target > 0 {
			pct = have * 100 / target
			if pct > 100 {
				pct = 100
			}
		}
		badgeViews = append(badgeViews, badgeView{
			Slug: st.Slug, Name: st.Name, Description: st.Description,
			Earned: st.Earned, AwardedAt: st.AwardedAt,
			Progress: have, Target: target, Percent: pct,
		})
	}
	recent, _ := s.Checkins.ListForUser(r.Context(), u.ID, u.ID, 8, "")
	s.render(w, r, http.StatusOK, "you", map[string]any{
		"Title": "You", "Stats": stats, "Badges": badgeViews, "Recent": recent,
		"IsAdmin": u.Role == "admin" || u.Role == "moderator",
	})
}

func ruleTarget(rule string) (string, int) {
	key, value, ok := strings.Cut(strings.TrimSpace(rule), ">=")
	if !ok {
		return "", 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return strings.TrimSpace(key), n
}

func (s *Server) tastePage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	twins, err := s.Taste.Twins(r.Context(), u.ID, 10)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	recs, err := s.Taste.Recommendations(r.Context(), u.ID, 10)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	aiEnabled, aiRecs := s.aiRecommendations(r.Context(), u.ID)
	s.render(w, r, http.StatusOK, "taste", map[string]any{
		"Title": "Taste twin", "Twins": twins, "Recommendations": recs,
		"AIEnabled": aiEnabled, "AIRecommendations": aiRecs,
	})
}

// aiRecommendationView is the template-facing AI recommendation shape.
type aiRecommendationView struct {
	Slug   string
	Name   string
	Reason string
}

// aiRecommendations returns recommendations only when AI is enabled. Any
// disabled state or error leaves the deterministic page to render normally.
func (s *Server) aiRecommendations(ctx context.Context, userID string) (bool, []aiRecommendationView) {
	if s.AI == nil {
		return false, []aiRecommendationView{}
	}
	enabled, err := s.AI.Enabled(ctx)
	if err != nil || !enabled {
		return false, []aiRecommendationView{}
	}
	recs, err := s.Taste.Recommendations(ctx, userID, 10)
	if err != nil {
		return true, []aiRecommendationView{}
	}
	out := make([]aiRecommendationView, 0, len(recs))
	for _, rec := range recs {
		out = append(out, aiRecommendationView{Slug: rec.Slug, Name: rec.Name, Reason: rec.Reason})
	}
	return true, out
}

type mixView struct {
	Slug       string
	Name       string
	Author     string
	Parts      int
	Visibility string
	Rating     string
}

func (s *Server) mixPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	rows, err := s.Mix.List(r.Context(), u.ID, 30)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	views := make([]mixView, 0, len(rows))
	for _, m := range rows {
		author := m.CreatedBy
		if row, err := s.Auth.Client.User.Get(r.Context(), m.CreatedBy); err == nil {
			author = "@" + row.Handle
		}
		views = append(views, mixView{
			Slug: m.Slug, Name: m.Name, Author: author, Parts: len(m.Components),
			Visibility: m.Visibility,
		})
	}
	drinks, _ := s.Catalog.Search(r.Context(), "cola", "", 10)
	s.render(w, r, http.StatusOK, "mix", map[string]any{
		"Title": "Mix Lab", "Mixes": views,
		"ComponentSlots": []int{1, 2, 3, 4, 5, 6}, "DrinkOptions": drinks,
	})
}

func (s *Server) mixCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	components := []mix.Component{}
	for i := 1; i <= 6; i++ {
		slug := strings.TrimSpace(r.FormValue("drink_" + strconv.Itoa(i)))
		if slug == "" {
			continue
		}
		d, err := s.Catalog.GetBySlug(r.Context(), slug)
		if err != nil {
			continue
		}
		parts, _ := strconv.Atoi(r.FormValue("parts_" + strconv.Itoa(i)))
		if parts <= 0 {
			parts = 1
		}
		components = append(components, mix.Component{DrinkID: d.Drink.ID, Parts: parts})
	}
	m, err := s.Mix.Create(r.Context(), mix.Input{
		UserID: u.ID, Name: r.FormValue("name"), Notes: r.FormValue("notes"),
		Visibility: r.FormValue("visibility"), Components: components,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/mix/"+m.Slug, http.StatusFound)
}

func (s *Server) mixDetailPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	m, err := s.Mix.GetForViewer(r.Context(), r.PathValue("slug"), u.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	author := m.CreatedBy
	if row, err := s.Auth.Client.User.Get(r.Context(), m.CreatedBy); err == nil {
		author = "@" + row.Handle
	}
	type componentView struct {
		Parts  int
		Name   string
		Slug   string
		Brand  string
		Rating string
	}
	components := make([]componentView, 0, len(m.Components))
	for _, c := range m.Components {
		d, err := s.Catalog.GetByID(r.Context(), c.DrinkID)
		if err != nil {
			continue
		}
		rating := "Not rated"
		if d.Rating.Shown && d.Rating.Avg != nil {
			rating = strconv.FormatFloat(*d.Rating.Avg, 'f', 2, 64)
		}
		components = append(components, componentView{
			Parts: c.Parts, Name: d.Drink.Name, Slug: d.Drink.Slug,
			Brand: d.BrandName, Rating: rating,
		})
	}
	s.render(w, r, http.StatusOK, "mix_detail", map[string]any{
		"Title": m.Name, "Mix": map[string]any{
			"Name": m.Name, "Slug": m.Slug, "Notes": m.Notes,
			"Visibility": m.Visibility, "Author": author,
		},
		"Components": components,
	})
}

type venueCard struct {
	ID, Slug, Name, Kind, Address, City, Country string
}

func (s *Server) venuesPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := map[string]any{"Title": "Venues", "Query": q, "Kinds": []string{"hotel", "bar", "restaurant", "cafe", "shop", "festival", "vending", "other"}}
	if len([]rune(q)) >= 2 {
		rows, err := s.Venues.Search(r.Context(), q, 20)
		if err != nil {
			data["Error"] = "Venue search is unavailable."
		}
		data["Venues"] = rows
	}
	s.render(w, r, http.StatusOK, "venues", data)
}

func (s *Server) venuesSavedPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	rows, err := s.Venues.Saved(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "venues_saved", map[string]any{
		"Title": "Saved venues", "Venues": rows,
	})
}

func (s *Server) venueUnsave(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	id, _ := strconv.Atoi(r.PathValue("id"))
	if err := s.Venues.Unsave(r.Context(), u.ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/venues/saved", http.StatusFound)
}

func (s *Server) venueCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	lat, _ := strconv.ParseFloat(r.FormValue("lat"), 64)
	lng, _ := strconv.ParseFloat(r.FormValue("lng"), 64)
	v, _, err := s.Venues.Create(r.Context(), u.ID, venues.CreateInput{
		Name: r.FormValue("name"), Kind: r.FormValue("kind"),
		Address: r.FormValue("address"), City: r.FormValue("city"),
		Region: r.FormValue("region"), Country: r.FormValue("country"),
		Lat: lat, Lng: lng, Website: r.FormValue("website"),
	})
	if err != nil {
		s.render(w, r, http.StatusBadRequest, "venues", map[string]any{
			"Title": "Venues", "Error": err.Error(), "Kinds": []string{"hotel", "bar", "restaurant", "cafe", "shop", "festival", "vending", "other"},
		})
		return
	}
	http.Redirect(w, r, "/venues/"+strconv.Itoa(v.ID), http.StatusFound)
}

func (s *Server) venuePage(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	v, drinks, err := s.Venues.Get(r.Context(), id)
	if err != nil {
		s.render(w, r, http.StatusNotFound, "error", map[string]any{"Title": "Not found", "Message": "That venue is not in Sodapop."})
		return
	}
	mediaRows, err := s.Venues.VenueMedia(r.Context(), id)
	if err != nil {
		if errors.Is(err, venues.ErrNotFound) {
			s.render(w, r, http.StatusNotFound, "error", map[string]any{"Title": "Not found", "Message": "That venue is not in Sodapop."})
			return
		}
		s.fail(w, r, err)
		return
	}
	u := UserFromContext(r)
	for i := range mediaRows {
		mediaRows[i].CanRemove = u != nil &&
			(mediaRows[i].UploadedBy == u.ID || u.Role == "admin" || u.Role == "moderator")
	}
	canUpload := u != nil && u.EmailVerifiedAt != nil
	s.render(w, r, http.StatusOK, "venue", map[string]any{
		"Title":      v.Name,
		"Venue":      v,
		"Drinks":     drinks,
		"Media":      mediaRows,
		"CanUpload":  canUpload,
		"MediaError": mediaErrorFromQuery(r.URL.Query().Get("media_error")),
	})
}

// mediaErrorFromQuery maps a fixed set of query codes to display copy. It never
// reflects arbitrary query text into the page.
func mediaErrorFromQuery(value string) string {
	switch value {
	case "invalid":
		return "That file must be a JPEG, PNG, WebP or GIF image."
	case "too_large":
		return "That image is too large. Photos must be 12 MB or smaller."
	case "limit":
		return "This venue has reached its photo and menu limit. Remove one before uploading another."
	default:
		return ""
	}
}

func (s *Server) venueSave(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	id, _ := strconv.Atoi(r.PathValue("id"))
	var err error
	if r.FormValue("undo") == "1" {
		err = s.Venues.Unsave(r.Context(), u.ID, id)
	} else {
		err = s.Venues.Save(r.Context(), u.ID, id)
	}
	if err != nil {
		http.Error(w, "could not update saved venue", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/venues/"+strconv.Itoa(id), http.StatusFound)
}
