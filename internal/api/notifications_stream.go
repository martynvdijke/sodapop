package api

import (
	"context"
	"net/http"

	"sodapop/internal/auth"
	"sodapop/internal/realtime"
)

// notificationsStream is the token-authenticated twin of the web EventSource.
// The mobile app cannot use the cookie-scoped /notifications/stream, so this
// route runs the same server-sent-events loop behind a Bearer token. The
// payload matches the web frames: `unread` carries the badge count and
// `notification` carries one visible row.
func (s *Server) notificationsStream(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	realtime.ServeStream(w, r, realtime.StreamOptions{
		UserID:      tok.User.ID,
		Bus:         s.Realtime,
		UnreadCount: func(ctx context.Context) (int, error) { return s.Social.UnreadCount(ctx, tok.User.ID) },
		Notification: func(ctx context.Context, notificationID string) (realtime.Notification, bool) {
			row, err := s.Social.NotificationForViewer(ctx, tok.User.ID, notificationID)
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
		},
	})
	// ServeStream owns the response once it starts; there is no error body to
	// write and appending one here would corrupt the event stream.
	return nil
}
