package web

import (
	"context"
	"net/http"
	"time"

	"sodapop/internal/realtime"
)

// The stream is push-first: a notification write publishes to the in-process
// bus and the matching connection forwards it immediately. The poll interval is
// a reconciliation cadence, not the delivery path: notification rows are also
// written by badges and a few direct inserts, so re-reading the unread counter
// periodically keeps a dropped bus event from becoming a permanent miss.
var (
	streamPollInterval      = 60 * time.Second
	streamHeartbeatInterval = 20 * time.Second
	streamMaxLifetime       = 30 * time.Minute
)

// notificationsStream keeps one EventSource connection alive and pushes the
// viewer's unread notification count whenever it changes. It deliberately does
// not use requireUser: a redirect to /login is not a valid EventSource response,
// so an anonymous request gets a plain 401 instead.
func (s *Server) notificationsStream(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	if u == nil {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}

	realtime.ServeStream(w, r, realtime.StreamOptions{
		UserID:            u.ID,
		Bus:               s.Realtime,
		UnreadCount:       func(ctx context.Context) (int, error) { return s.Social.UnreadCount(ctx, u.ID) },
		Notification:      s.liveNotification(u.ID),
		PollInterval:      streamPollInterval,
		HeartbeatInterval: streamHeartbeatInterval,
		MaxLifetime:       streamMaxLifetime,
	})
}

// liveNotification loads one row under the viewer's visibility rules and fills
// the web href, which differs from the mobile app's routing.
func (s *Server) liveNotification(userID string) func(context.Context, string) (realtime.Notification, bool) {
	return func(ctx context.Context, notificationID string) (realtime.Notification, bool) {
		row, err := s.Social.NotificationForViewer(ctx, userID, notificationID)
		if err != nil || row == nil {
			return realtime.Notification{}, false
		}
		return realtime.Notification{
			ID:        row.ID,
			Kind:      string(row.Kind),
			SubjectID: row.SubjectID,
			Text:      row.Text,
			Href:      notificationHref(string(row.Kind), row.SubjectID),
			CreatedAt: row.CreatedAt,
			Read:      row.ReadAt != nil,
		}, true
	}
}
