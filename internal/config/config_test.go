package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"DEBUG", slog.LevelDebug},
		{"  Info  ", slog.LevelInfo},
		{"", slog.LevelInfo},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseLogLevel(tc.input)
			if err != nil {
				t.Fatalf("ParseLogLevel(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseLogLevelRejectsUnknown(t *testing.T) {
	got, err := ParseLogLevel("verbose")
	if err == nil {
		t.Fatal("ParseLogLevel accepted an unknown level")
	}
	if got != slog.LevelInfo {
		t.Fatalf("fallback level = %v, want info", got)
	}
}

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
