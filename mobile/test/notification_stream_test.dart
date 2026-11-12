import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/notification_stream.dart';

void main() {
  group('SseParser', () {
    test('assembles a frame split across chunks', () {
      final parser = SseParser();
      expect(parser.add('event: unread\nda'), isEmpty);

      final frames = parser.add('ta: {"count": 3}\n\n');
      expect(frames, hasLength(1));
      expect(frames.single.type, 'unread');
      expect(frames.single.count, 3);
    });

    test('emits several frames from one chunk and skips comments', () {
      final parser = SseParser();
      final frames = parser.add(
        ': keep-alive\n\n'
        'event: unread\ndata: {"count":1}\n\n'
        'event: notification\ndata: {"id":"n1","kind":"toast"}\n\n',
      );

      expect(frames.map((frame) => frame.type), ['unread', 'notification']);
      expect(frames.last.data['kind'], 'toast');
      expect(frames.first.data['kind'], isNull);
    });

    test('drops malformed data instead of closing the stream', () {
      final parser = SseParser();
      final frames = parser.add('event: notification\ndata: not-json\n\n');

      expect(frames, hasLength(1));
      expect(frames.single.type, 'notification');
      expect(frames.single.data, isEmpty);
    });
  });

  test('streams events with the bearer token', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));
    final authHeaders = <String?>[];
    server.listen((request) async {
      authHeaders.add(request.headers.value(HttpHeaders.authorizationHeader));
      request.response.headers.contentType = ContentType(
        'text',
        'event-stream',
      );
      request.response.write('event: unread\ndata: {"count":2}\n\n');
      request.response.write(
        'event: notification\ndata: {"id":"n1","kind":"comment"}\n\n',
      );
      await request.response.close();
    });

    final center = NotificationCenter();
    addTearDown(center.dispose);
    final received = center.events.firstWhere(
      (event) => event.type == 'notification',
    );
    center.start(
      serverUrl: 'http://127.0.0.1:${server.port}/',
      token: 'sd_test',
    );
    final event = await received.timeout(const Duration(seconds: 5));

    expect(authHeaders.single, 'Bearer sd_test');
    expect(event.data['kind'], 'comment');
    expect(center.unread, 2);
    center.stop();
  });

  test('a rejected token stops instead of retrying', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));
    var requests = 0;
    server.listen((request) async {
      requests++;
      request.response.statusCode = HttpStatus.unauthorized;
      await request.response.close();
    });

    final center = NotificationCenter();
    addTearDown(center.dispose);
    center.start(serverUrl: 'http://127.0.0.1:${server.port}', token: 'bad');

    await Future<void>.delayed(const Duration(milliseconds: 400));
    expect(requests, 1);
    expect(center.connected, isFalse);
  });
}
