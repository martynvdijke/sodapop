// Package telemetry wires OpenTelemetry tracing for the HTTP server.
//
// Configuration is environment-only on purpose: OTEL_SDK_DISABLED,
// OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_EXPORTER_OTLP_{TRACES,METRICS,LOGS}_ENDPOINT,
// OTEL_EXPORTER_OTLP_INSECURE, OTEL_SERVICE_NAME, OTEL_SERVICE_VERSION,
// OTEL_TRACES_SAMPLER(_ARG), OTEL_PROPAGATORS and OTEL_BSP_*.
// When no OTLP endpoint is configured the matching signal is a no-op.
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// Default batch-span settings from the OpenTelemetry specification.
const (
	defaultBatchTimeout       = 5 * time.Second
	defaultExportTimeout      = 30 * time.Second
	defaultMaxQueueSize       = 2048
	defaultMaxExportBatchSize = 512
	defaultMetricsInterval    = 30 * time.Second
)

// Config is resolved from the environment.
type Config struct {
	Enabled         bool
	Disabled        bool
	Endpoint        string
	MetricsEndpoint string
	LogsEndpoint    string
	ServiceName     string
	ServiceVersion  string
	Environment     string
	Sampler         string
	SamplerArg      float64
	Insecure        bool
	Propagators     string

	BatchTimeout       time.Duration
	ExportTimeout      time.Duration
	MaxQueueSize       int
	MaxExportBatchSize int
	MetricsInterval    time.Duration

	// Prometheus enables the local /metrics scrape endpoint. It is
	// independent of the OTLP metrics endpoint.
	Prometheus bool
}

// FromEnv reads the standard OTEL variables. The Go SDK does not apply the
// sampler, propagator, or batch variables automatically, so they are parsed
// here.
func FromEnv(version string) Config {
	disabled := strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true")
	environment := env("ENV", "development")
	cfg := Config{
		Disabled:        disabled,
		ServiceName:     env("OTEL_SERVICE_NAME", "sodapop"),
		ServiceVersion:  env("OTEL_SERVICE_VERSION", version),
		Environment:     environment,
		Endpoint:        env("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", env("OTEL_EXPORTER_OTLP_ENDPOINT", "")),
		MetricsEndpoint: env("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", env("OTEL_EXPORTER_OTLP_ENDPOINT", "")),
		LogsEndpoint:    env("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", env("OTEL_EXPORTER_OTLP_ENDPOINT", "")),
		Insecure:        boolEnv("OTEL_EXPORTER_OTLP_INSECURE"),
		Propagators:     env("OTEL_PROPAGATORS", "tracecontext,baggage"),

		BatchTimeout:       durationEnv("OTEL_BSP_SCHEDULE_DELAY", defaultBatchTimeout),
		ExportTimeout:      durationEnv("OTEL_BSP_EXPORT_TIMEOUT", defaultExportTimeout),
		MaxQueueSize:       intEnv("OTEL_BSP_MAX_QUEUE_SIZE", defaultMaxQueueSize),
		MaxExportBatchSize: intEnv("OTEL_BSP_MAX_EXPORT_BATCH_SIZE", defaultMaxExportBatchSize),
		MetricsInterval:    durationEnv("OTEL_METRIC_EXPORT_INTERVAL", defaultMetricsInterval),
		Prometheus:         boolEnvDefault("METRICS_ENABLED", true),
	}

	// Sampling is explicit; production defaults to a 20% ratio, local
	// development keeps every trace.
	cfg.Sampler = env("OTEL_TRACES_SAMPLER", "parentbased_always_on")
	cfg.SamplerArg = 1
	if cfg.Environment == "production" {
		cfg.Sampler = env("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
		cfg.SamplerArg = 0.2
	}
	if v := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.SamplerArg = f
		}
	}
	cfg.Enabled = !disabled && cfg.Endpoint != ""
	return cfg
}

// Setup installs a tracer provider and returns a shutdown function.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}
	res, err := resourceFor(cfg)
	if err != nil {
		return nil, err
	}
	exporter, err := otlptracehttp.New(ctx, traceExporterOptions(cfg)...)
	if err != nil {
		return nil, fmt.Errorf("telemetry exporter: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(cfg.BatchTimeout),
			sdktrace.WithExportTimeout(cfg.ExportTimeout),
			sdktrace.WithMaxQueueSize(cfg.MaxQueueSize),
			sdktrace.WithMaxExportBatchSize(cfg.MaxExportBatchSize),
		),
		sdktrace.WithSampler(sampler(cfg.Sampler, cfg.SamplerArg)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(textMapPropagator(cfg.Propagators))
	return provider.Shutdown, nil
}

// Handler traces requests except liveness, scrape, and capability-link paths.
// The matched trace identifier is echoed as X-Trace-ID so operators can
// correlate a response with the collector without reading logs.
func Handler(next http.Handler) http.Handler {
	traced := otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			w.Header().Set("X-Trace-ID", sc.TraceID().String())
		}
		next.ServeHTTP(w, r)
	}), "http.server", otelhttp.WithFilter(traceable))
	return traced
}

// traceable reports whether a request may be exported as a span. Share URLs
// carry a bearer capability token in the path, so they are never traced:
// otherwise every preview fetch would copy the secret into OTel attributes.
func traceable(r *http.Request) bool {
	switch r.URL.Path {
	case "/healthz", "/readyz", "/metrics":
		return false
	}
	return !strings.HasPrefix(r.URL.Path, "/share/")
}

// signalURL appends the standard OTLP signal path when the configured endpoint
// is a bare collector URL. WithEndpointURL treats the value as the final URL,
// so http://collector:4318 would otherwise POST to "/" instead of the signal
// path the collector expects.
func signalURL(raw, signal string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/v1/" + signal
	}
	return u.String()
}

func traceExporterOptions(cfg Config) []otlptracehttp.Option {
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(signalURL(cfg.Endpoint, "traces"))}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if cfg.ExportTimeout > 0 {
		opts = append(opts, otlptracehttp.WithTimeout(cfg.ExportTimeout))
	}
	return opts
}

func resourceFor(cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
	}
	if cfg.Environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment.name", cfg.Environment))
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, attrs...))
	if err != nil {
		return nil, fmt.Errorf("telemetry resource: %w", err)
	}
	return res, nil
}

func sampler(name string, arg float64) sdktrace.Sampler {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "always_off":
		return sdktrace.NeverSample()
	case "always_on":
		return sdktrace.AlwaysSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(arg)
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(arg))
	default:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

// textMapPropagator maps the comma-separated OTEL_PROPAGATORS value onto the
// propagators this process supports. Unknown names are ignored; "none"
// disables propagation entirely.
func textMapPropagator(raw string) propagation.TextMapPropagator {
	props := make([]propagation.TextMapPropagator, 0, 2)
	for _, name := range strings.Split(raw, ",") {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "tracecontext":
			props = append(props, propagation.TraceContext{})
		case "baggage":
			props = append(props, propagation.Baggage{})
		}
	}
	return propagation.NewCompositeTextMapPropagator(props...)
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func boolEnv(key string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(key)))
	return err == nil && b
}

func boolEnvDefault(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func intEnv(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// durationEnv accepts a Go duration ("5s") or milliseconds, which is what the
// OTEL_BSP_* variables use.
func durationEnv(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return fallback
}
