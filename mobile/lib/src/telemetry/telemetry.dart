import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

/// Compile-time telemetry configuration. It is only a fallback: the endpoint is
/// normally fetched at runtime from the server's public settings route so a
/// self-hosted instance can point its own app at its own collector.
abstract final class TelemetryConfig {
  static const otelEndpoint = String.fromEnvironment(
    'OTEL_EXPORTER_OTLP_ENDPOINT',
  );
  static const otelHeaders = String.fromEnvironment(
    'OTEL_EXPORTER_OTLP_HEADERS',
  );
  static const serviceName = String.fromEnvironment(
    'OTEL_SERVICE_NAME',
    defaultValue: 'sodapop-android',
  );
  static const serviceVersion = String.fromEnvironment(
    'OTEL_SERVICE_VERSION',
    defaultValue: '1.0.0',
  );
  static const umamiUrl = String.fromEnvironment('UMAMI_URL');
  static const umamiWebsiteId = String.fromEnvironment('UMAMI_WEBSITE_ID');

  static bool get otelEnabled => otelEndpoint.trim().isNotEmpty && !kIsWeb;
  static bool get umamiEnabled =>
      umamiUrl.trim().isNotEmpty && umamiWebsiteId.trim().isNotEmpty;
}

/// Allowlisted analytics events. Keys and values are enforced at the boundary,
/// so drink names, notes, handles and searches can never be sent.
const Map<String, Set<String>> umamiEventAllowlist = {
  'screen_view': {'screen'},
  'drink_scan': {'source'},
  'checkin_submitted': {'mode'},
  'sync_started': {},
  'theme_changed': {'theme'},
};

const Set<String> _screens = {
  'home',
  'drink',
  'scan',
  'checkins',
  'checkin_form',
  'profile',
  'settings',
  'onboarding',
};
const Set<String> _themes = {'system', 'light', 'dark'};
const Set<String> _modes = {'create', 'edit'};
const Set<String> _sources = {'camera', 'manual'};

Map<String, Object>? sanitizeUmamiEvent(
  String event, [
  Map<String, Object?> props = const {},
]) {
  final allowed = umamiEventAllowlist[event];
  if (allowed == null) return null;
  final clean = <String, Object>{};
  for (final entry in props.entries) {
    if (!allowed.contains(entry.key)) continue;
    final value = entry.value;
    if (value is bool || value is num) {
      clean[entry.key] = value as Object;
      continue;
    }
    if (value is! String || value.length > 64) continue;
    if (entry.key == 'screen' && !_screens.contains(value)) continue;
    if (entry.key == 'theme' && !_themes.contains(value)) continue;
    if (entry.key == 'mode' && !_modes.contains(value)) continue;
    if (entry.key == 'source' && !_sources.contains(value)) continue;
    clean[entry.key] = value;
  }
  if (allowed.isNotEmpty && clean.isEmpty) return null;
  return clean;
}

String randomHex(int bytes) {
  final random = Random.secure();
  final buffer = StringBuffer();
  for (var i = 0; i < bytes; i++) {
    buffer.write(random.nextInt(256).toRadixString(16).padLeft(2, '0'));
  }
  return buffer.toString();
}

String buildTraceparent(String traceId, String spanId) =>
    '00-$traceId-$spanId-01';

/// The public observability view returned by `GET /api/otel-settings`.
///
/// Every field is optional on the wire: an older or different server must never
/// crash the app, so unknown shapes simply resolve to "disabled".
class OtelSettings {
  const OtelSettings({
    this.endpoint = '',
    this.enabled = false,
    this.tracesEnabled = false,
    this.metricsEnabled = false,
    this.logsEnabled = false,
  });

  final String endpoint;
  final bool enabled;
  final bool tracesEnabled;
  final bool metricsEnabled;
  final bool logsEnabled;

  /// Whether traces should actually be exported: an opted-in server with a
  /// usable endpoint.
  bool get usable => enabled && tracesEnabled && endpoint.trim().isNotEmpty;

  factory OtelSettings.fromJson(Map<String, dynamic> json) {
    final rawEndpoint = _pickString(json, const ['endpoint', 'otel_endpoint']);
    final endpoint = rawEndpoint.trim();
    final hasEndpoint = endpoint.isNotEmpty;
    final enabled = _pickBool(json, const ['enabled', 'otel_enabled']);
    final traces = _pickBool(json, const ['traces_enabled']);
    // `traces_enabled` defaults to `enabled`; either may drive the other so a
    // server that only publishes one flag still works.
    final resolvedEnabled = (enabled ?? traces ?? false) && hasEndpoint;
    final resolvedTraces =
        (traces ?? enabled ?? false) && resolvedEnabled && hasEndpoint;
    return OtelSettings(
      endpoint: endpoint,
      enabled: resolvedEnabled,
      tracesEnabled: resolvedTraces,
      metricsEnabled: (_pickBool(json, const ['metrics_enabled']) ?? false) &&
          hasEndpoint,
      logsEnabled:
          (_pickBool(json, const ['logs_enabled']) ?? false) && hasEndpoint,
    );
  }

  static String _pickString(Map<String, dynamic> json, List<String> keys) {
    for (final key in keys) {
      final value = json[key];
      if (value is String) return value;
    }
    return '';
  }

  static bool? _pickBool(Map<String, dynamic> json, List<String> keys) {
    for (final key in keys) {
      final value = json[key];
      if (value is bool) return value;
      if (value is num) return value != 0;
      if (value is String) {
        final normalized = value.trim().toLowerCase();
        if (normalized == 'true' || normalized == '1') return true;
        if (normalized == 'false' || normalized == '0') return false;
      }
    }
    return null;
  }
}

/// Fetches the server's observability settings. It never throws: an offline
/// device, a timeout, a non-200 answer or a malformed body all return null so
/// startup is never blocked or crashed by telemetry.
Future<OtelSettings?> fetchOtelSettings(
  String baseUrl, {
  Dio? dio,
  Duration timeout = const Duration(seconds: 3),
}) async {
  final base = baseUrl.trim().replaceAll(RegExp(r'/+$'), '');
  if (base.isEmpty) return null;
  final client = dio ?? Dio();
  try {
    final response = await client
        .get<dynamic>(
          '$base/api/otel-settings',
          options: Options(
            responseType: ResponseType.json,
            receiveTimeout: timeout,
            sendTimeout: timeout,
          ),
        )
        .timeout(timeout);
    if (response.statusCode != 200) return null;
    final data = response.data;
    if (data is! Map) return null;
    return OtelSettings.fromJson(data.cast<String, dynamic>());
  } catch (_) {
    return null;
  }
}

class TraceSpan {
  TraceSpan({
    required this.name,
    required this.attributes,
    required this.traceId,
    required this.spanId,
    required this.startNanos,
    this.kind = 1,
  });

  final String name;
  final Map<String, Object> attributes;
  final String traceId;
  final String spanId;
  final int startNanos;
  final int kind;
  bool _ended = false;
  int? statusCode;
  String error = '';

  String get traceparent => buildTraceparent(traceId, spanId);

  void end({
    int? httpStatus,
    Object? failure,
    Map<String, Object> extra = const {},
  }) {
    if (_ended) return;
    _ended = true;
    statusCode = httpStatus;
    if (failure != null) error = failure.toString();
    final attrs = <String, Object>{...attributes, ...extra};
    if (httpStatus != null) attrs['http.response.status_code'] = httpStatus;
    if (error.isNotEmpty) attrs['error.message'] = _short(error);
    Telemetry.instance._finish(
      this,
      attrs,
      error.isNotEmpty || (httpStatus != null && httpStatus >= 500),
    );
  }

  String _short(String value) =>
      value.length > 200 ? '${value.substring(0, 200)}…' : value;
}

Map<String, Object?> buildOtlpPayload({
  required String serviceName,
  required String serviceVersion,
  required TraceSpan span,
  required Map<String, Object> attributes,
  required bool failed,
  required int endNanos,
}) {
  List<Map<String, Object?>> attrs(Map<String, Object> values) => values.entries
      .map(
        (entry) => {
          'key': entry.key,
          'value': switch (entry.value) {
            final bool value => {'boolValue': value},
            final int value => {'intValue': value.toString()},
            final double value => {'doubleValue': value},
            _ => {'stringValue': entry.value.toString()},
          },
        },
      )
      .toList();

  return {
    'resourceSpans': [
      {
        'resource': {
          'attributes': attrs({
            'service.name': serviceName,
            'service.version': serviceVersion,
            'telemetry.sdk.name': 'sodapop-mobile',
            'telemetry.sdk.language': 'dart',
          }),
        },
        'scopeSpans': [
          {
            'scope': {'name': 'sodapop.mobile', 'version': serviceVersion},
            'spans': [
              {
                'traceId': span.traceId,
                'spanId': span.spanId,
                'name': span.name,
                'kind': span.kind,
                'startTimeUnixNano': span.startNanos.toString(),
                'endTimeUnixNano': endNanos.toString(),
                'attributes': attrs(attributes),
                'status': {'code': failed ? 2 : 1},
              },
            ],
          },
        ],
      },
    ],
  };
}

class Telemetry {
  Telemetry._();

  static final Telemetry instance = Telemetry._();

  static const String _scopeName = 'sodapop.mobile';
  static const String _umamiHost = 'sodapop.app';
  static const Duration _flushInterval = Duration(seconds: 5);
  static const int _maxQueue = 20;

  Dio? _client;
  Timer? _timer;
  final List<Map<String, Object?>> _queue = [];
  bool _started = false;
  String _tracesEndpoint = '';
  Map<String, String> _headers = const {};

  bool get otelEnabled => _tracesEndpoint.isNotEmpty;
  bool get umamiEnabled => TelemetryConfig.umamiEnabled;

  /// Starts exporting spans to [endpoint]. When omitted, the compile-time
  /// `OTEL_EXPORTER_OTLP_ENDPOINT` is used. Safe to call repeatedly; the most
  /// recently supplied endpoint wins.
  Future<void> start({
    String? endpoint,
    Map<String, String>? headers,
  }) async {
    if (kIsWeb) return;
    final raw = (endpoint ?? TelemetryConfig.otelEndpoint).trim();
    if (raw.isEmpty) return;
    final normalized = normalizeEndpoint(raw);
    if (_started && normalized == _tracesEndpoint) return;
    _started = true;
    _tracesEndpoint = normalized;
    _headers = headers ?? parseOtelHeaders(TelemetryConfig.otelHeaders);
    _client ??= Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 5),
        receiveTimeout: const Duration(seconds: 10),
        headers: {'User-Agent': TelemetryConfig.serviceName},
      ),
    );
    _timer ??= Timer.periodic(_flushInterval, (_) => _flush());
    final span = beginSpan('app.start');
    span.end();
  }

  /// Resolves the collector from the server's public settings route and starts
  /// exporting. A server that answers but has observability switched off wins
  /// over the compile-time fallback; an unreachable server falls back to it.
  Future<void> startFromServer(String baseUrl) async {
    if (kIsWeb) return;
    final settings = await fetchOtelSettings(baseUrl);
    if (settings != null && settings.usable) {
      await start(endpoint: settings.endpoint);
      return;
    }
    if (settings != null) return;
    await start();
  }

  TraceSpan beginSpan(
    String name, {
    Map<String, Object> attributes = const {},
    int kind = 1,
  }) {
    return TraceSpan(
      name: name,
      attributes: attributes,
      traceId: randomHex(16),
      spanId: randomHex(8),
      startNanos: DateTime.now().microsecondsSinceEpoch * 1000,
      kind: kind,
    );
  }

  void screen(String name) {
    final span = beginSpan('screen $name', attributes: {'screen.name': name});
    span.end();
    track('screen_view', {'screen': name});
  }

  void track(String event, [Map<String, Object?> props = const {}]) {
    final clean = sanitizeUmamiEvent(event, props);
    if (clean == null || !TelemetryConfig.umamiEnabled) return;
    final client = _client ??= Dio();
    final base = TelemetryConfig.umamiUrl.replaceAll(RegExp(r'/+$'), '');
    final uri = Uri.tryParse('$base/api/send');
    if (uri == null) return;
    final body = {
      'type': 'event',
      'payload': {
        'website': TelemetryConfig.umamiWebsiteId,
        'hostname': _umamiHost,
        'url': '/app/$event',
        'event': event,
        'data': clean,
      },
    };
    unawaited(
      client
          .postUri<dynamic>(
            uri,
            data: jsonEncode(body),
            options: Options(contentType: Headers.jsonContentType),
          )
          .then((_) {}, onError: (Object _) {}),
    );
  }

  void _finish(TraceSpan span, Map<String, Object> attributes, bool failed) {
    if (!otelEnabled || kIsWeb) return;
    final payload = buildOtlpPayload(
      serviceName: TelemetryConfig.serviceName,
      serviceVersion: TelemetryConfig.serviceVersion,
      span: span,
      attributes: attributes,
      failed: failed,
      endNanos: DateTime.now().microsecondsSinceEpoch * 1000,
    );
    _queue.add(payload);
    if (_queue.length >= _maxQueue) _flush();
  }

  void _flush() {
    if (_queue.isEmpty || _client == null || _tracesEndpoint.isEmpty) return;
    final spans = <Map<String, Object?>>[];
    for (final payload in _queue) {
      final resourceSpans = payload['resourceSpans'];
      if (resourceSpans is List && resourceSpans.isNotEmpty) {
        final resource = resourceSpans.first;
        if (resource is Map) {
          final scopeSpans = resource['scopeSpans'];
          if (scopeSpans is List && scopeSpans.isNotEmpty) {
            final scope = scopeSpans.first;
            if (scope is Map && scope['spans'] is List) {
              spans.addAll(
                (scope['spans'] as List).cast<Map<String, Object?>>(),
              );
            }
          }
        }
      }
    }
    _queue.clear();
    if (spans.isEmpty) return;
    final payload = {
      'resourceSpans': [
        {
          'resource': {
            'attributes': [
              {
                'key': 'service.name',
                'value': {'stringValue': TelemetryConfig.serviceName},
              },
              {
                'key': 'service.version',
                'value': {'stringValue': TelemetryConfig.serviceVersion},
              },
              {
                'key': 'telemetry.sdk.name',
                'value': {'stringValue': 'sodapop-mobile'},
              },
              {
                'key': 'telemetry.sdk.language',
                'value': {'stringValue': 'dart'},
              },
            ],
          },
          'scopeSpans': [
            {
              'scope': {'name': _scopeName},
              'spans': spans,
            },
          ],
        },
      ],
    };
    unawaited(
      _client!
          .postUri<dynamic>(
            Uri.parse(_tracesEndpoint),
            data: jsonEncode(payload),
            options: Options(
              contentType: Headers.jsonContentType,
              headers: _headers,
            ),
          )
          .then(
            (response) {
              if ((response.statusCode ?? 0) >= 400 && kDebugMode) {
                debugPrint('OTLP export failed: ${response.statusCode}');
              }
            },
            onError: (Object error) {
              if (kDebugMode) debugPrint('OTLP export failed: $error');
            },
          ),
    );
  }

  Future<void> shutdown() async {
    _timer?.cancel();
    _timer = null;
    _flush();
    await Future<void>.delayed(const Duration(milliseconds: 200));
    _client?.close(force: true);
    _client = null;
    _started = false;
    _tracesEndpoint = '';
  }

  /// Appends `/v1/traces` unless the path is already a complete traces URL.
  static String normalizeEndpoint(String raw) {
    final base = raw.trim().replaceAll(RegExp(r'/+$'), '');
    if (base.endsWith('/v1/traces')) return base;
    return '$base/v1/traces';
  }

  static Map<String, String> parseOtelHeaders(String raw) {
    final out = <String, String>{};
    for (final pair in raw.split(',')) {
      final idx = pair.indexOf('=');
      if (idx <= 0) continue;
      final key = pair.substring(0, idx).trim();
      var value = pair.substring(idx + 1).trim();
      try {
        value = Uri.decodeComponent(value);
      } catch (_) {}
      if (key.isNotEmpty && value.isNotEmpty) out[key] = value;
    }
    return out;
  }
}
