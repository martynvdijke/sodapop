package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sodapop/internal/overview"
)

const activityPageSize = 50

// activityKinds is the filter order shown above the feed. Keys match the
// overview service whitelist exactly; labels are the visible names.
var activityKinds = []struct{ Key, Label string }{
	{"checkin", "Check-ins"},
	{"drink", "Sodas"},
	{"user", "Members"},
	{"venue", "Venues"},
	{"report", "Reports"},
	{"comment", "Comments"},
	{"toast", "Toasts"},
}

type activityKindLink struct {
	Key, Label, Href string
	Active           bool
}

// activityRow is one feed entry with its display values precomputed. The
// template never parses timestamps or cursors.
type activityRow struct {
	KindLabel string
	When      string
	Actor     string
	Title     string
	Detail    string
	Href      string
	Pill      string
	PillWarn  bool
}

// adminActivityPage renders the instance-wide activity feed. Read failures
// degrade to the page with an Error banner, the way the other admin pages do.
func (s *Server) adminActivityPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()
	before := strings.TrimSpace(query.Get("before"))
	rawKinds := splitActivityKinds(query.Get("kind"))
	selected := selectedActivityKinds(rawKinds)
	selectedKind := strings.Join(selected, ",")

	svc := overview.New(s.DB.SQL)
	events, nextBefore, listErr := svc.List(ctx, overview.Filter{
		Kinds:  rawKinds,
		Before: before,
		Limit:  activityPageSize,
	})
	counts, countsErr := svc.Counts(ctx)

	errText := ""
	if countsErr != nil {
		errText = "The activity counters could not be loaded right now."
	}
	if listErr != nil {
		switch {
		case errors.Is(listErr, overview.ErrInvalidCursor):
			errText = "That activity link is no longer valid. Start again from the latest activity."
		default:
			errText = "The activity feed could not be loaded right now."
		}
		events = nil
		nextBefore = ""
	}

	now := time.Now().UTC()
	rows := activityRows(events, now)

	olderHref := ""
	if nextBefore != "" {
		values := url.Values{}
		values.Set("before", nextBefore)
		if selectedKind != "" {
			values.Set("kind", selectedKind)
		}
		olderHref = "/admin/activity?" + values.Encode()
	}

	data := map[string]any{
		"Title":        "Activity",
		"Events":       rows,
		"Counts":       counts,
		"NextBefore":   nextBefore,
		"OlderHref":    olderHref,
		"SelectedKind": selectedKind,
		"Kinds":        activityKindLinks(selected, len(rawKinds) == 0),
		"Empty":        len(rows) == 0,
		"HasCursor":    before != "",
		"Notice":       "",
		"Error":        errText,
	}
	if u := UserFromContext(r); u != nil {
		data["Role"] = string(u.Role)
	}
	s.render(w, r, http.StatusOK, "admin_activity", data)
}

// splitActivityKinds splits a comma-separated kind query value into the raw
// whitelist the service expects. Unknown names are passed through on purpose:
// the service ignores them, and an all-unknown filter must return an empty
// page rather than everything.
func splitActivityKinds(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if kind := strings.ToLower(strings.TrimSpace(part)); kind != "" {
			out = append(out, kind)
		}
	}
	return out
}

// selectedActivityKinds narrows the raw filter to the known kinds for the
// visible filter state and the pagination link. It preserves display order
// and drops duplicates.
func selectedActivityKinds(raw []string) []string {
	wanted := make(map[string]bool, len(raw))
	for _, kind := range raw {
		wanted[kind] = true
	}
	selected := make([]string, 0, len(wanted))
	for _, kind := range activityKinds {
		if wanted[kind.Key] {
			selected = append(selected, kind.Key)
		}
	}
	return selected
}

func activityKindLinks(selected []string, allActive bool) []activityKindLink {
	active := map[string]bool{}
	for _, key := range selected {
		active[key] = true
	}
	links := make([]activityKindLink, 0, len(activityKinds)+1)
	links = append(links, activityKindLink{
		Key:    "",
		Label:  "All",
		Href:   "/admin/activity",
		Active: allActive,
	})
	for _, kind := range activityKinds {
		href := "/admin/activity?kind=" + url.QueryEscape(kind.Key)
		links = append(links, activityKindLink{
			Key:    kind.Key,
			Label:  kind.Label,
			Href:   href,
			Active: active[kind.Key],
		})
	}
	return links
}

func activityRows(events []overview.Event, now time.Time) []activityRow {
	rows := make([]activityRow, 0, len(events))
	for _, event := range events {
		pill := event.Status
		if pill == "" {
			pill = event.Visibility
		}
		// "active" is the normal state for members and sodas; a pill on every
		// row would be pure noise. Provisional, suspended, open and the
		// visibility values still earn one.
		if pill == "active" {
			pill = ""
		}
		title := event.Title
		if event.Kind == "user" && title != "" {
			title = "@" + title
		}
		rows = append(rows, activityRow{
			KindLabel: activityKindLabel(event.Kind),
			When:      activityWhen(event.CreatedAt, now),
			Actor:     activityActor(event.ActorHandle),
			Title:     title,
			Detail:    event.Detail,
			Href:      event.Href,
			Pill:      pill,
			PillWarn:  activityPillWarn(pill),
		})
	}
	return rows
}

func activityKindLabel(kind string) string {
	for _, k := range activityKinds {
		if k.Key == kind {
			return k.Label
		}
	}
	return titleCase(kind)
}

func activityActor(handle string) string {
	if handle == "" {
		return "system"
	}
	return "@" + handle
}

// activityWhen renders an RFC3339 timestamp as a short relative label. It
// works in UTC so a viewer's local midnight cannot turn a stale row into
// "yesterday" early.
func activityWhen(iso string, now time.Time) string {
	created, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	created = created.UTC()
	now = now.UTC()
	if created.After(now) {
		return "just now"
	}
	delta := now.Sub(created)
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%d min ago", int(delta/time.Minute))
	}
	if created.Year() == now.Year() && created.YearDay() == now.YearDay() {
		return fmt.Sprintf("%d h ago", int(delta/time.Hour))
	}
	yesterday := now.AddDate(0, 0, -1)
	if created.Year() == yesterday.Year() && created.YearDay() == yesterday.YearDay() {
		return "yesterday"
	}
	return created.Format("2 Jan 2006")
}

// activityPillWarn marks the states that need attention rather than plain
// information.
func activityPillWarn(pill string) bool {
	switch pill {
	case "open", "provisional", "pending", "suspended":
		return true
	default:
		return false
	}
}
