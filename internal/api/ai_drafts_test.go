package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sodapop/ent/appsetting"
	"sodapop/ent/user"
	"sodapop/internal/ai"
)

type apiFakeAI struct {
	server *httptest.Server

	mu     sync.Mutex
	calls  int
	finish string
	names  []string
}

func newAPIFakeAI(t *testing.T) *apiFakeAI {
	t.Helper()
	fake := &apiFakeAI{finish: "stop", names: []string{"Lime Fizz", "Lime Fizz Zero", "Lime Fizz Final"}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
			return
		}
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		fake.mu.Lock()
		fake.calls++
		call := fake.calls
		finish := fake.finish
		name := fake.names[len(fake.names)-1]
		if call-1 < len(fake.names) {
			name = fake.names[call-1]
		}
		fake.mu.Unlock()
		if finish == "length" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]string{"content": ""}, "finish_reason": "length",
				}},
			})
			return
		}
		content, _ := json.Marshal(map[string]any{"name": name, "brand": "Fountain Co", "volume_ml": 355})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "fake",
			"choices": []any{map[string]any{
				"message": map[string]string{"content": string(content)}, "finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *apiFakeAI) setFinish(reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finish = reason
}

func newAIAPIHarness(t *testing.T) (*harness, *apiFakeAI) {
	t.Helper()
	fake := newAPIFakeAI(t)
	h := newHarness(t)
	h.server.AI = ai.New(h.server.DB, h.server.Cfg, "test", h.server.Log)
	h.server.RegisterAIAPI(h.mux)
	if _, err := h.server.DB.Client.AppSetting.Update().
		Where(appsetting.KeyEQ("ai_enabled")).
		SetValue("1").
		Save(context.Background()); err != nil {
		t.Fatalf("enable ai: %v", err)
	}
	t.Setenv("AI_BASE_URL", fake.server.URL)
	return h, fake
}

func decodeAPI(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

func apiErrorCode(body []byte) string {
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	return payload.Error.Code
}

func TestAIDraftRoutesLifecycleAndPermissions(t *testing.T) {
	h, _ := newAIAPIHarness(t)
	bobToken := tokenFor(t, h, h.bob, []string{"read", "write"})

	rec := h.do(t, http.MethodPost, "/api/v1/ai/drafts", `{"notes":"bright lime"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("create draft: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			SessionID string         `json:"session_id"`
			Status    string         `json:"status"`
			Message   string         `json:"message"`
			Draft     map[string]any `json:"draft"`
		} `json:"data"`
	}
	decodeAPI(t, rec, &created)
	sessionID := created.Data.SessionID
	if sessionID == "" || created.Data.Draft["name"] != "Lime Fizz" || created.Data.Message == "" {
		t.Fatalf("unexpected create payload: %s", rec.Body.String())
	}

	if rec := doAs(t, h, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/messages", `{"content":"zero sugar"}`, bobToken); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger messages: %d, want 404", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/api/v1/ai/drafts/does-not-exist/messages", `{"content":"hi"}`, true); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown messages: %d, want 404", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/messages", `{"content":""}`, true); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty content: %d, want 400", rec.Code)
	}

	rec = h.do(t, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/messages", `{"content":"zero sugar"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("messages: %d %s", rec.Code, rec.Body.String())
	}
	var turn struct {
		Data struct {
			Status string         `json:"status"`
			Draft  map[string]any `json:"draft"`
		} `json:"data"`
	}
	decodeAPI(t, rec, &turn)
	if turn.Data.Draft["name"] != "Lime Fizz Zero" || turn.Data.Status != "ready" {
		t.Fatalf("unexpected turn payload: %s", rec.Body.String())
	}

	rec = h.do(t, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/revise", `{"instruction":"355 ml can"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("revise: %d %s", rec.Code, rec.Body.String())
	}
	decodeAPI(t, rec, &turn)
	if turn.Data.Draft["name"] != "Lime Fizz Final" {
		t.Fatalf("unexpected revised draft: %s", rec.Body.String())
	}

	rec = h.do(t, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/commit", ``, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
	}
	var committed struct {
		Data struct {
			DrinkID int `json:"drink_id"`
			ID      int `json:"id"`
		} `json:"data"`
	}
	decodeAPI(t, rec, &committed)
	if committed.Data.DrinkID == 0 || committed.Data.DrinkID != committed.Data.ID {
		t.Fatalf("unexpected commit payload: %s", rec.Body.String())
	}
	rec = h.do(t, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/commit", ``, true)
	var repeated struct {
		Data struct {
			DrinkID int `json:"drink_id"`
		} `json:"data"`
	}
	decodeAPI(t, rec, &repeated)
	if repeated.Data.DrinkID != committed.Data.DrinkID {
		t.Fatalf("repeat commit returned %d, want %d", repeated.Data.DrinkID, committed.Data.DrinkID)
	}
	rec = h.do(t, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/messages", `{"content":"more"}`, true)
	if rec.Code != http.StatusConflict || apiErrorCode(rec.Body.Bytes()) != "draft_committed" {
		t.Fatalf("append after commit: %d %s", rec.Code, rec.Body.String())
	}

	if rec := doAs(t, h, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/commit", ``, bobToken); rec.Code != http.StatusNotFound {
		t.Fatalf("stranger commit: %d, want 404", rec.Code)
	}
}

func TestAIDraftExpiryDisabledAndTruncation(t *testing.T) {
	h, fake := newAIAPIHarness(t)
	ctx := context.Background()

	rec := h.do(t, http.MethodPost, "/api/v1/ai/drafts", `{"notes":"lime"}`, true)
	var created struct {
		Data struct {
			SessionID string `json:"session_id"`
		} `json:"data"`
	}
	decodeAPI(t, rec, &created)
	sessionID := created.Data.SessionID
	if sessionID == "" {
		t.Fatalf("create failed: %s", rec.Body.String())
	}

	before, err := h.server.DB.Client.AIDraftSession.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	fake.setFinish("length")
	rec = h.do(t, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/messages", `{"content":"change"}`, true)
	if rec.Code != http.StatusBadGateway || apiErrorCode(rec.Body.Bytes()) != "ai_output_truncated" {
		t.Fatalf("truncated append: %d %s", rec.Code, rec.Body.String())
	}
	after, err := h.server.DB.Client.AIDraftSession.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("load session after truncation: %v", err)
	}
	if after.MessagesJSON != before.MessagesJSON {
		t.Fatal("truncated append persisted a partial turn")
	}
	if count, _ := h.server.DB.Client.Drink.Query().Count(ctx); count != 0 {
		t.Fatalf("truncation created %d drinks", count)
	}
	fake.setFinish("stop")

	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := h.server.DB.Client.AIDraftSession.UpdateOneID(sessionID).SetCreatedAt(old).Save(ctx); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if rec := h.do(t, http.MethodPost, "/api/v1/ai/drafts/"+sessionID+"/messages", `{"content":"hi"}`, true); rec.Code != http.StatusNotFound {
		t.Fatalf("expired session: %d, want 404", rec.Code)
	}

	rec = h.do(t, http.MethodPost, "/api/v1/ai/drafts", `{"notes":"lime"}`, true)
	decodeAPI(t, rec, &created)
	if _, err := h.server.DB.Client.AppSetting.Update().
		Where(appsetting.KeyEQ("ai_enabled")).
		SetValue("0").
		Save(ctx); err != nil {
		t.Fatalf("disable ai: %v", err)
	}
	rec = h.do(t, http.MethodPost, "/api/v1/ai/drafts/"+created.Data.SessionID+"/messages", `{"content":"hi"}`, true)
	if rec.Code != http.StatusServiceUnavailable || apiErrorCode(rec.Body.Bytes()) != "ai_disabled" {
		t.Fatalf("disabled append: %d %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodPost, "/api/v1/ai/drafts", `{"notes":"anything"}`, true)
	if rec.Code != http.StatusServiceUnavailable || apiErrorCode(rec.Body.Bytes()) != "ai_disabled" {
		t.Fatalf("disabled create: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminAIEndpointAPI(t *testing.T) {
	h, fake := newAIAPIHarness(t)
	admin := newUser(t, h, "admin", true, user.RoleAdmin)
	adminToken := tokenFor(t, h, admin.ID, []string{"read", "write"})

	if rec := h.do(t, http.MethodGet, "/api/v1/admin/ai-endpoints", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("non-admin list: %d, want 404", rec.Code)
	}
	if rec := h.do(t, http.MethodPost, "/api/v1/admin/ai-endpoints", `{"name":"x","base_url":"https://api.example.com/v1","model":"m"}`, true); rec.Code != http.StatusNotFound {
		t.Fatalf("non-admin create: %d, want 404", rec.Code)
	}

	rec := doAs(t, h, http.MethodGet, "/api/v1/admin/ai-endpoints", "", adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: %d %s", rec.Code, rec.Body.String())
	}

	body := fmt.Sprintf(`{"name":"opencode zen","type":"text","base_url":"%s/chat/completions","model":"test-model","api_key":"sk-live-secret-9876","tags":["default","draft"],"enabled":true}`, fake.server.URL)
	rec = doAs(t, h, http.MethodPost, "/api/v1/admin/ai-endpoints", body, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			ID      int      `json:"id"`
			BaseURL string   `json:"base_url"`
			APIKey  string   `json:"api_key"`
			Tags    []string `json:"tags"`
		} `json:"data"`
	}
	decodeAPI(t, rec, &created)
	if created.Data.ID == 0 || created.Data.BaseURL != fake.server.URL {
		t.Fatalf("unexpected endpoint: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-live-secret") || !strings.HasPrefix(created.Data.APIKey, "sk-...") {
		t.Fatalf("api key was not masked: %s", rec.Body.String())
	}
	if len(created.Data.Tags) != 2 {
		t.Fatalf("tags = %v", created.Data.Tags)
	}

	rec = doAs(t, h, http.MethodPost, "/api/v1/admin/ai-endpoints", body, adminToken)
	if rec.Code != http.StatusConflict || apiErrorCode(rec.Body.Bytes()) != "duplicate_name" {
		t.Fatalf("duplicate endpoint: %d %s", rec.Code, rec.Body.String())
	}
	rec = doAs(t, h, http.MethodPost, "/api/v1/admin/ai-endpoints", `{"name":"bad","base_url":"ftp://example.com","model":"m"}`, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid base url: %d %s", rec.Code, rec.Body.String())
	}

	id := fmt.Sprint(created.Data.ID)
	update := fmt.Sprintf(`{"name":"opencode zen","type":"text","base_url":"%s","model":"updated-model","tags":["updated"],"enabled":true}`, fake.server.URL)
	rec = doAs(t, h, http.MethodPut, "/api/v1/admin/ai-endpoints/"+id, update, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin update: %d %s", rec.Code, rec.Body.String())
	}
	var updated struct {
		Data struct {
			Model  string `json:"model"`
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	decodeAPI(t, rec, &updated)
	if updated.Data.Model != "updated-model" {
		t.Fatalf("model = %q", updated.Data.Model)
	}

	rec = doAs(t, h, http.MethodPost, "/api/v1/admin/ai-endpoints/"+id+"/toggle", "", adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin toggle: %d %s", rec.Code, rec.Body.String())
	}
	decodeAPI(t, rec, &updated)
	if !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("toggle did not disable: %s", rec.Body.String())
	}

	rec = doAs(t, h, http.MethodPost, "/api/v1/admin/ai-endpoints/"+id+"/test", "", adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin test: %d %s", rec.Code, rec.Body.String())
	}
	var tested struct {
		Data struct {
			Status  int    `json:"status"`
			Preview string `json:"preview"`
			OK      bool   `json:"ok"`
		} `json:"data"`
	}
	decodeAPI(t, rec, &tested)
	if tested.Data.Status != http.StatusOK || !tested.Data.OK {
		t.Fatalf("test payload: %s", rec.Body.String())
	}

	rec = doAs(t, h, http.MethodDelete, "/api/v1/admin/ai-endpoints/"+id, "", adminToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = doAs(t, h, http.MethodDelete, "/api/v1/admin/ai-endpoints/"+id, "", adminToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d, want 404", rec.Code)
	}
	rec = doAs(t, h, http.MethodGet, "/api/v1/admin/ai-endpoints", "", adminToken)
	var listed struct {
		Data []map[string]any `json:"data"`
	}
	decodeAPI(t, rec, &listed)
	if len(listed.Data) != 0 {
		t.Fatalf("endpoints remain after delete: %v", listed.Data)
	}
}
