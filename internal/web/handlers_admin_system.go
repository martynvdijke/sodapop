package web

import (
	"context"
	"database/sql"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"sodapop/internal/backup"
)

// startedAt anchors the uptime shown on the admin system page.
var startedAt = time.Now()

// adminSystemPage renders instance diagnostics, scheduler state and snapshots.
// It deliberately never exposes secrets: only paths, sizes, versions and
// aggregate counts leave the handler.
func (s *Server) adminSystemPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, _ := s.DB.Stats(ctx)
	dbPath := stats.Path
	if dbPath == "" && s.Cfg != nil {
		dbPath = s.Cfg.DBPath
	}

	backupDir := ""
	var backups []backup.File
	lastBackup := "Never"
	if s.Backup != nil {
		backupDir = s.Backup.Dir
		if list, err := s.Backup.List(); err == nil {
			backups = list
			if len(list) > 0 {
				lastBackup = list[0].CreatedAt.Format(time.RFC3339)
			}
		}
		if last, err := s.Backup.LastRun(ctx); err == nil && last != "" {
			lastBackup = last
		}
	}

	outboxDepth := 0
	if s.Integrations != nil {
		if n, err := s.Integrations.OutboxDepth(ctx); err == nil {
			outboxDepth = n
		}
	}

	counts := []struct{ Label, Value string }{}
	for _, row := range []struct {
		label string
		count func(context.Context) (int, error)
	}{
		{"Users", s.DB.Client.User.Query().Count},
		{"Drinks", s.DB.Client.Drink.Query().Count},
		{"Checkins", s.DB.Client.Checkin.Query().Count},
		{"Venues", s.DB.Client.Venue.Query().Count},
		{"Comments", s.DB.Client.Comment.Query().Count},
		{"Badges", s.DB.Client.Badge.Query().Count},
	} {
		n, err := row.count(ctx)
		if err != nil {
			continue
		}
		counts = append(counts, struct{ Label, Value string }{row.label, strconv.Itoa(n)})
	}

	schedulers := []struct{ Name, LastRunAt, LockedUntil, LockOwner string }{}
	if rows, err := s.DB.SQL.QueryContext(ctx,
		`SELECT name, last_run_at, locked_until, lock_owner FROM scheduler_state ORDER BY name`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var name, owner string
			var lastRun, locked sql.NullString
			if err := rows.Scan(&name, &lastRun, &locked, &owner); err != nil {
				break
			}
			schedulers = append(schedulers, struct{ Name, LastRunAt, LockedUntil, LockOwner string }{
				Name:        name,
				LastRunAt:   lastRun.String,
				LockedUntil: locked.String,
				LockOwner:   owner,
			})
		}
	}

	notice, errText := "", ""
	switch r.URL.Query().Get("backup") {
	case "ok":
		notice = "Backup created."
	case "error":
		errText = "Backup could not be created. Check the server log."
	}

	csrf := ""
	if sess := SessionFromContext(r); sess != nil {
		csrf = sess.CsrfToken
	}

	version := ""
	if s.Cfg != nil {
		version = s.Cfg.Version
	}

	s.render(w, r, http.StatusOK, "admin_system", map[string]any{
		"Title":            "System",
		"Version":          version,
		"Uptime":           time.Since(startedAt).Round(time.Second).String(),
		"GoVersion":        runtime.Version(),
		"GOOS":             runtime.GOOS,
		"GOARCH":           runtime.GOARCH,
		"DBPath":           dbPath,
		"DBSize":           stats.Size,
		"WALSize":          stats.WALSize,
		"MigrationVersion": stats.SchemaVersion,
		"MigrationName":    stats.SchemaName,
		"BackupDir":        backupDir,
		"BackupCount":      len(backups),
		"LastBackup":       lastBackup,
		"OutboxDepth":      outboxDepth,
		"Counts":           counts,
		"Schedulers":       schedulers,
		"Backups":          backups,
		"CSRF":             csrf,
		"Notice":           notice,
		"Error":            errText,
	})
}

// adminBackupCreate takes an on-demand snapshot. Errors are reported through
// a generic redirect so filesystem details never reach the browser.
func (s *Server) adminBackupCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	if s.Backup == nil {
		http.Redirect(w, r, "/admin/system?backup=error", http.StatusFound)
		return
	}
	if _, err := s.Backup.Create(r.Context()); err != nil {
		http.Redirect(w, r, "/admin/system?backup=error", http.StatusFound)
		return
	}
	if _, err := s.Backup.Prune(); err != nil {
		s.Log.Warn("admin backup prune failed", "err", err)
	}
	http.Redirect(w, r, "/admin/system?backup=ok", http.StatusFound)
}
