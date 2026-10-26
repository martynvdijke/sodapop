package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sodapop/ent/aidraftsession"
	"sodapop/ent/appsetting"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

type fakeDraftProvider struct {
	server *httptest.Server

	mu           sync.Mutex
	calls        int
	finishReason string
	sessionIDs   []string
	drafts       []map[string]any
}

func newFakeDraftProvider(t *testing.T, drafts ...map[string]any) *fakeDraftProvider {
	t.Helper()
	if len(drafts) == 0 {
		drafts = []map[string]any{{"name": "Lime Fizz", "brand": "Fountain Co"}}
	}
	fake := &fakeDraftProvider{finishReason: "stop", drafts: drafts}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)

		fake.mu.Lock()
		fake.calls++
		call := fake.calls
		fake.sessionIDs = append(fake.sessionIDs, r.Header.Get("x-opencode-session"))
		finish := fake.finishReason
		draft := fake.drafts[len(fake.drafts)-1]
		if call-1 < len(fake.drafts) {
			draft = fake.drafts[call-1]
		}
		fake.mu.Unlock()

		if finish == "length" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "fake-model",
				"choices": []any{map[string]any{
					"message":       map[string]string{"content": ""},
					"finish_reason": "length",
				}},
			})
			return
		}
		content, _ := json.Marshal(draft)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "fake-model",
			"choices": []any{map[string]any{
				"message":       map[string]string{"content": string(content)},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10},
		})
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeDraftProvider) setFinish(reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finishReason = reason
}

func (f *fakeDraftProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeDraftProvider) sessions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sessionIDs...)
}

func newDraftService(t *testing.T, fake *fakeDraftProvider) (*Service, *db.DB) {
	t.Helper()
	database := newTestDatabase(t)
	ctx := context.Background()
	if _, err := database.Client.AppSetting.Update().
		Where(appsetting.KeyEQ("ai_enabled")).
		SetValue("1").
		Save(ctx); err != nil {
		t.Fatalf("enable ai: %v", err)
	}
	t.Setenv("AI_BASE_URL", fake.server.URL)
	service := New(database, &config.Config{Version: "test", SessionKey: []byte("test-session-secret")}, "test", discardLogger())
	return service, database
}

func TestDraftSessionLifecycle(t *testing.T) {
	fake := newFakeDraftProvider(t,
		map[string]any{"name": "Lime Fizz", "brand": "Fountain Co", "volume_ml": 330},
		map[string]any{"name": "Lime Fizz Zero", "brand": "Fountain Co", "volume_ml": 330},
		map[string]any{"name": "Lime Fizz Final", "brand": "Fountain Co", "volume_ml": 355},
	)
	service, database := newDraftService(t, fake)
	ctx := context.Background()
	user := "user-1"

	session, err := service.CreateDraftSession(ctx, user, "drink", "bright lime, light fizz", "")
	if err != nil {
		t.Fatalf("CreateDraftSession: %v", err)
	}
	if session.ID == "" {
		t.Fatal("session has no id")
	}
	if session.Status != aidraftsession.StatusChatting {
		t.Fatalf("status = %s, want chatting", session.Status)
	}
	messages, err := decodeDraftMessages(session.MessagesJSON)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages = %v (%v), want 2", messages, err)
	}
	if messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("roles = %q, %q", messages[0].Role, messages[1].Role)
	}
	draft, meta, err := decodeDraftState(session.DraftJSON)
	if err != nil {
		t.Fatalf("decode draft: %v", err)
	}
	if draft["name"] != "Lime Fizz" {
		t.Fatalf("draft name = %v", draft["name"])
	}
	if meta.UpstreamSessionID == "" {
		t.Fatal("upstream session id was not persisted")
	}

	turn, err := service.AppendDraftMessage(ctx, user, session.ID, "make it zero sugar")
	if err != nil {
		t.Fatalf("AppendDraftMessage: %v", err)
	}
	if turn.Draft["name"] != "Lime Fizz Zero" {
		t.Fatalf("appended draft name = %v", turn.Draft["name"])
	}
	if turn.Message == "" {
		t.Fatal("assistant reply is empty")
	}
	sessions := fake.sessions()
	if len(sessions) < 2 || sessions[0] != sessions[1] {
		t.Fatalf("upstream session continuity lost: %v", sessions)
	}

	turn, err = service.ReviseDraft(ctx, user, session.ID, "use a 355 ml can")
	if err != nil {
		t.Fatalf("ReviseDraft: %v", err)
	}
	if turn.Draft["name"] != "Lime Fizz Final" {
		t.Fatalf("revised draft name = %v", turn.Draft["name"])
	}
	if fake.callCount() != 3 {
		t.Fatalf("provider calls = %d, want 3", fake.callCount())
	}

	drink, err := service.CommitDraft(ctx, user, session.ID)
	if err != nil {
		t.Fatalf("CommitDraft: %v", err)
	}
	if drink.Name != "Lime Fizz Final" {
		t.Fatalf("committed drink = %q", drink.Name)
	}
	again, err := service.CommitDraft(ctx, user, session.ID)
	if err != nil {
		t.Fatalf("CommitDraft again: %v", err)
	}
	if again.ID != drink.ID {
		t.Fatalf("idempotent commit returned %d, want %d", again.ID, drink.ID)
	}
	if n := commitLocks.len(); n != 0 {
		t.Fatalf("commit lock entries after commit = %d, want 0", n)
	}
	count, err := database.Client.Drink.Query().Count(ctx)
	if err != nil {
		t.Fatalf("drink count: %v", err)
	}
	if count != 1 {
		t.Fatalf("drink count = %d, want 1", count)
	}

	committed, err := service.LoadDraft(ctx, user, session.ID)
	if err != nil {
		t.Fatalf("LoadDraft committed: %v", err)
	}
	if committed.Status != aidraftsession.StatusCommitted {
		t.Fatalf("status = %s, want committed", committed.Status)
	}
	if _, _, err := decodeDraftState(committed.DraftJSON); err != nil {
		t.Fatalf("committed draft json: %v", err)
	}
	if _, err := service.AppendDraftMessage(ctx, user, session.ID, "one more change"); !errors.Is(err, ErrDraftCommitted) {
		t.Fatalf("append after commit = %v, want ErrDraftCommitted", err)
	}
}

func TestDraftOwnerIsolationAndExpiry(t *testing.T) {
	fake := newFakeDraftProvider(t)
	service, database := newDraftService(t, fake)
	ctx := context.Background()

	session, err := service.CreateDraftSession(ctx, "owner", "drink", "lime", "")
	if err != nil {
		t.Fatalf("CreateDraftSession: %v", err)
	}
	if _, err := service.LoadDraft(ctx, "stranger", session.ID); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("stranger LoadDraft = %v, want ErrDraftNotFound", err)
	}
	if _, err := service.AppendDraftMessage(ctx, "stranger", session.ID, "hello"); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("stranger AppendDraftMessage = %v, want ErrDraftNotFound", err)
	}
	if _, err := service.CommitDraft(ctx, "stranger", session.ID); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("stranger CommitDraft = %v, want ErrDraftNotFound", err)
	}

	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := database.Client.AIDraftSession.UpdateOneID(session.ID).SetCreatedAt(old).Save(ctx); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if _, err := service.LoadDraft(ctx, "owner", session.ID); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("expired LoadDraft = %v, want ErrDraftNotFound", err)
	}
	removed, err := service.ExpireDrafts(ctx)
	if err != nil {
		t.Fatalf("ExpireDrafts: %v", err)
	}
	if removed != 1 {
		t.Fatalf("ExpireDrafts removed %d, want 1", removed)
	}
}

func TestDraftHistoryIsCappedWithoutSplittingPairs(t *testing.T) {
	fake := newFakeDraftProvider(t, map[string]any{"name": "Capped", "brand": "Test"})
	service, database := newDraftService(t, fake)
	ctx := context.Background()

	session, err := service.CreateDraftSession(ctx, "owner", "drink", "lime", "")
	if err != nil {
		t.Fatalf("CreateDraftSession: %v", err)
	}
	var history []DraftMessage
	for i := 0; i < 30; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		history = append(history, DraftMessage{
			Role: role, Content: fmt.Sprintf("turn %d", i), CreatedAt: db.Now(),
		})
	}
	encoded, err := encodeDraftMessages(history)
	if err != nil {
		t.Fatalf("encode history: %v", err)
	}
	if _, err := database.Client.AIDraftSession.UpdateOneID(session.ID).SetMessagesJSON(encoded).Save(ctx); err != nil {
		t.Fatalf("seed history: %v", err)
	}

	if _, err := service.AppendDraftMessage(ctx, "owner", session.ID, "cap this"); err != nil {
		t.Fatalf("AppendDraftMessage: %v", err)
	}
	row, err := service.LoadDraft(ctx, "owner", session.ID)
	if err != nil {
		t.Fatalf("LoadDraft: %v", err)
	}
	stored, err := decodeDraftMessages(row.MessagesJSON)
	if err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(stored) > MaxDraftTurns {
		t.Fatalf("stored %d turns, want <= %d", len(stored), MaxDraftTurns)
	}
	if stored[0].Role != "user" {
		t.Fatalf("first retained role = %q, want user (pair split)", stored[0].Role)
	}
	if stored[len(stored)-1].Role != "assistant" {
		t.Fatalf("last retained role = %q, want assistant", stored[len(stored)-1].Role)
	}
}

func TestDraftTruncationWritesNothing(t *testing.T) {
	fake := newFakeDraftProvider(t, map[string]any{"name": "Lime Fizz", "brand": "Test"})
	service, database := newDraftService(t, fake)
	ctx := context.Background()

	session, err := service.CreateDraftSession(ctx, "owner", "drink", "lime", "")
	if err != nil {
		t.Fatalf("CreateDraftSession: %v", err)
	}
	before := session.MessagesJSON
	fake.setFinish("length")
	if _, err := service.AppendDraftMessage(ctx, "owner", session.ID, "change it"); !errors.Is(err, ErrDraftTruncated) {
		t.Fatalf("AppendDraftMessage = %v, want ErrDraftTruncated", err)
	}
	row, err := service.LoadDraft(ctx, "owner", session.ID)
	if err != nil {
		t.Fatalf("LoadDraft: %v", err)
	}
	if row.MessagesJSON != before {
		t.Fatal("truncated turn was persisted")
	}
	count, _ := database.Client.Drink.Query().Count(ctx)
	if count != 0 {
		t.Fatalf("drink count = %d after truncation, want 0", count)
	}

	fake.setFinish("stop")
	if _, err := service.ReviseDraft(ctx, "owner", session.ID, "still fine"); err != nil {
		t.Fatalf("ReviseDraft after truncation: %v", err)
	}
}

func TestDraftDisabledAndIncompleteCommit(t *testing.T) {
	fake := newFakeDraftProvider(t, map[string]any{"brand": "No Name"})
	service, database := newDraftService(t, fake)
	ctx := context.Background()

	session, err := service.CreateDraftSession(ctx, "owner", "drink", "mystery soda", "")
	if err != nil {
		t.Fatalf("CreateDraftSession: %v", err)
	}
	if _, err := service.CommitDraft(ctx, "owner", session.ID); !errors.Is(err, ErrDraftIncomplete) {
		t.Fatalf("CommitDraft incomplete = %v, want ErrDraftIncomplete", err)
	}
	if count, _ := database.Client.Drink.Query().Count(ctx); count != 0 {
		t.Fatalf("incomplete commit created %d drinks", count)
	}

	if _, err := database.Client.AppSetting.Update().
		Where(appsetting.KeyEQ("ai_enabled")).
		SetValue("0").
		Save(ctx); err != nil {
		t.Fatalf("disable ai: %v", err)
	}
	if _, err := service.CommitDraft(ctx, "owner", session.ID); !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("CommitDraft disabled = %v, want ErrNotEnabled", err)
	}
	if _, err := service.AppendDraftMessage(ctx, "owner", session.ID, "hello"); !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("AppendDraftMessage disabled = %v, want ErrNotEnabled", err)
	}
	if count, _ := database.Client.Drink.Query().Count(ctx); count != 0 {
		t.Fatalf("disabled commit created %d drinks", count)
	}
}

func TestCreateDraftSessionRejectsOversizeNotes(t *testing.T) {
	fake := newFakeDraftProvider(t)
	service, _ := newDraftService(t, fake)
	if _, err := service.CreateDraftSession(context.Background(), "owner", "drink", strings.Repeat("x", MaxDraftNotesRunes+1), ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateDraftSession oversize = %v, want ErrInvalidInput", err)
	}
	if fake.callCount() != 0 {
		t.Fatalf("provider was called %d times for invalid input", fake.callCount())
	}
}
