// Package overview builds the admin activity feed: one keyset-paginated
// union over the moderation-relevant tables plus a small counters snapshot.
package overview

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Event is one entry in the admin activity feed.
type Event struct {
	ID          string `json:"id"` // "<kind>:<pk>"
	Kind        string `json:"kind"`
	CreatedAt   string `json:"created_at"`
	ActorID     string `json:"actor_id,omitempty"`
	ActorHandle string `json:"actor_handle,omitempty"`
	ActorName   string `json:"actor_name,omitempty"`
	Title       string `json:"title"`
	Detail      string `json:"detail,omitempty"`
	Status      string `json:"status,omitempty"`
	Visibility  string `json:"visibility,omitempty"`
	SubjectID   string `json:"subject_id,omitempty"`
	Href        string `json:"href,omitempty"` // web path
}

// Filter narrows one page of the activity feed.
type Filter struct {
	Kinds  []string // whitelist; empty = all seven
	Before string   // opaque cursor "<created_at>|<id>", exclusive
	Limit  int      // default 50, cap 100
}

// Counts is the small at-a-glance summary shown above the activity feed.
type Counts struct {
	Checkins24h   int `json:"checkins_24h"`
	Drinks24h     int `json:"drinks_24h"`
	Users7d       int `json:"users_7d"`
	OpenReports   int `json:"open_reports"`
	PendingDrinks int `json:"pending_drinks"`
}

// ErrInvalidCursor means Filter.Before was not a "<created_at>|<id>" pair.
var ErrInvalidCursor = errors.New("invalid cursor")

// Service reads the activity overview from the raw SQLite handle.
type Service struct {
	SQL *sql.DB
}

func New(sql *sql.DB) *Service { return &Service{SQL: sql} }

// kindOrder is the fixed whitelist order. It also doubles as the set of
// accepted kind names; nothing outside it is ever interpolated into SQL.
var kindOrder = []string{"checkin", "drink", "user", "venue", "report", "comment", "toast"}

// branches holds one fixed SQL fragment per source. Every fragment emits the
// same twelve columns in the same order.
var branches = map[string]string{
	"checkin": `
		SELECT 'checkin' AS kind, c.created_at AS created_at,
		       'checkin:' || c.id AS id,
		       c.user_id AS actor_id,
		       COALESCE(u.handle, '') AS actor_handle,
		       COALESCE(u.display_name, '') AS actor_name,
		       d.name AS title,
		       '' AS detail,
		       '' AS status,
		       c.visibility AS visibility,
		       c.id AS subject_id,
		       '/checkins/' || c.id AS href
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		JOIN users u ON u.id = c.user_id
		WHERE c.deleted_at IS NULL`,
	"drink": `
		SELECT 'drink' AS kind, d.created_at AS created_at,
		       'drink:' || d.id AS id,
		       COALESCE(d.created_by, '') AS actor_id,
		       COALESCE(u.handle, '') AS actor_handle,
		       COALESCE(u.display_name, '') AS actor_name,
		       d.name AS title,
		       COALESCE(b.name, '') AS detail,
		       d.status AS status,
		       '' AS visibility,
		       CAST(d.id AS TEXT) AS subject_id,
		       '/drinks/' || d.slug AS href
		FROM drinks d
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN users u ON u.id = d.created_by`,
	"user": `
		SELECT 'user' AS kind, u.created_at AS created_at,
		       'user:' || u.id AS id,
		       u.id AS actor_id,
		       u.handle AS actor_handle,
		       COALESCE(u.display_name, '') AS actor_name,
		       u.handle AS title,
		       COALESCE(u.display_name, '') AS detail,
		       u.status AS status,
		       '' AS visibility,
		       '' AS subject_id,
		       '/u/' || u.handle AS href
		FROM users u
		WHERE u.deleted_at IS NULL`,
	"venue": `
		SELECT 'venue' AS kind, v.created_at AS created_at,
		       'venue:' || v.id AS id,
		       COALESCE(v.created_by, '') AS actor_id,
		       COALESCE(u.handle, '') AS actor_handle,
		       COALESCE(u.display_name, '') AS actor_name,
		       v.name AS title,
		       v.kind || CASE WHEN v.city <> '' THEN ', ' || v.city ELSE '' END AS detail,
		       '' AS status,
		       '' AS visibility,
		       '' AS subject_id,
		       '/venues/' || v.id AS href
		FROM venues v
		LEFT JOIN users u ON u.id = v.created_by
		WHERE v.merged_into_id IS NULL`,
	"report": `
		SELECT 'report' AS kind, r.created_at AS created_at,
		       'report:' || r.id AS id,
		       COALESCE(r.user_id, '') AS actor_id,
		       COALESCE(u.handle, '') AS actor_handle,
		       COALESCE(u.display_name, '') AS actor_name,
		       r.entity AS title,
		       substr(CASE WHEN r.reason <> '' THEN r.reason ELSE r.details END, 1, 140) AS detail,
		       r.status AS status,
		       '' AS visibility,
		       '' AS subject_id,
		       '/admin' AS href
		FROM reports r
		LEFT JOIN users u ON u.id = r.user_id`,
	"comment": `
		SELECT 'comment' AS kind, cm.created_at AS created_at,
		       'comment:' || cm.id AS id,
		       cm.user_id AS actor_id,
		       COALESCE(u.handle, '') AS actor_handle,
		       COALESCE(u.display_name, '') AS actor_name,
		       substr(cm.body, 1, 120) AS title,
		       'on ' || d.name AS detail,
		       '' AS status,
		       '' AS visibility,
		       '' AS subject_id,
		       '/checkins/' || cm.checkin_id || '#comments' AS href
		FROM comments cm
		JOIN checkins c ON c.id = cm.checkin_id
		JOIN users u ON u.id = cm.user_id
		JOIN drinks d ON d.id = c.drink_id
		WHERE cm.deleted_at IS NULL AND c.deleted_at IS NULL`,
	"toast": `
		SELECT 'toast' AS kind, l.created_at AS created_at,
		       'toast:' || l.checkin_id || ':' || l.user_id AS id,
		       l.user_id AS actor_id,
		       COALESCE(u.handle, '') AS actor_handle,
		       COALESCE(u.display_name, '') AS actor_name,
		       d.name AS title,
		       'toasted' AS detail,
		       '' AS status,
		       '' AS visibility,
		       '' AS subject_id,
		       '/checkins/' || l.checkin_id AS href
		FROM checkin_likes l
		JOIN checkins c ON c.id = l.checkin_id
		JOIN users u ON u.id = l.user_id
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.deleted_at IS NULL`,
}

// List returns one newest-first page of activity events. nextBefore is the
// cursor for the following page and is empty when the page is the last one.
func (s *Service) List(ctx context.Context, f Filter) ([]Event, string, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	selected, ok := selectKinds(f.Kinds)
	if !ok {
		return []Event{}, "", nil
	}

	parts := make([]string, 0, len(selected))
	for _, kind := range kindOrder {
		if selected[kind] {
			parts = append(parts, branches[kind])
		}
	}
	q := "SELECT * FROM (" + strings.Join(parts, "\nUNION ALL\n") + ") e"

	args := make([]any, 0, 4)
	if f.Before != "" {
		createdAt, id, err := splitCursor(f.Before)
		if err != nil {
			return nil, "", err
		}
		q += ` WHERE (e.created_at < ? OR (e.created_at = ? AND e.id < ?))`
		args = append(args, createdAt, createdAt, id)
	}
	q += ` ORDER BY e.created_at DESC, e.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	items := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Kind, &e.CreatedAt, &e.ID, &e.ActorID, &e.ActorHandle,
			&e.ActorName, &e.Title, &e.Detail, &e.Status, &e.Visibility,
			&e.SubjectID, &e.Href); err != nil {
			return nil, "", err
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		next = last.CreatedAt + "|" + last.ID
	}
	return items, next, nil
}

// Counts returns the fixed activity summary. Windows are evaluated against
// the current UTC time at query time.
func (s *Service) Counts(ctx context.Context) (Counts, error) {
	now := time.Now().UTC()
	since24h := now.Add(-24 * time.Hour).Format(time.RFC3339)
	since7d := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339)

	var c Counts
	err := s.SQL.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM checkins WHERE deleted_at IS NULL AND created_at >= ?),
			(SELECT COUNT(*) FROM drinks WHERE created_at >= ?),
			(SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND created_at >= ?),
			(SELECT COUNT(*) FROM reports WHERE status = 'open'),
			(SELECT COUNT(*) FROM drinks WHERE status = 'provisional')`,
		since24h, since24h, since7d,
	).Scan(&c.Checkins24h, &c.Drinks24h, &c.Users7d, &c.OpenReports, &c.PendingDrinks)
	if err != nil {
		return Counts{}, err
	}
	return c, nil
}

// selectKinds resolves the caller's whitelist against the fixed set. An
// empty request means every source. Unknown names are ignored; if nothing
// recognised remains, the caller gets an empty page rather than everything.
func selectKinds(raw []string) (map[string]bool, bool) {
	if len(raw) == 0 {
		all := make(map[string]bool, len(kindOrder))
		for _, kind := range kindOrder {
			all[kind] = true
		}
		return all, true
	}
	valid := make(map[string]bool, len(kindOrder))
	for _, kind := range kindOrder {
		valid[kind] = true
	}
	out := make(map[string]bool)
	for _, rawKind := range raw {
		kind := strings.ToLower(strings.TrimSpace(rawKind))
		if valid[kind] {
			out[kind] = true
		}
	}
	return out, len(out) > 0
}

func splitCursor(raw string) (createdAt, id string, err error) {
	createdAt, id, ok := strings.Cut(raw, "|")
	if !ok || createdAt == "" || id == "" {
		return "", "", ErrInvalidCursor
	}
	if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
		return "", "", ErrInvalidCursor
	}
	return createdAt, id, nil
}
