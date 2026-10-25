// Package ai provides Sodapop's optional OpenAI-compatible AI helpers: endpoint
// management (with AES-GCM encrypted API keys), tasting-note polishing, drink
// drafting and Soda Sommelier recommendations.
//
// Every outbound request identifies Sodapop honestly with a sodapop/<version>
// user agent and carries an x-opencode-session header so multi-turn workflows
// keep upstream session affinity. Handlers are responsible for hiding AI UI
// when Enabled reports false; Chat itself is gated too.
package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/aiendpoint"
)

const (
	// TestTimeout bounds the admin "test endpoint" probe.
	TestTimeout = 30 * time.Second
	// TextTimeout bounds chat, tasting-note and recommendation calls.
	TextTimeout = 60 * time.Second
	// ImageTimeout bounds image generation calls.
	ImageTimeout = 120 * time.Second
	// DraftTimeout bounds multi-turn drink drafting, which may reason longer.
	DraftTimeout = 180 * time.Second

	defaultModel       = "opencode/grok-code"
	openAIDefaultModel = "gpt-4o-mini"
	defaultTemperature = 0.7
	defaultMaxTokens   = 4096
	minMaxTokens       = 256
	maxMaxTokens       = 16384
)

var (
	// ErrNotEnabled means the global ai_enabled switch is off.
	ErrNotEnabled = errors.New("ai is disabled")
	// ErrNoEndpoint means no enabled endpoint and no env fallback is configured.
	ErrNoEndpoint = errors.New("no enabled AI endpoint configured")
	// ErrEndpointNotFound means the requested endpoint row does not exist.
	ErrEndpointNotFound = errors.New("ai endpoint not found")
	// ErrDuplicateName means another endpoint already uses the name.
	ErrDuplicateName = errors.New("an ai endpoint with that name already exists")
	// ErrInvalidBaseURL means the base URL is not an absolute http(s) URL.
	ErrInvalidBaseURL = errors.New("invalid base URL: must be an absolute http(s) URL")
	// ErrInvalidInput means required input was missing.
	ErrInvalidInput = errors.New("invalid AI request")
	// ErrNothingToSend means no messages or notes were supplied.
	ErrNothingToSend = errors.New("nothing to send to the AI provider")
	// ErrTruncated means the provider stopped before producing usable output.
	ErrTruncated = errors.New("ai_output_truncated")
	// ErrInvalidOutput means the provider returned malformed structured output.
	ErrInvalidOutput = errors.New("ai_invalid_response")
)

// ChatMessage is one OpenAI-compatible message.
type ChatMessage struct {
	Role    string
	Content string
}

// ChatRequest is a single completion request.
type ChatRequest struct {
	Messages    []ChatMessage
	MaxTokens   int
	Temperature float64
	SessionID   string
	JSON        bool
}

// ChatResponse is the parsed subset of an OpenAI-compatible completion.
type ChatResponse struct {
	Content   string
	SessionID string
	Model     string
	Usage     map[string]int
}

// endpoint is the resolved connection details for one provider.
type endpoint struct {
	ID          int
	Name        string
	Type        string
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   *int
	Temperature *float64
}

// operationSuffixes are paths admins accidentally paste into base_url.
var operationSuffixes = []string{
	"/chat/completions",
	"/images/generations",
	"/completions",
}

// NormalizeBaseURL validates an http(s) base URL and strips trailing slashes
// and one or more accidental operation suffixes. The result never ends in a
// slash, so callers can append /chat/completions exactly once.
func NormalizeBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrInvalidBaseURL
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", ErrInvalidBaseURL
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidBaseURL
	}
	if parsed.Host == "" || parsed.User != nil {
		return "", ErrInvalidBaseURL
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""

	path := strings.TrimRight(parsed.Path, "/")
	for {
		stripped := false
		for _, suffix := range operationSuffixes {
			if strings.HasSuffix(path, suffix) {
				path = strings.TrimRight(strings.TrimSuffix(path, suffix), "/")
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
	}
	parsed.Path = path
	parsed.RawPath = ""

	out := strings.TrimRight(parsed.String(), "/")
	if out == "" {
		return "", ErrInvalidBaseURL
	}
	return out, nil
}

// RequestURL joins a normalized base URL and an operation path exactly once.
func requestURL(baseURL, operationPath string) string {
	base, err := NormalizeBaseURL(baseURL)
	if err != nil {
		base = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
	if !strings.HasPrefix(operationPath, "/") {
		operationPath = "/" + operationPath
	}
	return base + operationPath
}

// GenerateSessionID returns a 32-character hex random session ID.
func GenerateSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// ResolveSessionID reuses an inbound session or creates one for the first turn.
func ResolveSessionID(inbound string) string {
	if trimmed := strings.TrimSpace(inbound); trimmed != "" {
		return trimmed
	}
	return GenerateSessionID()
}

// setProviderHeaders adds honest attribution and session continuity headers.
func setProviderHeaders(req *http.Request, version, sessionID string) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "0.0.0-dev"
	}
	req.Header.Set("User-Agent", "sodapop/"+version)
	req.Header.Set("x-opencode-session", ResolveSessionID(sessionID))
}

func (s *Service) version() string {
	if s == nil {
		return "0.0.0-dev"
	}
	if v := strings.TrimSpace(s.Version); v != "" {
		return v
	}
	if s.Cfg != nil {
		if v := strings.TrimSpace(s.Cfg.Version); v != "" {
			return v
		}
	}
	return "0.0.0-dev"
}

// clientFor returns an HTTP client with at least the requested timeout. The
// caller's context deadline is still the authority for shorter operations.
func (s *Service) clientFor(timeout time.Duration) *http.Client {
	if s == nil || s.HTTP == nil {
		return &http.Client{Timeout: timeout}
	}
	client := *s.HTTP
	if client.Timeout == 0 || client.Timeout < timeout {
		client.Timeout = timeout
	}
	return &client
}

func (s *Service) logger() *slog.Logger {
	if s != nil && s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// resolveEndpoint picks the first enabled text/vision endpoint, then falls
// back to AI_BASE_URL/AI_API_KEY/AI_MODEL environment configuration.
func (s *Service) resolveEndpoint(ctx context.Context) (*endpoint, error) {
	if s.Client != nil {
		rows, err := s.Client.AIEndpoint.Query().
			Where(aiendpoint.EnabledEQ(true), aiendpoint.TypeNEQ(aiendpoint.TypeImage)).
			Order(ent.Asc(aiendpoint.FieldID)).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("look up ai endpoints: %w", err)
		}
		for _, row := range rows {
			ep, err := s.endpointFromRow(row)
			if err != nil {
				s.logger().Error("ai endpoint key unavailable", "endpoint_id", row.ID, "error", err)
				continue
			}
			return ep, nil
		}
	}

	base := strings.TrimSpace(os.Getenv("AI_BASE_URL"))
	if base != "" {
		normalized, err := NormalizeBaseURL(base)
		if err != nil {
			return nil, fmt.Errorf("AI_BASE_URL: %w", err)
		}
		model := strings.TrimSpace(os.Getenv("AI_MODEL"))
		if model == "" {
			model = defaultModelFor(normalized)
		}
		return &endpoint{
			Name:    "environment",
			Type:    string(aiendpoint.TypeText),
			BaseURL: normalized,
			APIKey:  strings.TrimSpace(os.Getenv("AI_API_KEY")),
			Model:   model,
		}, nil
	}
	return nil, ErrNoEndpoint
}

func defaultModelFor(baseURL string) string {
	if strings.Contains(strings.ToLower(baseURL), "openai.com") {
		return openAIDefaultModel
	}
	return defaultModel
}

func (s *Service) endpointFromRow(row *ent.AIEndpoint) (*endpoint, error) {
	if row == nil {
		return nil, ErrEndpointNotFound
	}
	base, err := NormalizeBaseURL(row.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("endpoint %d base URL: %w", row.ID, err)
	}
	key, err := decryptAPIKey(s.secret(), row.EncryptedAPIKey)
	if err != nil {
		return nil, fmt.Errorf("endpoint %d api key: %w", row.ID, err)
	}
	return &endpoint{
		ID:          row.ID,
		Name:        row.Name,
		Type:        string(row.Type),
		BaseURL:     base,
		APIKey:      key,
		Model:       row.Model,
		MaxTokens:   row.MaxTokens,
		Temperature: row.Temperature,
	}, nil
}

// Chat performs an OpenAI-compatible chat completion against the first enabled
// endpoint (or the environment fallback). It is gated by Enabled.
func (s *Service) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if len(req.Messages) == 0 {
		return nil, ErrNothingToSend
	}
	enabled, err := s.Enabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrNotEnabled
	}
	ep, err := s.resolveEndpoint(ctx)
	if err != nil {
		return nil, err
	}

	messages := make([]map[string]string, 0, len(req.Messages))
	for _, message := range req.Messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(message.Role))
		switch role {
		case "system", "user", "assistant", "tool":
		default:
			role = "user"
		}
		messages = append(messages, map[string]string{"role": role, "content": content})
	}
	if len(messages) == 0 {
		return nil, ErrNothingToSend
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 && ep.MaxTokens != nil {
		maxTokens = *ep.MaxTokens
	}
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	maxTokens = clamp(maxTokens, minMaxTokens, maxMaxTokens)

	temperature := req.Temperature
	if temperature <= 0 && ep.Temperature != nil {
		temperature = *ep.Temperature
	}
	if temperature <= 0 {
		temperature = defaultTemperature
	}
	if temperature > 2 {
		temperature = 2
	}

	payload := map[string]any{
		"model":       ep.Model,
		"messages":    messages,
		"max_tokens":  maxTokens,
		"temperature": temperature,
	}
	if req.JSON {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}

	sessionID := ResolveSessionID(req.SessionID)
	ctx, cancel := withDefaultTimeout(ctx, TextTimeout)
	defer cancel()

	content, model, usage, err := s.complete(ctx, ep, payload, sessionID, clientTimeout(ctx, TextTimeout))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(content) == "" {
		return nil, ErrTruncated
	}
	if model == "" {
		model = ep.Model
	}
	return &ChatResponse{
		Content:   content,
		SessionID: sessionID,
		Model:     model,
		Usage:     usage,
	}, nil
}

func (s *Service) complete(ctx context.Context, ep *endpoint, payload map[string]any, sessionID string, timeout time.Duration) (string, string, map[string]int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", nil, fmt.Errorf("encode ai request: %w", err)
	}
	target := requestURL(ep.BaseURL, "/chat/completions")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return "", "", nil, fmt.Errorf("create ai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if ep.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+ep.APIKey)
	}
	setProviderHeaders(httpReq, s.version(), sessionID)

	started := time.Now()
	resp, err := s.clientFor(timeout).Do(httpReq)
	if err != nil {
		s.logger().Error("ai request failed", "endpoint_id", ep.ID, "model", ep.Model, "error", err)
		return "", "", nil, fmt.Errorf("ai provider request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", "", nil, fmt.Errorf("read ai response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := truncateText(string(respBody), 200)
		s.logger().Error("ai provider returned non-2xx", "endpoint_id", ep.ID, "model", ep.Model, "status", resp.StatusCode, "preview", preview)
		return "", "", nil, fmt.Errorf("ai provider returned HTTP %d: %s", resp.StatusCode, preview)
	}

	var result struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		s.logger().Error("parse ai response failed", "endpoint_id", ep.ID, "error", err)
		return "", "", nil, fmt.Errorf("parse ai response: %w", ErrInvalidOutput)
	}
	if len(result.Choices) == 0 {
		return "", "", nil, ErrTruncated
	}
	choice := result.Choices[0]
	usage := usageInts(result.Usage)

	s.logger().Info("ai request completed",
		"endpoint_id", ep.ID,
		"model", ep.Model,
		"finish_reason", choice.FinishReason,
		"content_len", len(choice.Message.Content),
		"duration_ms", time.Since(started).Milliseconds(),
	)

	if choice.FinishReason == "length" {
		return "", result.Model, usage, ErrTruncated
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		return "", result.Model, usage, ErrTruncated
	}
	return choice.Message.Content, result.Model, usage, nil
}

func usageInts(raw map[string]any) map[string]int {
	if len(raw) == 0 {
		return map[string]int{}
	}
	out := make(map[string]int, len(raw))
	for key, value := range raw {
		switch n := value.(type) {
		case float64:
			out[key] = int(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				out[key] = int(i)
			}
		case int:
			out[key] = n
		}
	}
	return out
}

// withDefaultTimeout keeps an existing (shorter or longer) caller deadline and
// adds fallback only when the caller supplied none. Drafting passes 180s,
// plain chat and tasting notes pass 60s.
func withDefaultTimeout(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, fallback)
}

// clientTimeout lets the HTTP client outlive the default 60s text budget for
// drafting, while never exceeding the slowest supported operation.
func clientTimeout(ctx context.Context, fallback time.Duration) time.Duration {
	timeout := fallback
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > timeout {
			timeout = remaining
		}
	}
	if timeout > DraftTimeout {
		timeout = DraftTimeout
	}
	return timeout
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// truncateText collapses whitespace and caps text at limit runes.
func truncateText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
