package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFCMUnsetStaysDisabled(t *testing.T) {
	t.Setenv("FCM_SERVICE_ACCOUNT_JSON", "")
	t.Setenv("FCM_PROJECT_ID", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FCMServiceAccountJSON != "" || cfg.FCMProjectID != "" {
		t.Fatalf("FCM config = %q / %q, want empty", cfg.FCMServiceAccountJSON, cfg.FCMProjectID)
	}
}

func TestLoadFCMMalformedDoesNotFailStartup(t *testing.T) {
	t.Setenv("FCM_SERVICE_ACCOUNT_JSON", "{not json")
	t.Setenv("FCM_PROJECT_ID", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned %v, want nil", err)
	}
	if cfg.FCMServiceAccountJSON != "{not json" {
		t.Fatalf("stored service account = %q", cfg.FCMServiceAccountJSON)
	}
}

func TestLoadFCMUnreadableFileDoesNotFailStartup(t *testing.T) {
	t.Setenv("FCM_SERVICE_ACCOUNT_JSON", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("FCM_PROJECT_ID", "")
	if _, err := Load(); err != nil {
		t.Fatalf("Load returned %v, want nil", err)
	}
}

func TestLoadFCMDerivesProjectIDFromJSON(t *testing.T) {
	t.Setenv("FCM_SERVICE_ACCOUNT_JSON", `{"project_id":"derived-project"}`)
	t.Setenv("FCM_PROJECT_ID", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FCMProjectID != "derived-project" {
		t.Fatalf("project id = %q, want derived-project", cfg.FCMProjectID)
	}
}

func TestLoadFCMReadsServiceAccountFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service-account.json")
	if err := os.WriteFile(path, []byte(`{"project_id":"file-project"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("FCM_SERVICE_ACCOUNT_JSON", path)
	t.Setenv("FCM_PROJECT_ID", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FCMServiceAccountJSON != `{"project_id":"file-project"}` {
		t.Fatalf("service account = %q", cfg.FCMServiceAccountJSON)
	}
	if cfg.FCMProjectID != "file-project" {
		t.Fatalf("project id = %q", cfg.FCMProjectID)
	}
}
