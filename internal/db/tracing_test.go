package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"sodapop/ent/style"
)

func installRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return recorder
}

func findSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %q not found among %d ended spans", name, len(spans))
	return nil
}

func spanAttribute(span sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value.AsString(), true
		}
	}
	return "", false
}

func TestDriverEmitsQuerySpansWithoutArguments(t *testing.T) {
	recorder := installRecorder(t)

	database, err := Init(":memory:")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	// Init itself runs schema creation and migrations, which emit spans; the
	// assertions below filter by name.
	const sentinel = "sentinel-style-name"
	if _, err := database.Client.Style.Query().Where(style.NameEQ(sentinel)).All(context.Background()); err != nil {
		t.Fatalf("query styles: %v", err)
	}

	spans := recorder.Ended()
	querySpans := 0
	var matched sdktrace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == "db.query" {
			querySpans++
			matched = span
		}
	}
	if querySpans == 0 {
		t.Fatalf("no db.query spans recorded among %d ended spans", len(spans))
	}
	if matched == nil {
		t.Fatal("no db.query span captured")
	}
	if got, ok := spanAttribute(matched, "db.system"); !ok || got != "sqlite" {
		t.Fatalf("db.system = %q (present=%v), want sqlite", got, ok)
	}
	if got, ok := spanAttribute(matched, "db.operation"); !ok || got != "query" {
		t.Fatalf("db.operation = %q (present=%v), want query", got, ok)
	}
	statement, ok := spanAttribute(matched, "db.statement")
	if !ok || statement == "" {
		t.Fatal("db.statement is missing or empty")
	}
	if strings.Contains(statement, sentinel) {
		t.Fatalf("db.statement leaked a bound argument: %q", statement)
	}
}

func TestTraceQueryExRecordsErrors(t *testing.T) {
	recorder := installRecorder(t)
	wantErr := errors.New("boom")

	err := TraceQueryEx(context.Background(), "exec", "SELECT 1", func(context.Context) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("TraceQueryEx error = %v, want %v", err, wantErr)
	}

	span := findSpan(t, recorder.Ended(), "db.exec")
	if span.Status().Code != codes.Error {
		t.Fatalf("span status = %v, want Error", span.Status().Code)
	}
	if span.SpanKind() != trace.SpanKindClient {
		t.Fatalf("span kind = %v, want Client", span.SpanKind())
	}
	if len(span.Events()) == 0 {
		t.Fatal("expected a recorded error event")
	}
	var system string
	for _, attr := range span.Attributes() {
		if attr.Key == attribute.Key("db.system") {
			system = attr.Value.AsString()
		}
	}
	if system != "sqlite" {
		t.Fatalf("db.system = %q, want sqlite", system)
	}
}
