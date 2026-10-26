package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func newTestMetrics(t *testing.T) (*Metrics, *metric.ManualReader) {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	m, err := NewMetrics(provider)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	return m, reader
}

func collect(t *testing.T, reader *metric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return rm
}

func findMetric(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

func attrs(set attribute.Set) map[string]string {
	out := make(map[string]string, len(set.ToSlice()))
	for _, kv := range set.ToSlice() {
		out[string(kv.Key)] = kv.Value.Emit()
	}
	return out
}

func TestSetupMetricsNoop(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "disabled flag", cfg: Config{Disabled: true, MetricsEndpoint: "http://127.0.0.1:4318"}},
		{name: "no endpoint", cfg: Config{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shutdown, err := SetupMetrics(context.Background(), tt.cfg)
			if err != nil {
				t.Fatalf("SetupMetrics: %v", err)
			}
			if shutdown == nil {
				t.Fatal("shutdown is nil")
			}
			if err := shutdown(context.Background()); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
			if defaultMetrics.Load() != nil {
				t.Fatal("default metrics should stay unset")
			}
		})
	}
}

func TestHTTPMiddlewareRecordsRoute(t *testing.T) {
	m, reader := newTestMetrics(t)
	defaultMetrics.Store(m)
	t.Cleanup(func() { defaultMetrics.Store(nil) })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /drinks/{slug}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := HTTPMiddleware(mux)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/drinks/abc123", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing/abc123", nil))

	rm := collect(t, reader)
	counter := findMetric(t, rm, "http.server.requests")
	sum, ok := counter.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("requests data = %T, want Sum[int64]", counter.Data)
	}

	got := map[string]int64{}
	for _, dp := range sum.DataPoints {
		a := attrs(dp.Attributes)
		got[a["http.route"]+" "+a["http.status_code"]] = dp.Value
		if strings.Contains(a["http.route"], "abc123") {
			t.Fatalf("route label leaked raw identifier: %q", a["http.route"])
		}
	}
	if got["GET /drinks/{slug} 200"] != 1 {
		t.Fatalf("matched route count = %d, want 1 (points: %v)", got["GET /drinks/{slug} 200"], got)
	}
	if got["other 404"] != 1 {
		t.Fatalf("unmatched route count = %d, want 1 (points: %v)", got["other 404"], got)
	}

	histogram := findMetric(t, rm, "http.server.duration")
	hist, ok := histogram.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("duration data = %T, want Histogram[float64]", histogram.Data)
	}
	var count uint64
	for _, dp := range hist.DataPoints {
		count += dp.Count
		if dp.Sum < 0 {
			t.Fatalf("negative duration sum: %v", dp.Sum)
		}
	}
	if count != 2 {
		t.Fatalf("duration observations = %d, want 2", count)
	}
}

func TestRecordIntegrationResult(t *testing.T) {
	m, reader := newTestMetrics(t)
	m.RecordIntegration("matrix", nil)
	m.RecordIntegration("matrix", errors.New("boom"))

	rm := collect(t, reader)
	counter := findMetric(t, rm, "integration.deliveries")
	sum, ok := counter.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("deliveries data = %T, want Sum[int64]", counter.Data)
	}
	results := map[string]int64{}
	for _, dp := range sum.DataPoints {
		a := attrs(dp.Attributes)
		if a["kind"] != "matrix" {
			t.Fatalf("kind = %q, want matrix", a["kind"])
		}
		results[a["result"]] = dp.Value
	}
	if results["ok"] != 1 || results["error"] != 1 {
		t.Fatalf("results = %v, want ok=1 error=1", results)
	}
}

func TestOutboxDepthCallback(t *testing.T) {
	SetOutboxDepthFunc(func(context.Context) (int64, error) { return 7, nil })
	t.Cleanup(func() { SetOutboxDepthFunc(nil) })

	_, reader := newTestMetrics(t)
	rm := collect(t, reader)
	gauge := findMetric(t, rm, "delivery.outbox.depth")
	data, ok := gauge.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("outbox depth data = %T, want Gauge[int64]", gauge.Data)
	}
	if len(data.DataPoints) != 1 || data.DataPoints[0].Value != 7 {
		t.Fatalf("outbox depth points = %+v, want one point with value 7", data.DataPoints)
	}
}
