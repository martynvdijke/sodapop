package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const meterName = "sodapop/internal/telemetry"

// OutboxDepthFunc reports the number of pending deliveries in a queue. It is
// injected so this package never opens a database handle. Returning an error
// leaves the last observed value untouched.
type OutboxDepthFunc func(context.Context) (int64, error)

var (
	defaultMetrics atomic.Pointer[Metrics]

	outboxMu     sync.RWMutex
	outboxSource OutboxDepthFunc
)

// Metrics owns the request and delivery instruments shared across the server.
type Metrics struct {
	httpDuration          metric.Float64Histogram
	httpRequests          metric.Int64Counter
	integrationDeliveries metric.Int64Counter
	outboxDepth           metric.Int64ObservableGauge
}

// NewMetrics builds the instrument set on the given provider. Tests pass a
// provider backed by a manual reader; production passes the SDK provider.
func NewMetrics(provider metric.MeterProvider) (*Metrics, error) {
	meter := provider.Meter(meterName)

	duration, err := meter.Float64Histogram(
		"http.server.duration",
		metric.WithUnit("s"),
		metric.WithDescription("HTTP server request duration in seconds"),
	)
	if err != nil {
		return nil, fmt.Errorf("http duration histogram: %w", err)
	}
	requests, err := meter.Int64Counter(
		"http.server.requests",
		metric.WithDescription("HTTP server requests by route and status"),
	)
	if err != nil {
		return nil, fmt.Errorf("http requests counter: %w", err)
	}
	deliveries, err := meter.Int64Counter(
		"integration.deliveries",
		metric.WithDescription("Outbound integration delivery attempts"),
	)
	if err != nil {
		return nil, fmt.Errorf("integration deliveries counter: %w", err)
	}
	depth, err := meter.Int64ObservableGauge(
		"delivery.outbox.depth",
		metric.WithDescription("Pending outbound deliveries"),
	)
	if err != nil {
		return nil, fmt.Errorf("outbox depth gauge: %w", err)
	}
	m := &Metrics{
		httpDuration:          duration,
		httpRequests:          requests,
		integrationDeliveries: deliveries,
		outboxDepth:           depth,
	}
	if _, err := meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		source := outboxDepthSource()
		if source == nil {
			return nil
		}
		value, err := source(ctx)
		if err != nil {
			return nil
		}
		o.ObserveInt64(depth, value)
		return nil
	}, depth); err != nil {
		return nil, fmt.Errorf("outbox depth callback: %w", err)
	}
	return m, nil
}

// MetricsSetup bundles provider shutdown with the optional Prometheus scrape
// handler. Prometheus is nil when METRICS_ENABLED is false or the SDK is
// disabled.
type MetricsSetup struct {
	Shutdown   func(context.Context) error
	Prometheus http.Handler
}

// SetupMetrics installs an OTLP metric provider (when configured) and, by
// default, a Prometheus reader served at /metrics. With the SDK disabled it is
// a no-op.
func SetupMetrics(ctx context.Context, cfg Config) (*MetricsSetup, error) {
	noop := func(context.Context) error { return nil }
	if cfg.Disabled || (cfg.MetricsEndpoint == "" && !cfg.Prometheus) {
		defaultMetrics.Store(nil)
		return &MetricsSetup{Shutdown: noop}, nil
	}
	res, err := resourceFor(cfg)
	if err != nil {
		return nil, err
	}

	options := []sdkmetric.Option{sdkmetric.WithResource(res)}
	var promHandler http.Handler

	if cfg.MetricsEndpoint != "" {
		exporterOptions := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpointURL(signalURL(cfg.MetricsEndpoint, "metrics")),
			otlpmetrichttp.WithTimeout(10 * time.Second),
		}
		if cfg.Insecure {
			exporterOptions = append(exporterOptions, otlpmetrichttp.WithInsecure())
		}
		exporter, err := otlpmetrichttp.New(ctx, exporterOptions...)
		if err != nil {
			return nil, fmt.Errorf("metrics exporter: %w", err)
		}
		interval := cfg.MetricsInterval
		if interval <= 0 {
			interval = 30 * time.Second
		}
		options = append(options, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(interval)),
		))
	}

	if cfg.Prometheus {
		registry := prometheus.NewRegistry()
		registry.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
		exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
		if err != nil {
			return nil, fmt.Errorf("prometheus exporter: %w", err)
		}
		options = append(options, sdkmetric.WithReader(exporter))
		promHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	}

	provider := sdkmetric.NewMeterProvider(options...)
	m, err := NewMetrics(provider)
	if err != nil {
		_ = provider.Shutdown(ctx)
		return nil, err
	}
	otel.SetMeterProvider(provider)
	defaultMetrics.Store(m)
	return &MetricsSetup{
		Shutdown: func(shutdownCtx context.Context) error {
			defaultMetrics.Store(nil)
			return provider.Shutdown(shutdownCtx)
		},
		Prometheus: promHandler,
	}, nil
}

// SetOutboxDepthFunc injects the queue depth callback used by the observable
// gauge. Passing nil disables observation.
func SetOutboxDepthFunc(fn OutboxDepthFunc) {
	outboxMu.Lock()
	outboxSource = fn
	outboxMu.Unlock()
}

func outboxDepthSource() OutboxDepthFunc {
	outboxMu.RLock()
	defer outboxMu.RUnlock()
	return outboxSource
}

func currentMetrics() *Metrics { return defaultMetrics.Load() }

// HTTPMiddleware records request count and duration for the route pattern
// matched by the ServeMux. Unmatched requests use the bounded "other" label;
// raw paths and identifiers are never recorded.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := currentMetrics()
		if m == nil {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		attrs := metric.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.route", routeLabel(r)),
			attribute.Int("http.status_code", rec.status),
		)
		m.httpRequests.Add(r.Context(), 1, attrs)
		m.httpDuration.Record(r.Context(), time.Since(start).Seconds(), attrs)
	})
}

// RecordIntegration records one delivery attempt by integration kind. It is
// safe to call when telemetry is disabled.
func (m *Metrics) RecordIntegration(kind string, err error) {
	if m == nil {
		return
	}
	result := "ok"
	if err != nil {
		result = "error"
	}
	m.integrationDeliveries.Add(context.Background(), 1,
		metric.WithAttributes(
			attribute.String("kind", kind),
			attribute.String("result", result),
		))
}

// RecordIntegration records a delivery against the process-wide instruments.
func RecordIntegration(kind string, err error) {
	currentMetrics().RecordIntegration(kind, err)
}

func routeLabel(r *http.Request) string {
	if pattern := strings.TrimSpace(r.Pattern); pattern != "" {
		return pattern
	}
	return "other"
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
