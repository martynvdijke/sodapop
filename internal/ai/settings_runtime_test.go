package ai

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"sodapop/ent/appsetting"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/settings"
)

func TestEnabledUsesRuntimeSnapshotOverLegacyRow(t *testing.T) {
	t.Setenv("AI_BASE_URL", "")
	database, err := db.Init(filepath.Join(t.TempDir(), "ai-settings.db"))
	if err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()

	cfg := &config.Config{SessionKey: []byte("0123456789abcdef0123456789abcdef")}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := settings.New(database.Client, cfg, "", log)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	if err := svc.Load(ctx); err != nil {
		t.Fatalf("settings.Load: %v", err)
	}

	// Legacy row says on, runtime setting says off: runtime is authoritative.
	if _, err := database.Client.AppSetting.Update().
		Where(appsetting.KeyEQ("ai_enabled")).
		SetValue("1").Save(ctx); err != nil {
		t.Fatalf("legacy ai_enabled: %v", err)
	}
	service := New(database, cfg, "test", log, svc)
	enabled, err := service.Enabled(ctx)
	if err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if enabled {
		t.Fatal("runtime AIEnabled=false should override the legacy row")
	}

	if err := svc.Set(ctx, settings.KeyAIEnabled, "1", false, "admin"); err != nil {
		t.Fatalf("enable runtime: %v", err)
	}
	enabled, err = service.Enabled(ctx)
	if err != nil {
		t.Fatalf("Enabled after update: %v", err)
	}
	if !enabled {
		t.Fatal("runtime AIEnabled=true should enable AI")
	}
}
