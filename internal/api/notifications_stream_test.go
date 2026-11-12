package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sodapop/internal/realtime"
)

type sseFrame struct {
	event string
	data  string
}

// readFrames scans an SSE body into frames, skipping comments and blank lines.
func readFrames(t *testing.T, body io.Reader) <-chan sseFrame {
	t.Helper()
	ch := make(chan sseFrame, 8)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(body)
		event, data := "", ""
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if event != "" || data != "" {
					ch <- sseFrame{event: event, data: data}
				}
				event, data = "", ""
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()
	return ch
}

func nextFrame(t *testing.T, ch <-chan sseFrame) sseFrame {
	t.Helper()
	select {
	case frame, ok := <-ch:
		if !ok {
			t.Fatal("stream ended before the expected frame")
		}
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an SSE frame")
	}
	return sseFrame{}
}

func TestNotificationsStreamRequiresToken(t *testing.T) {
	h := newHarness(t)

	rec := h.doWith(t, "", http.MethodGet, "/api/v1/notifications/stream", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestNotificationsStreamPushesNotification(t *testing.T) {
	h := newHarness(t)

	bus := realtime.New()
	h.server.Realtime = bus
	h.server.Social.OnInsert = func(_ context.Context, userID, notificationID string) {
		bus.Publish(realtime.Event{UserID: userID, NotificationID: notificationID})
	}

	srv := httptest.NewServer(h.mux)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/notifications/stream", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}

	frames := readFrames(t, resp.Body)

	// The initial snapshot also proves the subscription happened first: the
	// handler subscribes before it writes this frame.
	initial := nextFrame(t, frames)
	if initial.event != "unread" {
		t.Fatalf("first event = %q, want unread", initial.event)
	}
	if initial.data != `{"count":0}` {
		t.Fatalf("initial data = %q, want count 0", initial.data)
	}

	if err := h.server.Social.Notify(context.Background(), h.alice, "comment", h.bob, "", "commented on your check-in"); err != nil {
		t.Fatalf("notify: %v", err)
	}

	var row map[string]any
	for {
		frame := nextFrame(t, frames)
		if frame.event != "notification" {
			continue
		}
		if err := json.Unmarshal([]byte(frame.data), &row); err != nil {
			t.Fatalf("decode notification %q: %v", frame.data, err)
		}
		break
	}
	if row["kind"] != "comment" {
		t.Fatalf("kind = %v, want comment", row["kind"])
	}
	if row["text"] != "commented on your check-in" {
		t.Fatalf("text = %v", row["text"])
	}
	if row["href"] != "/" {
		t.Fatalf("href = %v, want /", row["href"])
	}

	// The same publish drives the badge count.
	after := nextFrame(t, frames)
	if after.event != "unread" || after.data != `{"count":1}` {
		t.Fatalf("follow-up = %q %q, want unread count 1", after.event, after.data)
	}
}
