import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';

/// One server-sent event from `/api/v1/notifications/stream`.
///
/// The stream carries two event types: `unread` with `{"count": N}` and
/// `notification` with the same fields as an inbox row. `close` asks the client
/// to reconnect after roughly 30 minutes.
@immutable
class NotificationStreamEvent {
  const NotificationStreamEvent({required this.type, required this.data});

  final String type;
  final Map<String, dynamic> data;

  /// The unread badge count from an `unread` event, when present.
  int? get count {
    final value = data['count'];
    if (value is int) return value;
    if (value is num) return value.toInt();
    return int.tryParse('${value ?? ''}');
  }
}

/// Incremental parser for the `text/event-stream` wire format.
///
/// Feed it decoded text as it arrives; it returns only complete frames, so a
/// chunk may split a line (or a frame) without corrupting the next event.
class SseParser {
  String _pending = '';
  String _event = '';
  final List<String> _data = [];

  List<NotificationStreamEvent> add(String chunk) {
    _pending += chunk;
    final frames = <NotificationStreamEvent>[];
    while (true) {
      final newline = _pending.indexOf('\n');
      if (newline < 0) break;
      var line = _pending.substring(0, newline);
      _pending = _pending.substring(newline + 1);
      if (line.endsWith('\r')) {
        line = line.substring(0, line.length - 1);
      }
      if (line.isEmpty) {
        final frame = _flush();
        if (frame != null) frames.add(frame);
        continue;
      }
      if (line.startsWith(':')) continue; // comment / keep-alive
      final colon = line.indexOf(':');
      final field = colon < 0 ? line : line.substring(0, colon);
      var value = colon < 0 ? '' : line.substring(colon + 1);
      if (value.startsWith(' ')) value = value.substring(1);
      if (field == 'event') _event = value;
      if (field == 'data') _data.add(value);
    }
    return frames;
  }

  NotificationStreamEvent? _flush() {
    if (_event.isEmpty && _data.isEmpty) return null;
    final type = _event.isEmpty ? 'message' : _event;
    Map<String, dynamic> data = const {};
    final raw = _data.join('\n');
    if (raw.isNotEmpty) {
      try {
        final decoded = jsonDecode(raw);
        if (decoded is Map<String, dynamic>) data = decoded;
      } on FormatException {
        // A malformed frame is dropped rather than closing the stream.
      }
    }
    _event = '';
    _data.clear();
    return NotificationStreamEvent(type: type, data: data);
  }
}

/// Keeps one authenticated SSE connection to `GET /api/v1/notifications/stream`
/// open while the app is in the foreground.
///
/// This is the mobile counterpart of the web EventSource. Firebase Cloud
/// Messaging still owns notifications while the app is backgrounded or closed;
/// this stream makes the open app update immediately, with no pull interval.
class NotificationCenter extends ChangeNotifier {
  NotificationCenter({HttpClient Function()? httpClientFactory})
    : _httpClientFactory = httpClientFactory ?? HttpClient.new;

  static final NotificationCenter instance = NotificationCenter();

  static const Duration _minRetry = Duration(seconds: 1);
  static const Duration _maxRetry = Duration(seconds: 30);

  final HttpClient Function() _httpClientFactory;
  final StreamController<NotificationStreamEvent> _events =
      StreamController<NotificationStreamEvent>.broadcast();

  Uri? _uri;
  String? _token;
  bool _running = false;
  bool _connected = false;
  bool _disposed = false;
  int _unread = 0;
  int _generation = 0;

  /// Latest unread count reported by the server.
  int get unread => _unread;

  /// Whether a stream is currently established (not merely retrying).
  bool get connected => _connected;

  /// Every complete frame, for screens that want to react to new rows.
  Stream<NotificationStreamEvent> get events => _events.stream;

  /// Starts (or restarts) the stream for a signed-in server and token. Calling
  /// it repeatedly with the same pair is a no-op.
  void start({required String serverUrl, required String token}) {
    final base = serverUrl.trim().replaceFirst(RegExp(r'/+$'), '');
    if (base.isEmpty || token.isEmpty) return;
    final uri = Uri.parse('$base/api/v1/notifications/stream');
    if (_running && _uri == uri && _token == token) return;
    _stopLoop();
    _uri = uri;
    _token = token;
    _running = true;
    final generation = ++_generation;
    unawaited(_run(uri, token, generation));
  }

  /// Stops the stream and leaves the last known unread count in place.
  void stop() {
    _stopLoop();
    _setConnected(false);
  }

  void _stopLoop() {
    _running = false;
    _generation++;
  }

  Future<void> _run(Uri uri, String token, int generation) async {
    var delay = _minRetry;
    while (_running && generation == _generation) {
      HttpClient? client;
      try {
        client = _httpClientFactory();
        client.connectionTimeout = const Duration(seconds: 15);
        // Heartbeats arrive every 20s; keep the socket from being retired
        // between them.
        client.idleTimeout = const Duration(seconds: 90);
        final request = await client.getUrl(uri);
        request.headers.set(HttpHeaders.acceptHeader, 'text/event-stream');
        request.headers.set(HttpHeaders.authorizationHeader, 'Bearer $token');
        request.headers.set(HttpHeaders.cacheControlHeader, 'no-store');
        final response = await request.close();
        if (response.statusCode == HttpStatus.unauthorized ||
            response.statusCode == HttpStatus.forbidden) {
          // The token is dead; wait for the next sign-in instead of retrying.
          _running = false;
          break;
        }
        if (response.statusCode != HttpStatus.ok) {
          throw HttpException('notification stream: ${response.statusCode}');
        }
        _setConnected(true);
        delay = _minRetry;
        final parser = SseParser();
        await for (final chunk in response.transform(utf8.decoder)) {
          if (!_running || generation != _generation) break;
          for (final event in parser.add(chunk)) {
            _dispatch(event);
          }
        }
      } catch (_) {
        // Network churn is expected on mobile; reconnect below.
      } finally {
        client?.close(force: true);
        if (generation == _generation) _setConnected(false);
      }
      if (!_running || generation != _generation) return;
      await Future<void>.delayed(delay);
      delay = delay * 2 > _maxRetry ? _maxRetry : delay * 2;
    }
  }

  void _dispatch(NotificationStreamEvent event) {
    switch (event.type) {
      case 'unread':
        final count = event.count;
        if (count != null && count != _unread) {
          _unread = count;
          _notify();
        }
      case 'notification':
        if (!_events.isClosed) _events.add(event);
      default:
      // `close` (server-initiated reconnect) and anything unknown are handled
      // by the read loop ending.
    }
  }

  void _setConnected(bool value) {
    if (_connected == value) return;
    _connected = value;
    _notify();
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _stopLoop();
    _events.close();
    super.dispose();
  }
}
