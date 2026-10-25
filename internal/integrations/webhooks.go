package integrations

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/integration"
)

// HomeAssistant sends a test notification to the user's Home Assistant
// webhook.
func (s *Service) HomeAssistant(ctx context.Context, userID string) error {
	row, err := s.Get(ctx, userID, string(integration.KindHomeAssistant))
	if err != nil {
		return err
	}
	return s.sendHomeAssistant(ctx, row, Event{
		UserID: userID,
		Kind:   "test",
		Text:   "Sodapop integration test",
		URL:    s.baseURL(),
	})
}

type homeAssistantPayload struct {
	Event string `json:"event"`
	Text  string `json:"text"`
	URL   string `json:"url"`
}

func (s *Service) sendHomeAssistant(ctx context.Context, row *ent.Integration, e Event) error {
	target := strings.TrimSpace(row.Target)
	if target == "" {
		return errNoTarget()
	}
	body, err := json.Marshal(homeAssistantPayload{Event: e.Kind, Text: e.Text, URL: e.URL})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.do(req)
}

// Matrix sends a test notification to the user's Matrix room.
func (s *Service) Matrix(ctx context.Context, userID string) error {
	row, err := s.Get(ctx, userID, string(integration.KindMatrix))
	if err != nil {
		return err
	}
	return s.sendMatrix(ctx, row, Event{
		UserID: userID,
		Kind:   "test",
		Text:   "Sodapop integration test",
		URL:    s.baseURL(),
	})
}

type matrixMessage struct {
	MsgType string `json:"msgtype"`
	Body    string `json:"body"`
}

func (s *Service) sendMatrix(ctx context.Context, row *ent.Integration, e Event) error {
	homeserver, room, ok := strings.Cut(row.Target, "|")
	if !ok {
		return errors.New("matrix target must be homeserver|room_id")
	}
	homeserver = strings.TrimRight(strings.TrimSpace(homeserver), "/")
	room = strings.TrimSpace(room)
	if homeserver == "" || room == "" {
		return errors.New("matrix target must be homeserver|room_id")
	}
	text := strings.TrimSpace(e.Text)
	if text == "" {
		text = "Sodapop update"
	}
	body, err := json.Marshal(matrixMessage{MsgType: "m.text", Body: text})
	if err != nil {
		return err
	}
	endpoint := homeserver + "/_matrix/client/v3/rooms/" + url.PathEscape(room) +
		"/send/m.room.message/" + txnID()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if row.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+row.Secret)
	}
	return s.do(req)
}

// do executes a request and treats non-2xx responses as failures. Response
// bodies are truncated so a broken endpoint cannot flood the logs.
func (s *Service) do(req *http.Request) error {
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		return fmt.Errorf("%s %s: %s", req.Method, req.URL.Host, resp.Status)
	}
	return fmt.Errorf("%s %s: %s: %s", req.Method, req.URL.Host, resp.Status, msg)
}

func txnID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("sodapop-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
