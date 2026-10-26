/// Plain-language formatting helpers shared by the member screens.
library;

const _months = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
];

DateTime? _parse(String raw) {
  final trimmed = raw.trim();
  if (trimmed.isEmpty) return null;
  return DateTime.tryParse(trimmed)?.toLocal();
}

/// "Today", "Yesterday", "26 Oct" or "26 Oct 2025" in the device's time zone.
String sodaDate(String raw) {
  final value = _parse(raw);
  if (value == null) return raw;
  final now = DateTime.now();
  final today = DateTime(now.year, now.month, now.day);
  final day = DateTime(value.year, value.month, value.day);
  final difference = today.difference(day).inDays;
  if (difference == 0) return 'Today';
  if (difference == 1) return 'Yesterday';
  final month = _months[value.month - 1];
  return value.year == now.year
      ? '${value.day} $month'
      : '${value.day} $month ${value.year}';
}

/// "14:05" in the device's time zone from an RFC 3339 timestamp or an
/// already-clock-like value. Unparseable input is returned unchanged.
String sodaClock(String raw) {
  final value = _parse(raw);
  if (value == null) return raw;
  final hour = value.hour.toString().padLeft(2, '0');
  final minute = value.minute.toString().padLeft(2, '0');
  return '$hour:$minute';
}

/// "26 Oct 14:05" (or "Today 14:05") from a timestamp, keeping the day when a
/// clear-time lands after midnight.
String sodaDateTime(String raw) {
  final value = _parse(raw);
  if (value == null) return raw;
  return '${sodaDate(raw)} ${sodaClock(raw)}';
}

/// "Joined Oct 2026" from a profile creation timestamp.
String sodaJoined(String raw) {
  final value = _parse(raw);
  if (value == null) return '';
  return 'Joined ${_months[value.month - 1]} ${value.year}';
}

/// Check-in visibility wording, matching the web app.
String sodaVisibility(String visibility) {
  switch (visibility) {
    case 'public':
      return 'Anyone';
    case 'friends':
      return 'Friends';
    case 'private':
      return 'Only you';
    default:
      return '';
  }
}

/// List visibility wording, matching the web app.
String sodaListVisibility(String visibility) {
  switch (visibility) {
    case 'public':
      return 'Anyone';
    case 'friends':
      return 'Friends';
    case 'private':
      return 'Only me';
    default:
      return '';
  }
}

/// A readable venue kind. Unknown values are capitalised instead of hidden.
String sodaVenueKind(String kind) {
  switch (kind) {
    case 'bar':
      return 'Bar';
    case 'restaurant':
      return 'Restaurant';
    case 'cafe':
      return 'Café';
    case 'shop':
      return 'Shop';
    case 'hotel':
      return 'Hotel';
    case 'festival':
      return 'Festival';
    case 'vending':
      return 'Vending machine';
    case 'other':
      return 'Other';
    case '':
      return '';
    default:
      return kind[0].toUpperCase() + kind.substring(1);
  }
}

String sodaCount(int count, String singular, [String? plural]) =>
    '$count ${count == 1 ? singular : (plural ?? '${singular}s')}';
