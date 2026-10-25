import 'dart:math';

/// Validates and normalizes a user-entered Sodapop server URL.
///
/// Returns `null` when the value is empty, not HTTP(S), or carries a path,
/// query or fragment. Only an origin is accepted so every request path is
/// appended unambiguously.
String? normalizeServerUrl(String raw) {
  final value = raw.trim();
  if (value.isEmpty) return null;
  final uri = Uri.tryParse(value);
  if (uri == null) return null;
  if (uri.scheme != 'http' && uri.scheme != 'https') return null;
  if (uri.host.isEmpty) return null;
  if (uri.userInfo.isNotEmpty) return null;
  if (uri.path.isNotEmpty && uri.path != '/') return null;
  if (uri.hasQuery || uri.hasFragment) return null;
  final port = uri.hasPort ? ':${uri.port}' : '';
  return '${uri.scheme}://${uri.host}$port';
}

/// Returns a random RFC 4122 v4 identifier without external dependencies.
String newIdempotencyKey() {
  final random = Random.secure();
  final bytes = List<int>.generate(16, (_) => random.nextInt(256));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  final hex = bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  return '${hex.substring(0, 8)}-${hex.substring(8, 12)}-'
      '${hex.substring(12, 16)}-${hex.substring(16, 20)}-${hex.substring(20)}';
}

/// Demo helper for quarter-star values: 17 becomes "4.25".
String ratingLabel(int quarters) {
  if (quarters <= 0) return 'Not rated';
  final whole = quarters ~/ 4;
  final part = quarters % 4;
  return part == 0 ? '$whole' : '$whole.${part * 25}'.replaceFirst('.50', '.5');
}
