package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sodapop/ent"
)

// startNotificationStream exposes the stream handler with a signed-in user and
// returns a buffered reader over the live response. Closing the body cancels the
// request context, so the handler exits.
func startNotificationStream(t *testing.T, h *v12Harness, u *ent.User) *bufio.Reader {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userKey{}, u)
		h.srv.notificationsStream(w, r.WithContext(ctx))
	}))
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Get(ts.URL)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache control = %q, want no-store", cc)
	}
	if xab := resp.Header.Get("X-Accel-Buffering"); xab != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", xab)
	}
	return bufio.NewReader(resp.Body)
}

// readSSEEvent reads one event frame (terminated by a blank line) and fails if
// nothing arrives within timeout.
func readSSEEvent(t *testing.T, br *bufio.Reader, timeout time.Duration) string {
	t.Helper()
	type result struct {
		text string
		err  error
	}
	frames := make(chan result, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				frames <- result{text: b.String(), err: err}
				return
			}
			if line == "\n" || line == "\r\n" {
				frames <- result{text: b.String()}
				return
			}
			b.WriteString(line)
		}
	}()

	select {
	case frame := <-frames:
		if frame.err != nil {
			t.Fatalf("read event: %v (partial %q)", frame.err, frame.text)
		}
		return frame.text
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for an SSE event after %s", timeout)
		return ""
	}
}

func TestNotificationsStreamRequiresUserWithoutRedirect(t *testing.T) {
	h := newV12Harness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil)
	h.srv.notificationsStream(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %q)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("unauthenticated stream redirected to %q", loc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type = %q, want application/json", ct)
	}
}

func TestNotificationsStreamSendsInitialCount(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "stream-initial")
	ctx := context.Background()
	if err := h.srv.Social.Notify(ctx, u.ID, "toast", "", "", "toasted your check-in"); err != nil {
		t.Fatalf("seed toast: %v", err)
	}
	if err := h.srv.Social.Notify(ctx, u.ID, "comment", "", "", "commented on your check-in"); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	br := startNotificationStream(t, h, u)
	frame := readSSEEvent(t, br, 5*time.Second)
	if !strings.Contains(frame, "event: unread") {
		t.Fatalf("first event = %q, want event: unread", frame)
	}
	if !strings.Contains(frame, `"count":2`) {
		t.Fatalf("first event = %q, want count 2", frame)
	}
}

func TestNotificationsStreamPushesCountChange(t *testing.T) {
	previous := streamPollInterval
	streamPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { streamPollInterval = previous })

	h := newV12Harness(t)
	u := h.user(t, "stream-change")
	ctx := context.Background()
	if err := h.srv.Social.Notify(ctx, u.ID, "toast", "", "", "toasted your check-in"); err != nil {
		t.Fatalf("seed toast: %v", err)
	}

	br := startNotificationStream(t, h, u)
	first := readSSEEvent(t, br, 5*time.Second)
	if !strings.Contains(first, `"count":1`) {
		t.Fatalf("first event = %q, want count 1", first)
	}

	// The next poll tick must observe the insert and emit a new frame without
	// waiting for the production 15-second interval.
	if err := h.srv.Social.Notify(ctx, u.ID, "comment", "", "", "commented on your check-in"); err != nil {
		t.Fatalf("insert notification: %v", err)
	}
	second := readSSEEvent(t, br, 5*time.Second)
	if !strings.Contains(second, "event: unread") {
		t.Fatalf("second event = %q, want event: unread", second)
	}
	if !strings.Contains(second, `"count":2`) {
		t.Fatalf("second event = %q, want count 2", second)
	}
}
