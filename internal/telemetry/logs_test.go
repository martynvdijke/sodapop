package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSetupLogsNoop(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "disabled", cfg: Config{Disabled: true, LogsEndpoint: "http://127.0.0.1:4318"}},
		{name: "no endpoint", cfg: Config{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, shutdown, err := SetupLogs(context.Background(), tt.cfg)
			if err != nil {
				t.Fatalf("SetupLogs: %v", err)
			}
			if handler != nil {
				t.Fatal("handler should be nil when logs are off")
			}
			if shutdown == nil {
				t.Fatal("shutdown is nil")
			}
			if err := shutdown(context.Background()); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
		})
	}
}

func TestFanoutWritesEveryHandler(t *testing.T) {
	var a, b bytes.Buffer
	h := Fanout(
		slog.NewTextHandler(&a, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)
	log := slog.New(h).With("component", "test")
	log.InfoContext(context.Background(), "fanout", "n", 1)

	for name, out := range map[string]string{"first": a.String(), "second": b.String()} {
		if !strings.Contains(out, "fanout") || !strings.Contains(out, "n=1") || !strings.Contains(out, "component=test") {
			t.Fatalf("%s handler output = %q", name, out)
		}
	}
}

func TestFanoutRespectsLevels(t *testing.T) {
	var info, debug bytes.Buffer
	h := Fanout(
		slog.NewTextHandler(&info, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewTextHandler(&debug, &slog.HandlerOptions{Level: slog.LevelDebug}),
	)
	if !h.Enabled(context.Background(), slog.LevelDebug) || !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("fanout should be enabled when any child handles the level")
	}
	slog.New(h).Debug("only-debug")
	if strings.Contains(info.String(), "only-debug") {
		t.Fatal("info handler received a debug record")
	}
	if !strings.Contains(debug.String(), "only-debug") {
		t.Fatal("debug handler did not receive the record")
	}
}
