package web

import (
	"net/http"
	"strconv"
	"time"
)

// The stream polls the unread counter instead of subscribing to an event bus:
// notification writes are spread across social, badges and direct inserts, so a
// per-connection poll of the source of truth is the only reliable signal.
var (
	streamPollInterval      = 15 * time.Second
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
	lastCount := -1
	lastWrite := time.Now()

	// Re-check the count immediately so the client sees the current value
	// without waiting for the first poll tick.
	if count, err := s.Social.UnreadCount(ctx, u.ID); err == nil {
		if !writeUnreadCount(w, rc, count) {
			return
		}
		lastCount = count
		lastWrite = time.Now()
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
		case <-poll.C:
			count, err := s.Social.UnreadCount(ctx, u.ID)
			if err != nil {
				continue
			}
			if count == lastCount {
				continue
			}
			if !writeUnreadCount(w, rc, count) {
				return
			}
			lastCount = count
			lastWrite = time.Now()
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

func writeUnreadCount(w http.ResponseWriter, rc *http.ResponseController, count int) bool {
	return writeSSE(w, rc, "event: unread\ndata: {\"count\":"+strconv.Itoa(count)+"}\n\n")
}

func writeSSE(w http.ResponseWriter, rc *http.ResponseController, payload string) bool {
	if _, err := w.Write([]byte(payload)); err != nil {
		return false
	}
	return rc.Flush() == nil
}
