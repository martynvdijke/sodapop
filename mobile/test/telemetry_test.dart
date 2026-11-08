import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/telemetry/telemetry.dart';

class _StubAdapter implements HttpClientAdapter {
  _StubAdapter({
    this.body = '{"endpoint":"https://otel.example.test","enabled":true}',
    this.status = 200,
    this.throwError = false,
  });

  final String body;
  final int status;
  final bool throwError;
  final List<RequestOptions> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    if (throwError) throw Exception('offline');
    return ResponseBody.fromString(
      body,
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

Dio _dio(_StubAdapter adapter) => Dio()..httpClientAdapter = adapter;

void main() {
  group('normalizeEndpoint', () {
    test('appends the traces path once', () {
      expect(
        Telemetry.normalizeEndpoint('https://otel.example.test'),
        'https://otel.example.test/v1/traces',
      );
      expect(
        Telemetry.normalizeEndpoint('  https://otel.example.test///  '),
        'https://otel.example.test/v1/traces',
      );
      expect(
        Telemetry.normalizeEndpoint('https://otel.example.test/v1/traces'),
        'https://otel.example.test/v1/traces',
      );
    });
  });

  group('parseOtelHeaders', () {
    test('splits comma-separated key=value pairs and decodes values', () {
      final headers = Telemetry.parseOtelHeaders(
        'Authorization=Bearer%20abc123, x-api-key=secret, malformed, =novalue',
      );
      expect(headers, {
        'Authorization': 'Bearer abc123',
        'x-api-key': 'secret',
      });
    });

    test('returns an empty map for empty input', () {
      expect(Telemetry.parseOtelHeaders(''), isEmpty);
      expect(Telemetry.parseOtelHeaders('   '), isEmpty);
    });
  });

  group('buildOtlpPayload', () {
    test('emits the OTLP resource, scope and span shape', () {
      final span = TraceSpan(
        name: 'screen home',
        attributes: const {'screen.name': 'home'},
        traceId: 'a' * 32,
        spanId: 'b' * 16,
        startNanos: 1000,
      );
      final payload = buildOtlpPayload(
        serviceName: 'sodapop-android',
        serviceVersion: '1.2.3',
        span: span,
        attributes: const {
          'screen.name': 'home',
          'cache.hit': true,
          'retry.count': 2,
          'latency.ms': 1.5,
        },
        failed: false,
        endNanos: 2000,
      );

      final resourceSpans = (payload['resourceSpans'] as List).single as Map;
      final resource = resourceSpans['resource'] as Map;
      final resourceAttrs = {
        for (final attr in resource['attributes'] as List)
          (attr as Map)['key']: (attr['value'] as Map).values.single,
      };
      expect(resourceAttrs['service.name'], 'sodapop-android');
      expect(resourceAttrs['service.version'], '1.2.3');
      expect(resourceAttrs['telemetry.sdk.name'], 'sodapop-mobile');
      expect(resourceAttrs['telemetry.sdk.language'], 'dart');

      final scopeSpans = (resourceSpans['scopeSpans'] as List).single as Map;
      expect((scopeSpans['scope'] as Map)['name'], 'sodapop.mobile');
      final emitted = (scopeSpans['spans'] as List).single as Map;
      expect(emitted['traceId'], 'a' * 32);
      expect(emitted['spanId'], 'b' * 16);
      expect(emitted['name'], 'screen home');
      expect(emitted['kind'], 1);
      expect(emitted['startTimeUnixNano'], '1000');
      expect(emitted['endTimeUnixNano'], '2000');
      expect((emitted['status'] as Map)['code'], 1);

      final spanAttrs = {
        for (final attr in emitted['attributes'] as List)
          (attr as Map)['key']: (attr['value'] as Map),
      };
      expect(spanAttrs['screen.name'], {'stringValue': 'home'});
      expect(spanAttrs['cache.hit'], {'boolValue': true});
      expect(spanAttrs['retry.count'], {'intValue': '2'});
      expect(spanAttrs['latency.ms'], {'doubleValue': 1.5});
    });

    test('marks a failed span with status code 2', () {
      final span = TraceSpan(
        name: 'request',
        attributes: const {},
        traceId: 'c' * 32,
        spanId: 'd' * 16,
        startNanos: 1,
      );
      final payload = buildOtlpPayload(
        serviceName: 'sodapop-android',
        serviceVersion: '1.0.0',
        span: span,
        attributes: const {'error.message': 'boom'},
        failed: true,
        endNanos: 2,
      );
      final spans =
          (((payload['resourceSpans'] as List).single as Map)['scopeSpans']
                  as List)
              .single as Map;
      expect(((spans['spans'] as List).single as Map)['status'], {'code': 2});
    });
  });

  group('OtelSettings.fromJson', () {
    test('a blank or absent endpoint disables everything', () {
      expect(OtelSettings.fromJson(const {}).usable, isFalse);
      final noEndpoint = OtelSettings.fromJson(const {
        'endpoint': '',
        'enabled': true,
      });
      expect(noEndpoint.enabled, isFalse);
      expect(noEndpoint.tracesEnabled, isFalse);
      expect(noEndpoint.usable, isFalse);
    });

    test('accepts alternate key names and defaults traces to enabled', () {
      final settings = OtelSettings.fromJson(const {
        'otel_endpoint': 'https://otel.example.test',
        'otel_enabled': true,
      });
      expect(settings.endpoint, 'https://otel.example.test');
      expect(settings.enabled, isTrue);
      expect(settings.tracesEnabled, isTrue);
      expect(settings.metricsEnabled, isFalse);
      expect(settings.logsEnabled, isFalse);
      expect(settings.usable, isTrue);
    });

    test('honours an explicit traces_enabled=false', () {
      final settings = OtelSettings.fromJson(const {
        'endpoint': 'https://otel.example.test',
        'enabled': true,
        'traces_enabled': false,
        'metrics_enabled': true,
        'logs_enabled': true,
      });
      expect(settings.enabled, isTrue);
      expect(settings.tracesEnabled, isFalse);
      expect(settings.metricsEnabled, isTrue);
      expect(settings.logsEnabled, isTrue);
      expect(settings.usable, isFalse);
    });

    test('tolerates string booleans and numbers', () {
      final settings = OtelSettings.fromJson(const {
        'endpoint': 'https://otel.example.test',
        'enabled': 'true',
        'traces_enabled': '1',
        'metrics_enabled': 0,
      });
      expect(settings.enabled, isTrue);
      expect(settings.tracesEnabled, isTrue);
      expect(settings.metricsEnabled, isFalse);
    });
  });

  group('fetchOtelSettings', () {
    test('parses a successful response from the expected path', () async {
      final adapter = _StubAdapter(
        body:
            '{"endpoint":"https://otel.example.test","enabled":true,'
            '"traces_enabled":true,"metrics_enabled":false,"logs_enabled":false}',
      );
      final settings = await fetchOtelSettings(
        'https://soda.test/',
        dio: _dio(adapter),
      );
      expect(settings, isNotNull);
      expect(settings!.endpoint, 'https://otel.example.test');
      expect(settings.usable, isTrue);
      expect(adapter.requests.single.path, 'https://soda.test/api/otel-settings');
      expect(adapter.requests.single.method, 'GET');
    });

    test('returns null for a non-200 response', () async {
      final adapter = _StubAdapter(body: '{}', status: 404);
      final settings = await fetchOtelSettings(
        'https://soda.test',
        dio: _dio(adapter),
      );
      expect(settings, isNull);
    });

    test('returns null when the request throws', () async {
      final adapter = _StubAdapter(throwError: true);
      final settings = await fetchOtelSettings(
        'https://soda.test',
        dio: _dio(adapter),
      );
      expect(settings, isNull);
    });

    test('returns null for a blank base URL without calling the network', () async {
      final adapter = _StubAdapter();
      final settings = await fetchOtelSettings('  ', dio: _dio(adapter));
      expect(settings, isNull);
      expect(adapter.requests, isEmpty);
    });

    test('returns null when the body is not a JSON object', () async {
      final adapter = _StubAdapter(body: '"nope"');
      final settings = await fetchOtelSettings(
        'https://soda.test',
        dio: _dio(adapter),
      );
      expect(settings, isNull);
    });
  });
}
