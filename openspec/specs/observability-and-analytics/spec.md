# Observability (OpenTelemetry) and Analytics (Umami) Specification

## Purpose

Defines Sodapop's OpenTelemetry (`v1.47.0`) traces, metrics, and logs integration and privacy-preserving Umami analytics integration.

## Requirements

### Requirement: OpenTelemetry Traces, Metrics, and Correlated Logs
The system SHALL instrument HTTP requests (`otelhttp v0.72.0`), database queries (`db.TraceQueryEx`), outbound HTTP clients (OFF, Nominatim, OpenCode AI, OIDC), and structured logs (`otelslog v0.21.0`) using `go.opentelemetry.io/otel v1.47.0`.

#### Scenario: Explicit environment parsing and kill switch
- **WHEN** the server starts
- **THEN** `internal/config` explicitly parses `OTEL_SDK_DISABLED`, `OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_INSECURE`, `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`, `OTEL_PROPAGATORS`, and `OTEL_BSP_*`
- **AND** when `OTEL_SDK_DISABLED=true` or `OTEL_EXPORTER_OTLP_ENDPOINT` is empty, no OTLP network connections are attempted
- **AND** `/healthz`, `/readyz`, and `/metrics` are excluded from HTTP tracing spans

### Requirement: Self-Hosted Umami Analytics with Allowlisted Events and Opt-Out
When `UMAMI_URL` and `UMAMI_WEBSITE_ID` are configured, the system SHALL inject the Umami tracker script and allowlist custom events without PII.

#### Scenario: Per-user opt-out suppresses tracker injection
- **WHEN** a user enables `analytics_opt_out=1` in settings (or when `UMAMI_URL` is unset)
- **THEN** the server omits the Umami `<script>` tag from rendered HTML and the Flutter Android client disables `/api/send` telemetry
- **AND** emitted events are restricted to the compile-time allowlist (`signup_started`, `email_verified`, `checkin_created`, `barcode_scan_started`, `barcode_scan_result`, `drink_created`, `search_used`, `venue_viewed`, `friend_added`, `theme_changed`, `list_created`, `badge_earned`)
