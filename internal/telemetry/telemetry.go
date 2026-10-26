// Package telemetry wires OpenTelemetry tracing for the HTTP server.
//
// Configuration is environment-only on purpose: OTEL_SDK_DISABLED,
// OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_EXPORTER_OTLP_TRACES_ENDPOINT,
// OTEL_SERVICE_NAME, OTEL_SERVICE_VERSION and OTEL_TRACES_SAMPLER(_ARG).
// When no OTLP endpoint is configured tracing is a no-op.
package telemetry

import (
	"context"
	"fmt"
	"net/http"
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
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// Config is resolved from the environment.
type Config struct {
	Enabled         bool
	Disabled        bool
	Endpoint        string
	MetricsEndpoint string
	ServiceName     string
	ServiceVersion  string
	Environment     string
	Sampler         string
	SamplerArg      float64
}

// FromEnv reads the standard OTEL variables. The Go SDK does not apply the
// sampler or propagator variables automatically, so they are parsed here.
func FromEnv(version string) Config {
	disabled := strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true")
	cfg := Config{
		Disabled:        disabled,
		ServiceName:     env("OTEL_SERVICE_NAME", "sodapop"),
		ServiceVersion:  env("OTEL_SERVICE_VERSION", version),
		Environment:     env("ENV", "development"),
		Sampler:         env("OTEL_TRACES_SAMPLER", "parentbased_always_on"),
		Endpoint:        env("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", env("OTEL_EXPORTER_OTLP_ENDPOINT", "")),
		MetricsEndpoint: env("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", env("OTEL_EXPORTER_OTLP_ENDPOINT", "")),
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
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("telemetry exporter: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithSampler(sampler(cfg.Sampler, cfg.SamplerArg)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	return provider.Shutdown, nil
}

// Handler traces requests except liveness and scrape endpoints.
func Handler(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.server",
		otelhttp.WithFilter(func(r *http.Request) bool {
			switch r.URL.Path {
			case "/healthz", "/readyz", "/metrics":
				return false
			}
			return true
		}),
	)
}

func resourceFor(cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
	}
	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(cfg.Environment))
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

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
