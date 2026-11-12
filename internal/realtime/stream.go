package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// Defaults for a live connection. A caller that wants different timings sets
// the matching StreamOptions field; tests use that to avoid waiting.
const (
	defaultPollInterval      = 60 * time.Second
	defaultHeartbeatInterval = 20 * time.Second
	defaultMaxLifetime       = 30 * time.Minute
)

// Notification is the live shape a stream forwards. It is a signal, not a data
// source: the inbox itself stays server-rendered (web) or fetched over
// /api/v1/notifications (mobile). Href is filled by the caller so every
// transport keeps its own routing rules.
type Notification struct {
	ID        string
	Kind      string
	SubjectID string
	Text      string
	Href      string
	CreatedAt string
	Read      bool
}

// StreamOptions describes one authenticated server-sent-events connection.
type StreamOptions struct {
	// UserID scopes every read. It must be the authenticated viewer.
	UserID string
	// Bus delivers notification IDs as they are written. A nil bus leaves the
	// stream on its reconciliation tick alone.
	Bus *Bus
	// UnreadCount reads the viewer's unread total. Nil disables count events.
	UnreadCount func(context.Context) (int, error)
	// Notification loads one row under the viewer's visibility rules. The bool
	// is false when the row is gone or no longer visible (deleted, blocked
	// actor), in which case it is skipped rather than announced.
	Notification func(context.Context, string) (Notification, bool)
	// Intervals default to 60s / 20s / 30m when zero.
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	MaxLifetime       time.Duration
}

// ServeStream keeps one EventSource connection alive. Delivery is push-first:
// a notification write publishes to the in-process bus and the matching
// connection forwards it immediately. The poll interval is a reconciliation
// cadence, not the delivery path: notification rows are also written by badges
// and a few direct inserts, so re-reading the unread counter periodically keeps
// a dropped bus event from becoming a permanent miss.
//
// The caller authenticates before calling this (session cookie on the web,
// Bearer token on /api/v1) and owns the error response for an unauthenticated
// request: a redirect to a login page is not a valid EventSource response.
func ServeStream(w http.ResponseWriter, r *http.Request, opts StreamOptions) {
	if opts.UserID == "" {
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
	var events <-chan Event
	unsubscribe := func() {}
	if opts.Bus != nil {
		events, unsubscribe = opts.Bus.Subscribe(opts.UserID)
	}
	defer unsubscribe()

	lastCount := -1
	lastWrite := time.Now()

	// Re-check the count immediately so the client sees the current value
	// without waiting for the first tick.
	if opts.UnreadCount != nil {
		if count, err := opts.UnreadCount(ctx); err == nil {
			if !writeUnreadCount(w, rc, count) {
				return
			}
			lastCount = count
			lastWrite = time.Now()
		}
	}

	// syncCount re-reads the counter and writes it only when it changed.
	syncCount := func() bool {
		if opts.UnreadCount == nil {
			return true
		}
		count, err := opts.UnreadCount(ctx)
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

	poll := time.NewTicker(durationOr(opts.PollInterval, defaultPollInterval))
	defer poll.Stop()
	heartbeatInterval := durationOr(opts.HeartbeatInterval, defaultHeartbeatInterval)
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	lifetime := time.NewTimer(durationOr(opts.MaxLifetime, defaultMaxLifetime))
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
			if !writeNotificationEvent(w, rc, ctx, opts, ev.NotificationID) {
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
			if time.Since(lastWrite) < heartbeatInterval {
				continue
			}
			if !writeSSE(w, rc, ": keep-alive\n\n") {
				return
			}
			lastWrite = time.Now()
		}
	}
}

// writeNotificationEvent re-reads the row under the same visibility rules as
// the inbox and writes it as `event: notification`. A missing or now-hidden row
// (deleted, blocked actor) is skipped rather than announced. It reports whether
// the connection can continue.
func writeNotificationEvent(w http.ResponseWriter, rc *http.ResponseController, ctx context.Context, opts StreamOptions, notificationID string) bool {
	if opts.Notification == nil {
		return true
	}
	n, visible := opts.Notification(ctx, notificationID)
	if !visible {
		return true
	}
	payload, err := notificationEventPayload(n)
	if err != nil {
		return true
	}
	return writeSSE(w, rc, payload)
}

type notificationEvent struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	SubjectID string `json:"subject_id,omitempty"`
	Text      string `json:"text"`
	Href      string `json:"href"`
	CreatedAt string `json:"created_at"`
	Read      bool   `json:"read"`
}

func notificationEventPayload(n Notification) (string, error) {
	body, err := json.Marshal(notificationEvent{
		ID:        n.ID,
		Kind:      n.Kind,
		SubjectID: n.SubjectID,
		Text:      n.Text,
		Href:      n.Href,
		CreatedAt: n.CreatedAt,
		Read:      n.Read,
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

func durationOr(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}
