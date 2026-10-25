package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"sodapop/ent/appsetting"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

func newTestDatabase(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "ai.db"))
	if err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestEnabledUsesAppSettingThenEnv(t *testing.T) {
	t.Setenv("AI_BASE_URL", "")
	database := newTestDatabase(t)
	service := New(database, &config.Config{SessionKey: []byte("test-session-secret")}, "test", discardLogger())
	ctx := context.Background()

	enabled, err := service.Enabled(ctx)
	if err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if enabled {
		t.Fatal("AI should be disabled by the seeded ai_enabled=0 setting")
	}

	if _, err := database.Client.AppSetting.Update().
		Where(appsetting.KeyEQ("ai_enabled")).
		SetValue("1").
		Save(ctx); err != nil {
		t.Fatalf("enable ai_enabled: %v", err)
	}
	enabled, err = service.Enabled(ctx)
	if err != nil {
		t.Fatalf("Enabled after update: %v", err)
	}
	if !enabled {
		t.Fatal("AI should be enabled after ai_enabled=1")
	}

	envOnly := &Service{}
	t.Setenv("AI_BASE_URL", "http://localhost:11434/v1")
	enabled, err = envOnly.Enabled(ctx)
	if err != nil {
		t.Fatalf("env Enabled: %v", err)
	}
	if !enabled {
		t.Fatal("AI should be enabled by AI_BASE_URL when no setting exists")
	}
}

func TestEndpointCRUDAndMasking(t *testing.T) {
	database := newTestDatabase(t)
	secret := "test-session-secret"
	service := New(database, &config.Config{SessionKey: []byte(secret)}, "test", discardLogger())
	ctx := context.Background()

	list, err := service.ListEndpoints(ctx)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("initial endpoints = %d, want 0", len(list))
	}

	created, err := service.SaveEndpoint(ctx, 0, EndpointInput{
		Name:    "opencode zen",
		Type:    "text",
		BaseURL: "https://opencode.ai/zen/go/v1/chat/completions/",
		APIKey:  "sk-live-very-secret-value-9876",
		Model:   "opencode/grok-code",
		Tags:    []string{"default", "draft"},
	})
	if err != nil {
		t.Fatalf("SaveEndpoint create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("created endpoint has no ID")
	}
	if created.BaseURL != "https://opencode.ai/zen/go/v1" {
		t.Errorf("BaseURL = %q, want normalized URL", created.BaseURL)
	}
	if created.EncryptedAPIKey != "sk-...9876" {
		t.Errorf("returned key = %q, want masked", created.EncryptedAPIKey)
	}
	if !created.Enabled {
		t.Error("new endpoint should be enabled by default")
	}

	stored, err := database.Client.AIEndpoint.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get stored endpoint: %v", err)
	}
	if stored.EncryptedAPIKey == "" || strings.Contains(stored.EncryptedAPIKey, "sk-live") {
		t.Fatalf("stored key is not encrypted: %q", stored.EncryptedAPIKey)
	}
	plaintext, err := decryptAPIKey(secret, stored.EncryptedAPIKey)
	if err != nil {
		t.Fatalf("decrypt stored key: %v", err)
	}
	if plaintext != "sk-live-very-secret-value-9876" {
		t.Fatalf("decrypted key = %q", plaintext)
	}
	if stored.Tags != `["default","draft"]` {
		t.Errorf("Tags = %q", stored.Tags)
	}

	if _, err := service.SaveEndpoint(ctx, 0, EndpointInput{
		Name:    "opencode zen",
		Type:    "text",
		BaseURL: "https://opencode.ai/zen/go/v1",
		Model:   "opencode/grok-code",
	}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate name error = %v, want ErrDuplicateName", err)
	}

	originalCipher := stored.EncryptedAPIKey
	updated, err := service.SaveEndpoint(ctx, created.ID, EndpointInput{
		Name:    "opencode zen renamed",
		Type:    "text",
		BaseURL: "https://opencode.ai/zen/go/v1/",
		Model:   "opencode/grok-code-v2",
	})
	if err != nil {
		t.Fatalf("SaveEndpoint update: %v", err)
	}
	if updated.Name != "opencode zen renamed" || updated.Model != "opencode/grok-code-v2" {
		t.Errorf("updated endpoint = %+v", updated)
	}
	stored, _ = database.Client.AIEndpoint.Get(ctx, created.ID)
	if stored.EncryptedAPIKey != originalCipher {
		t.Error("empty APIKey on update replaced the stored key")
	}

	if _, err := service.SaveEndpoint(ctx, created.ID, EndpointInput{
		Name:    "opencode zen renamed",
		Type:    "text",
		BaseURL: "https://opencode.ai/zen/go/v1",
		APIKey:  "sk-new-key-0000",
		Model:   "opencode/grok-code-v2",
	}); err != nil {
		t.Fatalf("SaveEndpoint rekey: %v", err)
	}
	stored, _ = database.Client.AIEndpoint.Get(ctx, created.ID)
	if plaintext, _ := decryptAPIKey(secret, stored.EncryptedAPIKey); plaintext != "sk-new-key-0000" {
		t.Errorf("rotated key = %q", plaintext)
	}

	list, err = service.ListEndpoints(ctx)
	if err != nil {
		t.Fatalf("ListEndpoints after create: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(list))
	}
	if list[0].EncryptedAPIKey != "sk-...0000" {
		t.Errorf("listed key = %q, want masked", list[0].EncryptedAPIKey)
	}

	if _, err := service.SaveEndpoint(ctx, created.ID, EndpointInput{
		Name:        "opencode zen renamed",
		Type:        "text",
		BaseURL:     "https://opencode.ai/zen/go/v1",
		Model:       "opencode/grok-code-v2",
		ClearAPIKey: true,
	}); err != nil {
		t.Fatalf("SaveEndpoint clear key: %v", err)
	}
	stored, _ = database.Client.AIEndpoint.Get(ctx, created.ID)
	if stored.EncryptedAPIKey != "" {
		t.Errorf("cleared key = %q, want empty", stored.EncryptedAPIKey)
	}

	if err := service.DeleteEndpoint(ctx, created.ID); err != nil {
		t.Fatalf("DeleteEndpoint: %v", err)
	}
	if err := service.DeleteEndpoint(ctx, created.ID); !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("second delete error = %v, want ErrEndpointNotFound", err)
	}
}

func TestSaveEndpointValidation(t *testing.T) {
	database := newTestDatabase(t)
	service := New(database, &config.Config{SessionKey: []byte("test-session-secret")}, "test", discardLogger())
	ctx := context.Background()

	tests := []struct {
		name string
		in   EndpointInput
		want error
	}{
		{name: "missing name", in: EndpointInput{BaseURL: "https://example.com/v1", Model: "m"}, want: ErrInvalidInput},
		{name: "bad type", in: EndpointInput{Name: "x", Type: "audio", BaseURL: "https://example.com/v1", Model: "m"}, want: ErrInvalidInput},
		{name: "bad base url", in: EndpointInput{Name: "x", BaseURL: "ftp://example.com", Model: "m"}, want: ErrInvalidBaseURL},
		{name: "missing model", in: EndpointInput{Name: "x", BaseURL: "https://example.com/v1"}, want: ErrInvalidInput},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.SaveEndpoint(ctx, 0, tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	if _, err := service.SaveEndpoint(ctx, 999, EndpointInput{
		Name:    "missing row",
		Type:    "text",
		BaseURL: "https://example.com/v1",
		Model:   "m",
	}); !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("update missing endpoint error = %v, want ErrEndpointNotFound", err)
	}
}

func TestTestEndpointUsesStoredKey(t *testing.T) {
	database := newTestDatabase(t)
	service := New(database, &config.Config{SessionKey: []byte("test-session-secret")}, "test", discardLogger())
	ctx := context.Background()

	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer server.Close()

	ep, err := service.SaveEndpoint(ctx, 0, EndpointInput{
		Name:    "local",
		Type:    "text",
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-local-key-5555",
		Model:   "local-model",
	})
	if err != nil {
		t.Fatalf("SaveEndpoint: %v", err)
	}

	status, _, err := service.TestEndpoint(ctx, ep.ID)
	if err != nil {
		t.Fatalf("TestEndpoint: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if auth != "Bearer sk-local-key-5555" {
		t.Errorf("Authorization = %q, want decrypted stored key", auth)
	}

	if _, _, err := service.TestEndpoint(ctx, 999); !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("missing endpoint test error = %v, want ErrEndpointNotFound", err)
	}
}
