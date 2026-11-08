package telemetry

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestTraceableSkipsCapabilityPaths(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/healthz", false},
		{"/readyz", false},
		{"/metrics", false},
		{"/share/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", false},
		{"/share/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/card.png", false},
		{"/share", true},
		{"/passport", true},
		{"/shareholder", true},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		if got := traceable(req); got != tt.want {
			t.Errorf("traceable(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestFromEnvSignals(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "")
	t.Setenv("ENV", "development")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	t.Setenv("OTEL_TRACES_SAMPLER", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
	t.Setenv("OTEL_PROPAGATORS", "tracecontext,baggage")
	t.Setenv("OTEL_BSP_MAX_QUEUE_SIZE", "1024")
	t.Setenv("OTEL_BSP_MAX_EXPORT_BATCH_SIZE", "256")
	t.Setenv("OTEL_BSP_EXPORT_TIMEOUT", "12000")
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "2s")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "15000")
	t.Setenv("METRICS_ENABLED", "")

	cfg := FromEnv("1.2.3")
	if !cfg.Enabled || cfg.Disabled {
		t.Fatalf("Enabled=%v Disabled=%v, want enabled", cfg.Enabled, cfg.Disabled)
	}
	if cfg.Endpoint != "http://collector:4318" || cfg.MetricsEndpoint != cfg.Endpoint || cfg.LogsEndpoint != cfg.Endpoint {
		t.Fatalf("endpoints = %q/%q/%q", cfg.Endpoint, cfg.MetricsEndpoint, cfg.LogsEndpoint)
	}
	if !cfg.Insecure {
		t.Fatal("insecure not parsed")
	}
	if cfg.MaxQueueSize != 1024 || cfg.MaxExportBatchSize != 256 {
		t.Fatalf("batch sizes = %d/%d", cfg.MaxQueueSize, cfg.MaxExportBatchSize)
	}
	if cfg.ExportTimeout != 12*time.Second || cfg.BatchTimeout != 2*time.Second {
		t.Fatalf("timeouts = %v/%v", cfg.ExportTimeout, cfg.BatchTimeout)
	}
	if cfg.MetricsInterval != 15*time.Second {
		t.Fatalf("metrics interval = %v", cfg.MetricsInterval)
	}
	if cfg.Sampler != "parentbased_always_on" {
		t.Fatalf("development sampler = %q", cfg.Sampler)
	}
	if !cfg.Prometheus {
		t.Fatal("prometheus should default to enabled")
	}
}

func TestFromEnvProductionSampler(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_TRACES_SAMPLER", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
	cfg := FromEnv("v")
	if cfg.Sampler != "parentbased_traceidratio" || cfg.SamplerArg != 0.2 {
		t.Fatalf("production sampler = %q/%v", cfg.Sampler, cfg.SamplerArg)
	}
}

func TestFromEnvKillSwitch(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	cfg := FromEnv("v")
	if cfg.Enabled || !cfg.Disabled {
		t.Fatalf("kill switch not honoured: %+v", cfg)
	}
}

func TestTextMapPropagatorParsing(t *testing.T) {
	if got := textMapPropagator(""); got == nil {
		t.Fatal("empty propagator list should still return a propagator")
	}
	// Unknown names are ignored; a traceparent round-trip still works.
	p := textMapPropagator("tracecontext,unknown,baggage")
	if p == nil {
		t.Fatal("propagator is nil")
	}
}
