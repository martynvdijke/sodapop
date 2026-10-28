import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';

class _StubAdapter implements HttpClientAdapter {
  _StubAdapter({this.body = '{"data":{"id":"c1"}}', this.status = 201});

  final String body;
  final int status;
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

class _Harness {
  _Harness({String body = '{"data":{"id":"c1"}}', int status = 201}) {
    dio = Dio();
    adapter = _StubAdapter(body: body, status: status);
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

File _tempImage([List<int> bytes = const [1, 2, 3, 4]]) {
  final dir = Directory.systemTemp.createTempSync('sodapop-api-test');
  addTearDown(() {
    if (dir.existsSync()) dir.deleteSync(recursive: true);
  });
  final file = File('${dir.path}/cola.jpg')..writeAsBytesSync(bytes);
  return file;
}

Map<String, String> _fields(FormData form) => {
  for (final field in form.fields) field.key: field.value,
};

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

    test('builds the poster URL', () {
      final api = ApiClient()..configure('https://soda.test', token: 'sd_test');
      expect(api.posterUrl('abc'), 'https://soda.test/media/abc/poster');
    });
  });

  group('drink detail', () {
    test('GETs the slug and parses the full DTO', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {
            'id': 5,
            'slug': 'cola',
            'name': 'Cola',
            'brand': 'Soda Co',
            'style': 'Cola',
            'caffeine_mg_per_100ml': 10,
            'sugar_g_per_100ml': 10.6,
            'flavours': [
              {'name': 'Cola', 'slug': 'cola', 'intensity': 4},
            ],
            'aliases': ['Coke'],
            'rater_count': 3,
            'rating_shown': true,
            'barcode_count': 2,
          },
        }),
        status: 200,
      );

      final drink = await h.api.drink('cola');
      final request = h.adapter.requests.single;
      expect(request.method, 'GET');
      expect(request.path, 'api/v1/drinks/cola');
      expect(drink.name, 'Cola');
      expect(drink.caffeineMgPer100ml, 10);
      expect(drink.flavours.single.intensity, 4);
      expect(drink.aliases, ['Coke']);
      expect(drink.barcodeCount, 2);
    });

    test('lists the gallery', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': [
            {'id': 9, 'media_id': 'm9', 'url': '/media/m9', 'is_primary': true},
          ],
        }),
        status: 200,
      );

      final images = await h.api.drinkImages('cola');
      expect(h.adapter.requests.single.path, 'api/v1/drinks/cola/images');
      expect(images.single.id, 9);
      expect(images.single.thumbUrl, '/media/m9/thumb');
      expect(images.single.isPrimary, isTrue);
    });

    test('uploads a gallery image with the attribution field', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {'id': 10, 'media_id': 'm10', 'url': '/media/m10'},
        }),
      );
      final file = _tempImage();

      final image = await h.api.uploadDrinkImage(
        'cola',
        file: file,
        attribution: 'Ada',
      );

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/drinks/cola/images');
      expect(_fields(h.captured!), {'attribution': 'Ada'});
      expect(h.captured!.files.single.key, 'image');
      expect(h.captured!.files.single.value.filename, 'cola.jpg');
      expect(h.captured!.files.single.value.length, 4);
      expect(h.captured!.files.single.value.contentType?.mimeType, 'image/jpeg');
      expect(h.adapter.bodies.single, contains('name="image"; filename="cola.jpg"'));
      expect(image.id, 10);
    });

    test('promotes and deletes gallery images', () async {
      final promote = _Harness(
        body: '{"data":{"id":10,"is_primary":true}}',
        status: 200,
      );
      await promote.api.setDrinkImagePrimary(10);
      expect(promote.adapter.requests.single.method, 'POST');
      expect(
        promote.adapter.requests.single.path,
        'api/v1/drinks/images/10/primary',
      );

      final remove = _Harness(body: '', status: 204);
      await remove.api.deleteDrinkImage(10);
      expect(remove.adapter.requests.single.method, 'DELETE');
      expect(remove.adapter.requests.single.path, 'api/v1/drinks/images/10');
    });
  });

  group('check-in histories', () {
    test('myCheckins sends limit and before', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': [
            {
              'id': 'c1',
              'user_id': 'u1',
              'drink_id': 5,
              'drink_name': 'Cola',
              'drink_slug': 'cola',
              'rating': 17,
              'venue_id': 7,
              'venue_name': 'Corner Shop',
              'created_at': '2026-10-26T10:00:00Z',
              'photo_url': '/media/m1',
            },
          ],
        }),
        status: 200,
      );

      final items = await h.api.myCheckins(
        limit: 5,
        before: '2026-10-01T00:00:00Z',
      );

      final request = h.adapter.requests.single;
      expect(request.path, 'api/v1/me/checkins');
      expect(request.queryParameters, {
        'limit': 5,
        'before': '2026-10-01T00:00:00Z',
      });
      expect(items.single.rating, 17);
      expect(items.single.venueName, 'Corner Shop');
      expect(items.single.photoUrl, '/media/m1');
    });

    test('omits an empty cursor and defaults to 20', () async {
      final h = _Harness(body: '{"data":[]}', status: 200);
      await h.api.myCheckins(before: '  ');
      expect(h.adapter.requests.single.queryParameters, {'limit': 20});
    });

    test('userCheckins uses the handle path', () async {
      final h = _Harness(body: '{"data":[]}', status: 200);
      await h.api.userCheckins('ada', limit: 10);
      final request = h.adapter.requests.single;
      expect(request.path, 'api/v1/users/ada/checkins');
      expect(request.queryParameters, {'limit': 10});
    });
  });

  group('friends', () {
    test('reads the capitalised directory', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {
            'Friends': [
              {'id': 'u1', 'handle': 'ada', 'display_name': 'Ada'},
            ],
            'Incoming': [
              {'id': 'f1', 'handle': 'bob', 'display_name': 'Bob'},
            ],
            'Outgoing': <Object>[],
          },
        }),
        status: 200,
      );

      final directory = await h.api.friends();
      expect(h.adapter.requests.single.path, 'api/v1/friends');
      expect(directory.friends.single.handle, 'ada');
      expect(directory.incoming.single.handle, 'bob');
      expect(directory.outgoing, isEmpty);
    });

    test('posts a friend request with the handle', () async {
      final h = _Harness(
        body: '{"data":{"status":"requested"}}',
        status: 202,
      );
      await h.api.sendFriendRequest('ada');
      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/friends/requests');
      expect(request.data, {'handle': 'ada'});
    });
  });

  group('saved check-ins', () {
    test('lists saved check-ins with paging and parses saved', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': [
            {
              'id': 'c1',
              'user': {'handle': 'ada', 'display_name': 'Ada'},
              'drink': {'slug': 'cola', 'name': 'Cola'},
              'rating': 16,
              'saved': true,
            },
          ],
        }),
        status: 200,
      );

      final items = await h.api.savedCheckins(
        limit: 3,
        before: '2026-10-01T00:00:00Z',
      );
      final request = h.adapter.requests.single;
      expect(request.path, 'api/v1/me/saved');
      expect(request.queryParameters, {
        'limit': 3,
        'before': '2026-10-01T00:00:00Z',
      });
      expect(items.single.drinkName, 'Cola');
      expect(items.single.saved, isTrue);
    });

    test('saves and unsaves with POST and DELETE', () async {
      final save = _Harness(body: '{"data":{"saved":true}}', status: 200);
      await save.api.saveCheckin('c1');
      expect(save.adapter.requests.single.method, 'POST');
      expect(save.adapter.requests.single.path, 'api/v1/checkins/c1/save');

      final unsave = _Harness(body: '{"data":{"saved":false}}', status: 200);
      await unsave.api.unsaveCheckin('c1');
      expect(unsave.adapter.requests.single.method, 'DELETE');
      expect(unsave.adapter.requests.single.path, 'api/v1/checkins/c1/save');
    });
  });

  group('comment pages', () {
    test('GETs one page and parses the paging metadata', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': [
            {
              'id': 'm1',
              'user': {'handle': 'ada', 'display_name': 'Ada'},
              'body': 'Older',
              'created_at': '2026-10-25T10:00:00Z',
              'can_delete': false,
              'user_id': 'u1',
            },
          ],
          'meta': {'count': 1, 'has_more': true, 'next_before': 'm1'},
        }),
        status: 200,
      );

      final page = await h.api.commentPage(
        'c1',
        before: 'm9',
        limit: 30,
      );

      final request = h.adapter.requests.single;
      expect(request.method, 'GET');
      expect(request.path, 'api/v1/checkins/c1/comments');
      expect(request.queryParameters, {'limit': 30, 'before': 'm9'});
      expect(page.items.single.body, 'Older');
      expect(page.count, 1);
      expect(page.hasMore, isTrue);
      expect(page.nextBefore, 'm1');
    });

    test('defaults to 50 and omits an empty cursor', () async {
      final h = _Harness(body: '{"data":[],"meta":{"count":0}}', status: 200);

      final page = await h.api.commentPage('c1');

      expect(h.adapter.requests.single.queryParameters, {'limit': 50});
      expect(page.items, isEmpty);
      expect(page.hasMore, isFalse);
    });

    test('tolerates a page without meta', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': [
            {'id': 'm1', 'body': 'Just one'},
          ],
        }),
        status: 200,
      );

      final page = await h.api.commentPage('c1');

      expect(page.items.single.id, 'm1');
      expect(page.hasMore, isFalse);
      expect(page.nextBefore, '');
    });

    test('listComments delegates to the page reader', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': [
            {'id': 'm1', 'body': 'Older'},
            {'id': 'm2', 'body': 'Newer'},
          ],
          'meta': {'count': 2, 'has_more': false},
        }),
        status: 200,
      );

      final comments = await h.api.listComments('c1');

      final request = h.adapter.requests.single;
      expect(request.path, 'api/v1/checkins/c1/comments');
      expect(request.queryParameters, {'limit': 100});
      expect(comments.map((comment) => comment.id), ['m1', 'm2']);
    });
  });

  group('venue media', () {
    test('uploads with kind and label fields', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {
            'id': 'm1',
            'media_id': 'media-1',
            'kind': 'menu',
            'label': 'Front',
            'can_remove': true,
          },
        }),
      );
      final file = _tempImage();

      final media = await h.api.uploadVenueMedia(
        7,
        file: file,
        kind: 'menu',
        label: '  Front  ',
      );

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/venues/7/media');
      expect(_fields(h.captured!), {'kind': 'menu', 'label': 'Front'});
      expect(h.captured!.files.single.key, 'image');
      expect(h.captured!.files.single.value.filename, 'cola.jpg');
      expect(media.mediaId, 'media-1');
      expect(media.kind, 'menu');
      expect(media.canRemove, isTrue);
    });

    test('deletes venue media through the multiplexed path', () async {
      final h = _Harness(body: '', status: 204);
      await h.api.deleteVenueMedia('m1');
      final request = h.adapter.requests.single;
      expect(request.method, 'DELETE');
      expect(request.path, 'api/v1/venues/media/m1');
    });
  });

  group('lists', () {
    test('creates a list with name, description and visibility', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {
            'id': 'l1',
            'name': 'Favourites',
            'description': 'Best cans',
            'visibility': 'friends',
            'is_system': false,
            'item_count': 0,
          },
        }),
      );

      final list = await h.api.createList(
        name: 'Favourites',
        description: 'Best cans',
        visibility: 'friends',
      );

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/lists');
      expect(request.data, {
        'name': 'Favourites',
        'description': 'Best cans',
        'visibility': 'friends',
      });
      expect(list.id, 'l1');
      expect(list.visibility, 'friends');
    });

    test('updates only the supplied fields', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {'id': 'l1', 'name': 'Renamed', 'visibility': 'private'},
        }),
        status: 200,
      );

      final list = await h.api.updateList('l1', name: 'Renamed');

      final request = h.adapter.requests.single;
      expect(request.method, 'PATCH');
      expect(request.path, 'api/v1/lists/l1');
      expect(request.data, {'name': 'Renamed'});
      expect(list.name, 'Renamed');
    });

    test('deletes a list and handles 204', () async {
      final h = _Harness(body: '', status: 204);
      await h.api.deleteList('l1');

      final request = h.adapter.requests.single;
      expect(request.method, 'DELETE');
      expect(request.path, 'api/v1/lists/l1');
    });

    test('adds an existing item (200) and sends identifiers', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {
            'drink_id': 5,
            'slug': 'cola',
            'name': 'Cola',
            'brand': 'Soda Co',
            'note': 'Try chilled',
            'created_at': '2026-10-26T10:00:00Z',
          },
        }),
        status: 200,
      );

      final item = await h.api.addListItem(
        'l1',
        drinkId: 5,
        slug: 'cola',
        note: '  Try chilled  ',
      );

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/lists/l1/items');
      expect(request.data, {
        'drink_id': 5,
        'slug': 'cola',
        'note': 'Try chilled',
      });
      expect(item.drinkId, 5);
      expect(item.brand, 'Soda Co');
      expect(item.note, 'Try chilled');
    });

    test('adds a new item (201) with only a slug', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {'drink_id': 6, 'slug': 'lime', 'name': 'Lime'},
        }),
        status: 201,
      );

      final item = await h.api.addListItem('l1', slug: 'lime');

      expect(h.adapter.requests.single.data, {'slug': 'lime'});
      expect(item.slug, 'lime');
      expect(item.name, 'Lime');
    });

    test('removes an item by drink id', () async {
      final h = _Harness(body: '', status: 204);
      await h.api.removeListItem('l1', 5);

      final request = h.adapter.requests.single;
      expect(request.method, 'DELETE');
      expect(request.path, 'api/v1/lists/l1/items/5');
    });
  });

  group('compare', () {
    test('sends repeated drinks params and parses drinks', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': [
            {'id': 5, 'slug': 'cola', 'name': 'Cola'},
            {'id': 6, 'slug': 'lime', 'name': 'Lime'},
          ],
          'meta': {'count': 2},
        }),
        status: 200,
      );

      final drinks = await h.api.compare(['cola', 'lime']);

      final request = h.adapter.requests.single;
      expect(request.method, 'GET');
      expect(request.path, 'api/v1/compare');
      expect(request.uri.queryParametersAll['drinks'], ['cola', 'lime']);
      expect(drinks.map((drink) => drink.slug), ['cola', 'lime']);
    });
  });

  group('passport', () {
    test('reads the nested stamp collections', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {
            'stats': {'checkins': 4},
            'badges': <Object>[],
            'totals': {
              'checkins': 40,
              'styles': 5,
              'venue_kinds': 3,
              'countries': 2,
            },
            'styles': [
              {'name': 'Cola', 'count': 9, 'last_at': '2026-10-01'},
            ],
            'venue_kinds': [
              {'kind': 'shop', 'count': 4},
            ],
            'countries': [
              {'country': 'GB', 'count': 7},
            ],
            'recent': [
              {
                'name': 'Cola',
                'slug': 'cola',
                'style': 'Cola',
                'created_at': '2026-10-26T10:00:00Z',
                'rating': null,
              },
            ],
          },
        }),
        status: 200,
      );

      final passport = await h.api.passport();

      expect(h.adapter.requests.single.path, 'api/v1/passport');
      expect(passport.stats.checkins, 4);
      expect(passport.totals.countries, 2);
      expect(passport.styles.single.lastAt, '2026-10-01');
      expect(passport.venueKinds.single.kind, 'shop');
      expect(passport.countries.single.count, 7);
      expect(passport.recent.single.rating, isNull);
    });
  });

  group('AI drafts', () {
    const turnBody = {
      'session_id': 's1',
      'status': 'ready',
      'message': 'Here is a first draft.',
      'draft': {
        'name': 'Cherry Cola',
        'brand': 'Soda Co',
        'volume_ml': 355,
        'flavours': [
          {'name': 'Cherry', 'intensity': 3},
        ],
      },
      'upstream_session_id': 'up-1',
    };

    test('starts a draft with notes and a barcode', () async {
      final h = _Harness(
        body: jsonEncode({'data': turnBody}),
        status: 200,
      );

      final turn = await h.api.aiDraft(
        notes: '  Cherry cola, canned  ',
        barcode: '  5449000000996 ',
      );

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/ai/drafts');
      expect(request.data, {
        'notes': 'Cherry cola, canned',
        'barcode': '5449000000996',
      });
      expect(turn.sessionId, 's1');
      expect(turn.status, 'ready');
      expect(turn.message, 'Here is a first draft.');
      expect(turn.draft['name'], 'Cherry Cola');
      expect(turn.upstreamSessionId, 'up-1');
      expect(turn.canCommit, isTrue);
    });

    test('omits an empty barcode', () async {
      final h = _Harness(body: '{"data":{"session_id":"s1"}}', status: 200);
      await h.api.aiDraft(notes: 'Lime soda');
      expect(h.adapter.requests.single.data, {'notes': 'Lime soda'});
    });

    test('appends a message to the encoded session path', () async {
      final h = _Harness(
        body: jsonEncode({'data': turnBody}),
        status: 200,
      );

      final turn = await h.api.aiDraftMessage('s 1', '  Less sweet  ');

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/ai/drafts/s%201/messages');
      expect(request.data, {'content': 'Less sweet'});
      expect(turn.draft['volume_ml'], 355);
    });

    test('revises through the revise path', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {...turnBody, 'status': 'chatting'},
        }),
        status: 200,
      );

      final turn = await h.api.aiDraftRevise('s1', '  Make it drier  ');

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/ai/drafts/s1/revise');
      expect(request.data, {'instruction': 'Make it drier'});
      expect(turn.status, 'chatting');
    });

    test('commits and parses the provisional drink', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {
            'id': 12,
            'drink_id': 12,
            'slug': 'cherry-cola',
            'name': 'Cherry Cola',
            'status': 'provisional',
            'session_id': 's1',
            'message': 'provisional drink committed',
          },
        }),
        status: 200,
      );

      final commit = await h.api.aiDraftCommit('s1');

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/ai/drafts/s1/commit');
      expect(request.data, isNull);
      expect(commit.drinkId, 12);
      expect(commit.slug, 'cherry-cola');
      expect(commit.name, 'Cherry Cola');
      expect(commit.status, 'provisional');
      expect(commit.sessionId, 's1');
      expect(commit.message, 'provisional drink committed');
    });

    test('maps a disabled AI response to ApiException', () async {
      final h = _Harness(
        body:
            '{"error":{"code":"ai_disabled","message":"AI features are disabled"}}',
        status: 503,
      );

      await expectLater(
        h.api.aiDraft(notes: 'Lime soda'),
        throwsA(
          isA<ApiException>()
              .having((e) => e.message, 'message', 'AI features are disabled')
              .having((e) => e.statusCode, 'statusCode', 503),
        ),
      );
    });
  });

  group('settings', () {
    Map<String, Object?> settingsBody({List<Object> sessions = const []}) => {
      'data': {
        'profile': {
          'handle': 'ada',
          'display_name': 'Ada Lovelace',
          'bio': 'Fizz fan',
          'email': 'ada@soda.test',
          'email_verified': true,
          'role': 'admin',
        },
        'prefs': {
          'theme': 'dark',
          'units': 'imperial',
          'email_weekly': true,
          'email_friend_req': false,
          'push_enabled': true,
          'public_profile': true,
          'analytics_opt_out': true,
          'caffeine_bedtime': '21:30',
          'default_visibility': 'friends',
        },
        'sessions': sessions,
        'token_count': 2,
        'blocked_count': 1,
      },
    };

    test('GETs and parses the full settings payload', () async {
      final h = _Harness(
        body: jsonEncode(
          settingsBody(
            sessions: [
              {
                'id': 9,
                'ip': '203.0.113.7',
                'user_agent': 'Firefox',
                'created_at': '2026-10-20T10:00:00Z',
                'last_seen_at': '2026-10-27T10:00:00Z',
                'current': true,
              },
            ],
          ),
        ),
        status: 200,
      );

      final settings = await h.api.settings();

      final request = h.adapter.requests.single;
      expect(request.method, 'GET');
      expect(request.path, 'api/v1/me/settings');
      expect(request.headers['Authorization'], 'Bearer sd_test');
      expect(settings.profile.handle, 'ada');
      expect(settings.profile.emailVerified, isTrue);
      expect(settings.profile.role, 'admin');
      expect(settings.prefs.theme, 'dark');
      expect(settings.prefs.units, 'imperial');
      expect(settings.prefs.emailWeekly, isTrue);
      expect(settings.prefs.analyticsOptOut, isTrue);
      expect(settings.prefs.caffeineBedtime, '21:30');
      expect(settings.prefs.defaultVisibility, 'friends');
      expect(settings.sessions.single.id, 9);
      expect(settings.sessions.single.userAgent, 'Firefox');
      expect(settings.sessions.single.current, isTrue);
      expect(settings.tokenCount, 2);
      expect(settings.blockedCount, 1);
    });

    test('parses an empty sessions list', () async {
      final h = _Harness(body: jsonEncode(settingsBody()), status: 200);

      final settings = await h.api.settings();

      expect(settings.sessions, isEmpty);
      expect(settings.tokenCount, 2);
    });

    test('PATCHes only the supplied fields and parses the response', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {
            'profile': {'handle': 'ada', 'display_name': 'Ada Two'},
            'prefs': {'theme': 'light', 'email_weekly': false},
            'sessions': <Object>[],
            'token_count': 0,
            'blocked_count': 0,
          },
        }),
        status: 200,
      );

      final settings = await h.api.updateSettings(
        displayName: 'Ada Two',
        theme: 'light',
        emailWeekly: false,
        bio: '',
      );

      final request = h.adapter.requests.single;
      expect(request.method, 'PATCH');
      expect(request.path, 'api/v1/me/settings');
      expect(request.data, {
        'display_name': 'Ada Two',
        'bio': '',
        'theme': 'light',
        'email_weekly': false,
      });
      expect(settings.profile.displayName, 'Ada Two');
      expect(settings.prefs.theme, 'light');
      expect(settings.prefs.emailWeekly, isFalse);
      expect(settings.sessions, isEmpty);
    });

    test('excludes every null key', () async {
      final h = _Harness(
        body: jsonEncode({'data': <String, Object?>{}}),
        status: 200,
      );

      await h.api.updateSettings();

      expect(h.adapter.requests.single.data, <String, dynamic>{});
    });
  });

  group('sessions and password', () {
    test('lists sessions', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': [
            {'id': '11', 'ip': '198.51.100.4', 'current': false},
          ],
        }),
        status: 200,
      );

      final sessions = await h.api.sessions();

      final request = h.adapter.requests.single;
      expect(request.method, 'GET');
      expect(request.path, 'api/v1/me/sessions');
      expect(sessions.single.id, 11);
      expect(sessions.single.current, isFalse);
    });

    test('revokes one session and handles 204', () async {
      final h = _Harness(body: '', status: 204);

      await h.api.revokeSession(11);

      final request = h.adapter.requests.single;
      expect(request.method, 'DELETE');
      expect(request.path, 'api/v1/me/sessions/11');
    });

    test('revokes every session and handles 204', () async {
      final h = _Harness(body: '', status: 204);

      await h.api.revokeAllSessions();

      final request = h.adapter.requests.single;
      expect(request.method, 'DELETE');
      expect(request.path, 'api/v1/me/sessions');
    });

    test('changes the password and handles 204', () async {
      final h = _Harness(body: '', status: 204);

      await h.api.changePassword('old-secret', 'new-secret-123');

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/me/password');
      expect(request.data, {
        'current_password': 'old-secret',
        'new_password': 'new-secret-123',
      });
    });
  });

  group('blocks', () {
    test('lists blocked accounts', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': [
            {
              'id': 'u2',
              'handle': 'bob',
              'display_name': 'Bob',
              'blocked_at': '2026-10-20T10:00:00Z',
            },
          ],
        }),
        status: 200,
      );

      final blocked = await h.api.blockedUsers();

      final request = h.adapter.requests.single;
      expect(request.method, 'GET');
      expect(request.path, 'api/v1/me/blocks');
      expect(blocked.single.handle, 'bob');
      expect(blocked.single.blockedAt, '2026-10-20T10:00:00Z');
    });

    test('unblocks through the encoded handle path', () async {
      final h = _Harness(body: '', status: 204);

      await h.api.unblockUser('bob smith');

      final request = h.adapter.requests.single;
      expect(request.method, 'DELETE');
      expect(request.path, 'api/v1/users/bob%20smith/block');
    });
  });

  group('native auth', () {
    test('reads the public auth config without a token', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {
            'registration_open': true,
            'password_fallback': true,
            'oidc_enabled': false,
            'email_verification_required': true,
          },
        }),
        status: 200,
      );

      final config = await h.api.authConfig();

      final request = h.adapter.requests.single;
      expect(request.method, 'GET');
      expect(request.path, 'api/v1/auth/config');
      expect(request.headers['Authorization'], isNull);
      expect(config.registrationOpen, isTrue);
      expect(config.passwordFallback, isTrue);
      expect(config.oidcEnabled, isFalse);
      expect(config.emailVerificationRequired, isTrue);
    });

    test('registers and parses the verification status', () async {
      final h = _Harness(
        body: jsonEncode({
          'data': {'status': 'verification_sent'},
        }),
        status: 201,
      );

      final result = await h.api.register(
        email: 'ada@soda.test',
        handle: 'ada',
        displayName: 'Ada',
        password: 'long-enough-password',
      );

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/auth/register');
      expect(request.headers['Authorization'], isNull);
      expect(request.data, {
        'email': 'ada@soda.test',
        'handle': 'ada',
        'display_name': 'Ada',
        'password': 'long-enough-password',
      });
      expect(result.status, 'verification_sent');
      expect(result.verificationRequired, isTrue);
    });

    test('parses a created registration', () async {
      final h = _Harness(
        body: '{"data":{"status":"created"}}',
        status: 201,
      );

      final result = await h.api.register(
        email: 'ada@soda.test',
        handle: 'ada',
        displayName: 'Ada',
        password: 'long-enough-password',
      );

      expect(result.status, 'created');
      expect(result.verificationRequired, isFalse);
    });

    test('verifies an email token', () async {
      final h = _Harness(body: '{"data":{"status":"verified"}}', status: 200);

      await h.api.verifyEmail('tok 1');

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/auth/verify');
      expect(request.headers['Authorization'], isNull);
      expect(request.data, {'token': 'tok 1'});
    });

    test('resends verification and accepts 202', () async {
      final h = _Harness(
        body: '{"data":{"status":"sent"}}',
        status: 202,
      );

      await h.api.resendVerification('ada@soda.test');

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/auth/verify/resend');
      expect(request.headers['Authorization'], isNull);
      expect(request.data, {'email': 'ada@soda.test'});
    });

    test('requests a password reset and accepts 202', () async {
      final h = _Harness(
        body: '{"data":{"status":"sent"}}',
        status: 202,
      );

      await h.api.requestPasswordReset('ada@soda.test');

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/auth/password/reset');
      expect(request.headers['Authorization'], isNull);
      expect(request.data, {'email': 'ada@soda.test'});
    });

    test('confirms a password reset and handles 204', () async {
      final h = _Harness(body: '', status: 204);

      await h.api.confirmPasswordReset('tok', 'new-secret-123');

      final request = h.adapter.requests.single;
      expect(request.method, 'POST');
      expect(request.path, 'api/v1/auth/password/reset/confirm');
      expect(request.headers['Authorization'], isNull);
      expect(request.data, {
        'token': 'tok',
        'new_password': 'new-secret-123',
      });
    });

    test('maps registration_conflict to ApiException', () async {
      final h = _Harness(
        body:
            '{"error":{"code":"registration_conflict","message":"handle or email is already taken"}}',
        status: 409,
      );

      await expectLater(
        h.api.register(
          email: 'ada@soda.test',
          handle: 'ada',
          displayName: 'Ada',
          password: 'long-enough-password',
        ),
        throwsA(
          isA<ApiException>()
              .having(
                (e) => e.message,
                'message',
                'handle or email is already taken',
              )
              .having((e) => e.statusCode, 'statusCode', 409),
        ),
      );
    });
  });

  group('error mapping', () {
    test('wraps existing endpoints in ApiException', () async {
      final h = _Harness(
        body: '{"error":{"message":"boom"}}',
        status: 500,
      );
      await expectLater(
        h.api.feed(),
        throwsA(
          isA<ApiException>()
              .having((e) => e.message, 'message', 'boom')
              .having((e) => e.statusCode, 'statusCode', 500),
        ),
      );
    });
  });
}
