import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

import '../api/api_client.dart';

/// Durable queue for check-ins made while offline. The server deduplicates on
/// the idempotency key, so replaying a request after a timeout is safe.
class CheckinOutbox {
  static const _key = 'sodapop.outbox.v1';

  Future<void> enqueue(Map<String, dynamic> checkin) async {
    final prefs = await SharedPreferences.getInstance();
    final rows = prefs.getStringList(_key) ?? <String>[];
    rows.add(jsonEncode(checkin));
    await prefs.setStringList(_key, rows);
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
          drinkId: item['drink_id'] as int,
          rating: item['rating'] as int?,
          note: '${item['note'] ?? ''}',
          idempotencyKey: '${item['idempotency_key']}',
        );
        sent++;
      } catch (_) {
        remaining.add(row);
      }
    }
    await prefs.setStringList(_key, remaining);
    return sent;
  }
}
