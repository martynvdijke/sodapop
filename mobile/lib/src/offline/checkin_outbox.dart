import 'dart:convert';
import 'dart:typed_data';

import 'package:shared_preferences/shared_preferences.dart';

import '../api/api_client.dart';

/// Durable queue for check-ins made while offline. The server deduplicates on
/// the idempotency key, so replaying a request after a timeout is safe.
class CheckinOutbox {
  static const _key = 'sodapop.outbox.v1';

  /// Decoded photo bytes kept per queued check-in. Larger photos still queue,
  /// just without the image, mirroring the web's best-effort photo storage.
  static const maxPhotoBytes = 2 * 1024 * 1024;

  /// Queues [checkin]. Returns `false` only when a [photo] was supplied but
  /// exceeded [maxPhotoBytes] and had to be dropped, so the caller can warn.
  Future<bool> enqueue(
    Map<String, dynamic> checkin, {
    Uint8List? photo,
    String? photoFilename,
    String? photoMime,
  }) async {
    final prefs = await SharedPreferences.getInstance();
    final rows = prefs.getStringList(_key) ?? <String>[];
    final row = Map<String, dynamic>.from(checkin);
    var photoKept = true;
    if (photo != null && photo.isNotEmpty) {
      photoKept = photo.length <= maxPhotoBytes;
      if (photoKept) {
        row['photo_base64'] = base64Encode(photo);
        final filename = photoFilename?.trim();
        if (filename != null && filename.isNotEmpty) {
          row['photo_filename'] = filename;
        }
        final mime = photoMime?.trim();
        if (mime != null && mime.isNotEmpty) row['photo_mime'] = mime;
      }
    }
    rows.add(jsonEncode(row));
    await prefs.setStringList(_key, rows);
    return photoKept;
  }

  Future<List<Map<String, dynamic>>> pending() async {
    final prefs = await SharedPreferences.getInstance();
    final rows = prefs.getStringList(_key) ?? <String>[];
    return rows
        .map((row) => (jsonDecode(row) as Map).cast<String, dynamic>())
        .toList();
  }

  Future<int> flush(ApiClient api) async {
    final prefs = await SharedPreferences.getInstance();
    final rows = prefs.getStringList(_key) ?? <String>[];
    if (rows.isEmpty) return 0;
    final remaining = <String>[];
    var sent = 0;
    for (final row in rows) {
      final item = (jsonDecode(row) as Map).cast<String, dynamic>();
      try {
        await api.createCheckin(
          drinkId: (item['drink_id'] as num).toInt(),
          rating: (item['rating'] as num?)?.toInt(),
          note: '${item['note'] ?? ''}',
          venueId: (item['venue_id'] as num?)?.toInt(),
          serving: item['serving'] as String?,
          visibility: item['visibility'] as String?,
          idempotencyKey: '${item['idempotency_key']}',
          photo: _decodePhoto(item['photo_base64']),
          photoFilename: item['photo_filename'] as String?,
          photoMime: item['photo_mime'] as String?,
        );
        sent++;
      } catch (_) {
        remaining.add(row);
      }
    }
    await prefs.setStringList(_key, remaining);
    return sent;
  }

  /// Old rows have no photo field, and a malformed one must not block replay.
  static Uint8List? _decodePhoto(Object? encoded) {
    if (encoded is! String || encoded.isEmpty) return null;
    try {
      return base64Decode(encoded);
    } catch (_) {
      return null;
    }
  }
}
