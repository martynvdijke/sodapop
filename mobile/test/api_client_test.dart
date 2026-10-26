import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';

class _StubAdapter implements HttpClientAdapter {
  final List<RequestOptions> requests = [];
  final List<String> bodies = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    final chunks = requestStream == null
        ? const <Uint8List>[]
        : await requestStream.toList();
    bodies.add(
      utf8.decode(
        chunks.expand((chunk) => chunk).toList(),
        allowMalformed: true,
      ),
    );
    return ResponseBody.fromString(
      '{"data":{"id":"c1"}}',
      201,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

class _Harness {
  _Harness() {
    dio = Dio();
    adapter = _StubAdapter();
    dio.httpClientAdapter = adapter;
    captured = null;
    dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          final data = options.data;
          if (data is FormData) captured = data;
          handler.next(options);
        },
      ),
    );
    api = ApiClient(dio: dio)..configure('https://soda.test', token: 'sd_test');
  }

  late final Dio dio;
  late final _StubAdapter adapter;
  late final ApiClient api;
  FormData? captured;
}

void main() {
  group('createCheckin JSON', () {
    test('keeps the existing body and headers without a photo', () async {
      final h = _Harness();
      await h.api.createCheckin(
        drinkId: 7,
        rating: 17,
        note: '  Crisp  ',
        idempotencyKey: 'key-json',
      );

      final request = h.adapter.requests.single;
      expect(request.data, {'drink_id': 7, 'rating': 4.25, 'note': 'Crisp'});
      expect(request.headers['Authorization'], 'Bearer sd_test');
      expect(request.headers['Idempotency-Key'], 'key-json');
      expect(request.headers['traceparent'], isA<String>());
      expect(h.captured, isNull);
    });

    test('passes optional serving and visibility through as JSON', () async {
      final h = _Harness();
      await h.api.createCheckin(
        drinkId: 7,
        serving: 'can',
        visibility: 'friends',
        idempotencyKey: 'key-json-2',
      );

      expect(h.adapter.requests.single.data, {
        'drink_id': 7,
        'serving': 'can',
        'visibility': 'friends',
      });
    });
  });

  group('createCheckin multipart', () {
    test('sends scalar fields and the image bytes as form data', () async {
      final h = _Harness();
      final photo = Uint8List.fromList([1, 2, 3, 4]);
      await h.api.createCheckin(
        drinkId: 7,
        rating: 17,
        note: 'Crisp',
        venueId: 2,
        serving: 'can',
        visibility: 'friends',
        idempotencyKey: 'key-photo',
        photo: photo,
        photoFilename: 'cola.jpg',
        photoMime: 'image/jpeg',
      );

      final form = h.captured;
      expect(form, isNotNull);
      final fields = {for (final field in form!.fields) field.key: field.value};
      expect(fields, {
        'drink_id': '7',
        'venue_id': '2',
        'rating': '4.25',
        'note': 'Crisp',
        'serving': 'can',
        'visibility': 'friends',
        'idempotency_key': 'key-photo',
      });
      expect(form.files, hasLength(1));
      final file = form.files.single;
      expect(file.key, 'image');
      expect(file.value.filename, 'cola.jpg');
      expect(file.value.length, 4);
      expect(file.value.contentType?.mimeType, 'image/jpeg');

      final request = h.adapter.requests.single;
      expect(request.data, isA<FormData>());
      expect(
        request.headers['content-type'],
        startsWith('multipart/form-data; boundary='),
      );
      expect(request.headers['Idempotency-Key'], 'key-photo');
      final body = h.adapter.bodies.single;
      expect(body, contains('name="drink_id"'));
      expect(body, contains('name="image"; filename="cola.jpg"'));
      expect(body, contains('content-type: image/jpeg'));
    });

    test('defaults the filename when none is given', () async {
      final h = _Harness();
      await h.api.createCheckin(
        drinkId: 7,
        photo: Uint8List.fromList([1]),
        idempotencyKey: 'key-default-name',
      );

      expect(h.captured!.files.single.value.filename, 'photo.jpg');
      // Dio infers the type from the fallback filename.
      expect(
        h.captured!.files.single.value.contentType?.mimeType,
        'image/jpeg',
      );
    });
  });

  group('media helpers', () {
    test('builds an authenticated media URL', () {
      final api = ApiClient()..configure('https://soda.test', token: 'sd_test');
      expect(api.mediaUrl('abc'), 'https://soda.test/api/v1/media/abc');
      expect(api.imageHeaders, {'Authorization': 'Bearer sd_test'});

      api.setToken(null);
      expect(api.imageHeaders, isEmpty);
    });
  });
}
