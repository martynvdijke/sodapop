package telemetry

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

func TestSetupLogsExportsToCollector(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/logs" {
			t.Errorf("path = %q, want /v1/logs", r.URL.Path)
		}
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		ServiceName:        "sodapop",
		ServiceVersion:     "test",
		Environment:        "test",
		LogsEndpoint:       server.URL,
		BatchTimeout:       10 * time.Millisecond,
		ExportTimeout:      2 * time.Second,
		MaxQueueSize:       64,
		MaxExportBatchSize: 16,
	}
	handler, shutdown, err := SetupLogs(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SetupLogs: %v", err)
	}
	if handler == nil {
		t.Fatal("handler is nil")
	}
	slog.New(handler).InfoContext(context.Background(), "export me")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if requests.Load() == 0 {
		t.Fatal("no OTLP log request reached the collector")
	}
}

func TestSetupTracesExportsToCollector(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("path = %q, want /v1/traces", r.URL.Path)
		}
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := Config{
		Enabled:            true,
		ServiceName:        "sodapop",
		ServiceVersion:     "test",
		Environment:        "test",
		Endpoint:           server.URL,
		BatchTimeout:       10 * time.Millisecond,
		ExportTimeout:      2 * time.Second,
		MaxQueueSize:       64,
		MaxExportBatchSize: 16,
		Sampler:            "always_on",
	}
	shutdown, err := Setup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	_, span := otel.Tracer("test").Start(context.Background(), "test-span")
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if requests.Load() == 0 {
		t.Fatal("no OTLP trace request reached the collector")
	}
}
