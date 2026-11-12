package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"sodapop/ent"
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

	// The global server WriteTimeout is 60s, which would kill every stream.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return
	}

	ctx := r.Context()

	// Subscribe before the first read so a row written between the snapshot and
	// the loop is still delivered. A nil bus leaves the channel nil, which the
	// select simply never selects; the reconcile tick still keeps the badge
	// current.
	var events <-chan realtime.Event
	unsubscribe := func() {}
	if s.Realtime != nil {
		events, unsubscribe = s.Realtime.Subscribe(u.ID)
	}
	defer unsubscribe()

	lastCount := -1
	lastWrite := time.Now()

	// Re-check the count immediately so the client sees the current value
	// without waiting for the first tick.
	if count, err := s.Social.UnreadCount(ctx, u.ID); err == nil {
		if !writeUnreadCount(w, rc, count) {
			return
		}
		lastCount = count
		lastWrite = time.Now()
	}

	// syncCount re-reads the counter and writes it only when it changed.
	syncCount := func() bool {
		count, err := s.Social.UnreadCount(ctx, u.ID)
		if err != nil {
			return true
		}
		if count == lastCount {
			return true
		}
		if !writeUnreadCount(w, rc, count) {
			return false
		}
		lastCount = count
		lastWrite = time.Now()
		return true
	}

	poll := time.NewTicker(streamPollInterval)
	defer poll.Stop()
	heartbeat := time.NewTicker(streamHeartbeatInterval)
	defer heartbeat.Stop()
	lifetime := time.NewTimer(streamMaxLifetime)
	defer lifetime.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-lifetime.C:
			// Ask the client to reconnect so no connection and goroutine live
			// forever behind a proxy.
			writeSSE(w, rc, "event: close\ndata: {}\n\n")
			return
		case ev, ok := <-events:
			if !ok {
				// The bus closed this subscription; keep serving via the tick.
				events = nil
				continue
			}
			if !s.writeNotificationEvent(w, rc, ctx, u.ID, ev.NotificationID) {
				return
			}
			if !syncCount() {
				return
			}
		case <-poll.C:
			if !syncCount() {
				return
			}
		case <-heartbeat.C:
			// Keep intermediaries from closing an idle connection, but only
			// when nothing was sent since the previous heartbeat.
			if time.Since(lastWrite) < streamHeartbeatInterval {
				continue
			}
			if !writeSSE(w, rc, ": keep-alive\n\n") {
				return
			}
			lastWrite = time.Now()
		}
	}
}

// notificationEvent mirrors the fields a live client needs to announce a row.
// It is a signal, not a data source: the inbox itself stays server-rendered.
type notificationEvent struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	SubjectID string `json:"subject_id,omitempty"`
	Text      string `json:"text"`
	Href      string `json:"href"`
	CreatedAt string `json:"created_at"`
	Read      bool   `json:"read"`
}

// writeNotificationEvent re-reads the row under the same visibility rules as the
// inbox and writes it as `event: notification`. A missing or now-hidden row
// (deleted, blocked actor) is skipped rather than announced. It reports whether
// the connection can continue.
func (s *Server) writeNotificationEvent(w http.ResponseWriter, rc *http.ResponseController, ctx context.Context, userID, notificationID string) bool {
	row, err := s.Social.NotificationForViewer(ctx, userID, notificationID)
	if err != nil || row == nil {
		return true
	}
	payload, err := notificationEventPayload(row)
	if err != nil {
		return true
	}
	return writeSSE(w, rc, payload)
}

func notificationEventPayload(row *ent.Notification) (string, error) {
	body, err := json.Marshal(notificationEvent{
		ID:        row.ID,
		Kind:      string(row.Kind),
		SubjectID: row.SubjectID,
		Text:      row.Text,
		Href:      notificationHref(string(row.Kind), row.SubjectID),
		CreatedAt: row.CreatedAt,
		Read:      row.ReadAt != nil,
	})
	if err != nil {
		return "", err
	}
	return "event: notification\ndata: " + string(body) + "\n\n", nil
}

func writeUnreadCount(w http.ResponseWriter, rc *http.ResponseController, count int) bool {
	return writeSSE(w, rc, "event: unread\ndata: {\"count\":"+strconv.Itoa(count)+"}\n\n")
}

func writeSSE(w http.ResponseWriter, rc *http.ResponseController, payload string) bool {
	if _, err := w.Write([]byte(payload)); err != nil {
		return false
	}
	return rc.Flush() == nil
}
