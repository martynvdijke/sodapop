package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sodapop/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testService(t *testing.T, handler http.HandlerFunc) (*Service, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Service{
		HTTP:    server.Client(),
		Cfg:     &config.Config{Version: "0.0.0-test", SessionKey: []byte("test-session-secret")},
		Version: "1.4.2",
		Log:     discardLogger(),
	}, server
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain", in: "https://opencode.ai/zen/go/v1", want: "https://opencode.ai/zen/go/v1"},
		{name: "trailing slash", in: "https://opencode.ai/zen/go/v1/", want: "https://opencode.ai/zen/go/v1"},
		{name: "chat suffix", in: "https://opencode.ai/zen/go/v1/chat/completions", want: "https://opencode.ai/zen/go/v1"},
		{name: "chat suffix slash", in: "https://openrouter.ai/api/v1/chat/completions/", want: "https://openrouter.ai/api/v1"},
		{name: "images suffix", in: "https://api.example.com/v1/images/generations", want: "https://api.example.com/v1"},
		{name: "completions suffix", in: "https://api.example.com/v1/completions", want: "https://api.example.com/v1"},
		{name: "repeated suffix", in: "https://api.example.com/v1/chat/completions/chat/completions", want: "https://api.example.com/v1"},
		{name: "whitespace and query", in: "  https://api.example.com/v1/chat/completions?x=1  ", want: "https://api.example.com/v1"},
		{name: "host only", in: "http://localhost:11434", want: "http://localhost:11434"},
		{name: "uppercase scheme", in: "HTTPS://api.example.com/v1", want: "https://api.example.com/v1"},
		{name: "empty", in: "   ", wantErr: true},
		{name: "no scheme", in: "api.example.com/v1", wantErr: true},
		{name: "wrong scheme", in: "ftp://api.example.com/v1", wantErr: true},
		{name: "no host", in: "https:///v1", wantErr: true},
		{name: "userinfo", in: "https://user:pass@api.example.com/v1", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeBaseURL(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidBaseURL) {
					t.Fatalf("NormalizeBaseURL(%q) error = %v, want ErrInvalidBaseURL", tc.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeBaseURL(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRequestURLAddsOperationOnce(t *testing.T) {
	got := requestURL("https://api.example.com/v1/chat/completions", "/chat/completions")
	if want := "https://api.example.com/v1/chat/completions"; got != want {
		t.Fatalf("requestURL = %q, want %q", got, want)
	}
}

func TestChatSendsHeadersAndParsesResponse(t *testing.T) {
	type captured struct {
		method      string
		path        string
		userAgent   string
		session     string
		auth        string
		contentType string
		payload     map[string]any
	}
	var got captured

	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		got.userAgent = r.Header.Get("User-Agent")
		got.session = r.Header.Get("x-opencode-session")
		got.auth = r.Header.Get("Authorization")
		got.contentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got.payload)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"model": "opencode/grok-code",
			"choices": [{"message": {"content": "A crisp lime soda."}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 12, "completion_tokens": 5, "total_tokens": 17}
		}`)
	})

	t.Setenv("AI_BASE_URL", server.URL+"/v1/chat/completions/")
	t.Setenv("AI_API_KEY", "env-secret-key")
	t.Setenv("AI_MODEL", "opencode/grok-code")

	resp, err := service.Chat(context.Background(), ChatRequest{
		Messages:    []ChatMessage{{Role: "system", Content: "Be brief."}, {Role: "user", Content: "Describe a lime soda."}},
		MaxTokens:   800,
		Temperature: 0.5,
		SessionID:   "session-abc",
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if got.path != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", got.path)
	}
	if got.userAgent != "sodapop/1.4.2" {
		t.Errorf("User-Agent = %q, want sodapop/1.4.2", got.userAgent)
	}
	if got.session != "session-abc" {
		t.Errorf("x-opencode-session = %q, want session-abc", got.session)
	}
	if got.auth != "Bearer env-secret-key" {
		t.Errorf("Authorization = %q", got.auth)
	}
	if got.contentType != "application/json" {
		t.Errorf("Content-Type = %q", got.contentType)
	}
	if got.payload["model"] != "opencode/grok-code" {
		t.Errorf("payload model = %v", got.payload["model"])
	}
	if got.payload["max_tokens"] != float64(800) {
		t.Errorf("payload max_tokens = %v", got.payload["max_tokens"])
	}
	messages, _ := got.payload["messages"].([]any)
	if len(messages) != 2 {
		t.Errorf("payload messages = %v", got.payload["messages"])
	}

	if resp.Content != "A crisp lime soda." {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.SessionID != "session-abc" {
		t.Errorf("SessionID = %q", resp.SessionID)
	}
	if resp.Model != "opencode/grok-code" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.Usage["total_tokens"] != 17 {
		t.Errorf("Usage = %v", resp.Usage)
	}
}

func TestChatGeneratesSessionIDWhenMissing(t *testing.T) {
	var header string
	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("x-opencode-session")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	})
	t.Setenv("AI_BASE_URL", server.URL+"/v1")

	resp, err := service.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if len(resp.SessionID) != 32 {
		t.Errorf("generated SessionID = %q, want 32 hex chars", resp.SessionID)
	}
	if header != resp.SessionID {
		t.Errorf("header %q != response session %q", header, resp.SessionID)
	}
}

func TestChatJSONMode(t *testing.T) {
	var payload map[string]any
	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
	})
	t.Setenv("AI_BASE_URL", server.URL)

	if _, err := service.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "give json"}},
		JSON:     true,
	}); err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	format, ok := payload["response_format"].(map[string]any)
	if !ok || format["type"] != "json_object" {
		t.Fatalf("response_format = %v, want json_object", payload["response_format"])
	}
}

func TestChatTruncation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "finish length", body: `{"choices":[{"message":{"content":"partial answer"},"finish_reason":"length"}]}`},
		{name: "empty content", body: `{"choices":[{"message":{"content":"   "},"finish_reason":"stop"}]}`},
		{name: "no choices", body: `{"choices":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.body)
			})
			t.Setenv("AI_BASE_URL", server.URL)

			_, err := service.Chat(context.Background(), ChatRequest{
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			})
			if !errors.Is(err, ErrTruncated) {
				t.Fatalf("Chat error = %v, want ErrTruncated", err)
			}
			if err.Error() != "ai_output_truncated" {
				t.Fatalf("error text = %q, want ai_output_truncated", err.Error())
			}
		})
	}
}

func TestChatNon2xx(t *testing.T) {
	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream exploded", http.StatusBadGateway)
	})
	t.Setenv("AI_BASE_URL", server.URL)

	_, err := service.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("Chat error = %v, want upstream status", err)
	}
}

func TestChatDisabled(t *testing.T) {
	t.Setenv("AI_BASE_URL", "")
	service := &Service{Log: discardLogger()}

	_, err := service.Chat(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("Chat error = %v, want ErrNotEnabled", err)
	}
}

func TestTestEndpoint(t *testing.T) {
	var got struct {
		path string
		auth string
		ua   string
	}
	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		got.ua = r.Header.Get("User-Agent")
		fmt.Fprint(w, `{"data":[{"id":"opencode/grok-code"}]}`)
	})
	t.Setenv("AI_BASE_URL", server.URL+"/v1")
	t.Setenv("AI_API_KEY", "probe-key")

	status, preview, err := service.TestEndpoint(context.Background(), 0)
	if err != nil {
		t.Fatalf("TestEndpoint error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if got.path != "/v1/models" {
		t.Errorf("path = %q, want /v1/models", got.path)
	}
	if got.auth != "Bearer probe-key" {
		t.Errorf("Authorization = %q", got.auth)
	}
	if got.ua != "sodapop/1.4.2" {
		t.Errorf("User-Agent = %q", got.ua)
	}
	if !strings.Contains(preview, "opencode/grok-code") {
		t.Errorf("preview = %q", preview)
	}
}

func TestTestEndpointNon2xxPreviewIsCapped(t *testing.T) {
	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, strings.Repeat("x", 500))
	})
	t.Setenv("AI_BASE_URL", server.URL)

	status, preview, err := service.TestEndpoint(context.Background(), 0)
	if err != nil {
		t.Fatalf("TestEndpoint error: %v", err)
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", status)
	}
	if len(preview) > 200 {
		t.Errorf("preview length = %d, want <= 200", len(preview))
	}
}

func TestDraftDrinkStripsFencesAndNormalizes(t *testing.T) {
	var payload map[string]any
	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		content := "```json\n{\n  \"name\": \"Lime Fizz!\",\n  \"brand\": \"Fountain Co\",\n  \"flavours\": [\"lime\", {\"name\": \"mint\", \"intensity\": 9}],\n  \"volume_ml\": \"330\",\n  \"reply\": \"Here is your draft!\"\n}\n```"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message":       map[string]string{"content": content},
				"finish_reason": "stop",
			}},
		})
	})
	t.Setenv("AI_BASE_URL", server.URL)

	draft, reply, session, err := service.DraftDrink(context.Background(), "bright lime, light fizz", "0123456789012", "draft-session-1")
	if err != nil {
		t.Fatalf("DraftDrink error: %v", err)
	}
	if session != "draft-session-1" {
		t.Errorf("session = %q, want draft-session-1", session)
	}
	if draft["name"] != "Lime Fizz." {
		t.Errorf("name = %v, want exclamation stripped", draft["name"])
	}
	if draft["volume_ml"] != float64(330) {
		t.Errorf("volume_ml = %v, want numeric 330", draft["volume_ml"])
	}
	if draft["style"] != "" {
		t.Errorf("style = %v, want empty default", draft["style"])
	}
	flavours, ok := draft["flavours"].([]any)
	if !ok || len(flavours) != 2 {
		t.Fatalf("flavours = %v", draft["flavours"])
	}
	mint := flavours[1].(map[string]any)
	if mint["intensity"] != 5 {
		t.Errorf("mint intensity = %v, want clamped 5", mint["intensity"])
	}
	if strings.Contains(reply, "!") {
		t.Errorf("reply = %q, want no exclamation mark", reply)
	}
	if got, _ := payload["response_format"].(map[string]any); got["type"] != "json_object" {
		t.Errorf("draft request did not enable JSON mode: %v", payload["response_format"])
	}
	if payload["max_tokens"] != float64(defaultMaxTokens) {
		t.Errorf("max_tokens = %v, want %d", payload["max_tokens"], defaultMaxTokens)
	}
}

func TestDraftDrinkInvalidJSON(t *testing.T) {
	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"not json"},"finish_reason":"stop"}]}`)
	})
	t.Setenv("AI_BASE_URL", server.URL)

	_, _, _, err := service.DraftDrink(context.Background(), "notes", "", "")
	if !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("DraftDrink error = %v, want ErrInvalidOutput", err)
	}
}

func TestTastingNoteVoiceRules(t *testing.T) {
	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Big lime hit! Very fizzy! Super sweet! Would drink again! Extra sentence."},"finish_reason":"stop"}]}`)
	})
	t.Setenv("AI_BASE_URL", server.URL)

	note, err := service.TastingNote(context.Background(), "lime, very fizzy, sweet")
	if err != nil {
		t.Fatalf("TastingNote error: %v", err)
	}
	if strings.Contains(note, "!") {
		t.Errorf("note = %q, want no exclamation marks", note)
	}
	if len([]rune(note)) > 280 {
		t.Errorf("note length = %d, want <= 280", len([]rune(note)))
	}
	if count := strings.Count(note, "."); count > 3 {
		t.Errorf("note has %d sentence terminators: %q", count, note)
	}
}

func TestClientTimeoutRespectsDraftDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), DraftTimeout)
	defer cancel()
	if got := clientTimeout(ctx, TextTimeout); got <= TextTimeout {
		t.Fatalf("clientTimeout = %v, want more than the %v text budget for drafting", got, TextTimeout)
	}
	if got := clientTimeout(context.Background(), TextTimeout); got != TextTimeout {
		t.Fatalf("clientTimeout without deadline = %v, want %v", got, TextTimeout)
	}

	short, shortCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shortCancel()
	same, stop := withDefaultTimeout(short, TextTimeout)
	defer stop()
	if deadline, ok := same.Deadline(); !ok || time.Until(deadline) > 6*time.Second {
		t.Fatal("withDefaultTimeout did not preserve the caller's shorter deadline")
	}
}

func TestRecommend(t *testing.T) {
	service, server := testService(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Try a dry yuzu soda next! Then a cola."},"finish_reason":"stop"}]}`)
	})
	t.Setenv("AI_BASE_URL", server.URL)

	got, err := service.Recommend(context.Background(), "likes citrus and low sugar")
	if err != nil {
		t.Fatalf("Recommend error: %v", err)
	}
	if strings.Contains(got, "!") {
		t.Errorf("recommendation = %q, want no exclamation marks", got)
	}
	if !strings.Contains(got, "yuzu") {
		t.Errorf("recommendation = %q", got)
	}
}
