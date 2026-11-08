package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// SetupLogs installs an OTLP log provider and returns an slog.Handler that
// exports records alongside the local stdout handler. It returns a nil handler
// when logs are disabled or no logs endpoint is configured, in which case the
// caller keeps its existing handler unchanged.
//
// The bridge only attaches trace_id/span_id when the record is emitted with a
// context (slog.*Context); callers should use the Context variants on request
// paths.
func SetupLogs(ctx context.Context, cfg Config) (slog.Handler, func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if cfg.Disabled || cfg.LogsEndpoint == "" {
		return nil, noop, nil
	}
	res, err := resourceFor(cfg)
	if err != nil {
		return nil, nil, err
	}
	opts := []otlploghttp.Option{otlploghttp.WithEndpointURL(signalURL(cfg.LogsEndpoint, "logs"))}
	if cfg.Insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	if cfg.ExportTimeout > 0 {
		opts = append(opts, otlploghttp.WithTimeout(cfg.ExportTimeout))
	}
	exporter, err := otlploghttp.New(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("logs exporter: %w", err)
	}
	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter,
			sdklog.WithExportInterval(cfg.BatchTimeout),
			sdklog.WithExportTimeout(cfg.ExportTimeout),
			sdklog.WithMaxQueueSize(cfg.MaxQueueSize),
			sdklog.WithExportMaxBatchSize(cfg.MaxExportBatchSize),
		)),
	)
	handler := otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(provider))
	return handler, provider.Shutdown, nil
}

// Fanout returns a handler that forwards each record to every handler whose
// level permits it. It is used to keep JSON/console output on stdout while the
// same record is exported over OTLP.
func Fanout(handlers ...slog.Handler) slog.Handler {
	kept := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			kept = append(kept, h)
		}
	}
	return &fanoutHandler{handlers: kept}
}

type fanoutHandler struct {
	handlers []slog.Handler
}

func (f *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f *fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, h := range f.handlers {
		if !h.Enabled(ctx, record.Level) {
			continue
		}
		if err := h.Handle(ctx, record.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, 0, len(f.handlers))
	for _, h := range f.handlers {
		next = append(next, h.WithAttrs(attrs))
	}
	return &fanoutHandler{handlers: next}
}

func (f *fanoutHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, 0, len(f.handlers))
	for _, h := range f.handlers {
		next = append(next, h.WithGroup(name))
	}
	return &fanoutHandler{handlers: next}
}
