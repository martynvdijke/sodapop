package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestHTTPClientEmitsClientSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := HTTPClient(server.Client(), "off")
	resp, err := client.Get(server.URL + "/product/123")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("no client span recorded")
	}
	span := spans[len(spans)-1]
	if got := span.Name(); got != "off GET" {
		t.Fatalf("span name = %q, want %q", got, "off GET")
	}
	if span.SpanKind() != trace.SpanKindClient {
		t.Fatalf("span kind = %v, want client", span.SpanKind())
	}
}

func TestHTTPClientWithoutProviderIsNoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := HTTPClient(nil, "off").Get(server.URL); err != nil {
		t.Fatalf("GET without tracer provider: %v", err)
	}
}
