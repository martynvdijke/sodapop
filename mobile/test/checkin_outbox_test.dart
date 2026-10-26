import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/offline/checkin_outbox.dart';

class _Call {
  _Call({
    required this.drinkId,
    required this.rating,
    required this.note,
    required this.idempotencyKey,
    this.photo,
    this.photoFilename,
    this.photoMime,
  });

  final int drinkId;
  final int? rating;
  final String note;
  final String idempotencyKey;
  final Uint8List? photo;
  final String? photoFilename;
  final String? photoMime;
}

class _RecordingApi extends ApiClient {
  final calls = <_Call>[];

  @override
  Future<void> createCheckin({
    required int drinkId,
    int? rating,
    String note = '',
    int? venueId,
    String? serving,
    String? visibility,
    String? idempotencyKey,
    Uint8List? photo,
    String? photoFilename,
    String? photoMime,
  }) async {
    calls.add(
      _Call(
        drinkId: drinkId,
        rating: rating,
        note: note,
        idempotencyKey: idempotencyKey ?? '',
        photo: photo,
        photoFilename: photoFilename,
        photoMime: photoMime,
      ),
    );
  }
}

const _prefsKey = 'sodapop.outbox.v1';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('round-trips a queued check-in without a photo', () async {
    final outbox = CheckinOutbox();
    final kept = await outbox.enqueue({
      'drink_id': 9,
      'rating': 16,
      'note': 'Cold and sharp.',
      'idempotency_key': 'key-plain',
    });
    expect(kept, isTrue);

    final pending = await outbox.pending();
    expect(pending.single['drink_id'], 9);
    expect(pending.single.containsKey('photo_base64'), isFalse);

    final api = _RecordingApi();
    expect(await outbox.flush(api), 1);
    expect(api.calls.single.drinkId, 9);
    expect(api.calls.single.rating, 16);
    expect(api.calls.single.note, 'Cold and sharp.');
    expect(api.calls.single.idempotencyKey, 'key-plain');
    expect(api.calls.single.photo, isNull);
    expect(await outbox.pending(), isEmpty);
  });

  test('stores photo bytes as base64 and replays them', () async {
    final outbox = CheckinOutbox();
    final photo = Uint8List.fromList([0xFF, 0xD8, 0xFF, 1, 2, 3]);
    final kept = await outbox.enqueue(
      {
        'drink_id': 9,
        'rating': null,
        'note': '',
        'idempotency_key': 'key-photo',
      },
      photo: photo,
      photoFilename: 'cola.jpg',
      photoMime: 'image/jpeg',
    );
    expect(kept, isTrue);

    final row = (await outbox.pending()).single;
    expect(row['photo_base64'], base64Encode(photo));
    expect(row['photo_filename'], 'cola.jpg');
    expect(row['photo_mime'], 'image/jpeg');

    final api = _RecordingApi();
    expect(await outbox.flush(api), 1);
    expect(api.calls.single.photo, photo);
    expect(api.calls.single.photoFilename, 'cola.jpg');
    expect(api.calls.single.photoMime, 'image/jpeg');
    expect(api.calls.single.idempotencyKey, 'key-photo');
    expect(await outbox.pending(), isEmpty);
  });

  test('drops an oversized photo but still queues the check-in', () async {
    final outbox = CheckinOutbox();
    final big = Uint8List(CheckinOutbox.maxPhotoBytes + 1);
    final kept = await outbox.enqueue(
      {'drink_id': 3, 'idempotency_key': 'key-big'},
      photo: big,
      photoFilename: 'big.jpg',
      photoMime: 'image/jpeg',
    );
    expect(kept, isFalse);

    final row = (await outbox.pending()).single;
    expect(row.containsKey('photo_base64'), isFalse);
    expect(row.containsKey('photo_filename'), isFalse);

    final api = _RecordingApi();
    expect(await outbox.flush(api), 1);
    expect(api.calls.single.photo, isNull);
    expect(api.calls.single.idempotencyKey, 'key-big');
  });

  test('replays legacy rows without photo fields', () async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setStringList(_prefsKey, [
      jsonEncode({
        'drink_id': 4,
        'rating': 12,
        'note': 'Old row',
        'idempotency_key': 'legacy-key',
      }),
    ]);

    final api = _RecordingApi();
    expect(await CheckinOutbox().flush(api), 1);
    expect(api.calls.single.drinkId, 4);
    expect(api.calls.single.rating, 12);
    expect(api.calls.single.note, 'Old row');
    expect(api.calls.single.photo, isNull);
    expect(api.calls.single.idempotencyKey, 'legacy-key');
  });

  test('a malformed photo does not block the row', () async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setStringList(_prefsKey, [
      jsonEncode({
        'drink_id': 5,
        'idempotency_key': 'bad-photo',
        'photo_base64': 'not base64!!',
      }),
    ]);

    final api = _RecordingApi();
    expect(await CheckinOutbox().flush(api), 1);
    expect(api.calls.single.photo, isNull);
  });
}
