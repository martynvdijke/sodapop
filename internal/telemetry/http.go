package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// HTTPTransport instruments outbound HTTP calls with a client span and
// propagates W3C trace context. operation is a low-cardinality name such as
// "off" or "ai"; the span is named "operation METHOD" rather than including a
// path or identifier.
//
// A nil base uses http.DefaultTransport. Instrumentation is safe when no
// tracer provider is installed: the global provider is a no-op.
func HTTPTransport(base http.RoundTripper, operation string) http.RoundTripper {
	return otelhttp.NewTransport(base,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return operation + " " + r.Method
		}),
	)
}

// HTTPClient returns an outbound client whose transport is instrumented for
// the given operation name. Callers keep their own timeout.
func HTTPClient(client *http.Client, operation string) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	clone := *client
	clone.Transport = HTTPTransport(client.Transport, operation)
	return &clone
}
